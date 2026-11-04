// fn-terminal runs only on Linux: fnOS shell identities are local Linux accounts.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"fn-terminal/internal/broker"
	"fn-terminal/internal/config"
	"fn-terminal/internal/identity"
	"fn-terminal/internal/launch"
	"fn-terminal/internal/server"
	"fn-terminal/internal/terminal"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

var version = "dev"

func main() {
	if e := run(os.Args[1:]); e != nil {
		slog.Error("application stopped", "error", e)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: fn-terminal dev|supervise|version")
	}
	if args[0] == "version" {
		fmt.Println(version)
		return nil
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	switch args[0] {
	case "dev":
		return development(ctx, args[1:])
	case "supervise":
		return supervise(ctx, args[1:])
	case "web":
		return web(ctx, args[1:])
	case "configure":
		return configure(args[1:])
	default:
		return errors.New("unknown run mode")
	}
}
func development(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("dev", flag.ContinueOnError)
	address := f.String("listen", "127.0.0.1:8088", "loopback listen address")
	if e := f.Parse(args); e != nil {
		return e
	}
	if e := launch.ValidateDevListen(*address); e != nil {
		return e
	}
	if os.Geteuid() == 0 {
		return errors.New("development mode must not run as root")
	}
	account, e := identity.Lookup(uint32(os.Geteuid()))
	if e != nil {
		return e
	}
	c := config.Defaults()
	c.Enabled = true
	if e = identity.Validate(account, identity.Claims{UID: account.UID, Username: account.Username}, identity.Policy{MinUID: 1000}); e != nil {
		return e
	}
	dir, e := os.MkdirTemp("", "fnterminal-dev-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "broker.sock")
	l, e := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if e != nil {
		return e
	}
	defer l.Close()
	if e = os.Chmod(path, 0600); e != nil {
		return e
	}
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	b := broker.New(c, account.UID)
	b.Development = true
	done := make(chan error, 1)
	go func() { done <- b.Serve(child, l) }()
	listener, e := net.Listen("tcp", *address)
	if e != nil {
		return e
	}
	slog.Info("development terminal listening", "url", "http://"+*address+server.Prefix+"/", "uid", account.UID)
	e = serveHTTP(child, listener, server.Options{Config: c, Development: true, Connect: connector(path)})
	cancel()
	l.Close()
	<-done
	return e
}
func connector(path string) server.Connector {
	return func(ctx context.Context, c identity.Claims, size terminal.Size) (net.Conn, error) {
		return broker.Dial(ctx, path, c, size)
	}
}
func serveHTTP(ctx context.Context, l net.Listener, o server.Options) error {
	srv := &http.Server{Handler: server.New(o), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 * 1024, BaseContext: func(net.Listener) context.Context { return ctx }}
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = srv.Shutdown(c)
		case <-stopped:
		}
	}()
	e := srv.Serve(l)
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}
func web(ctx context.Context, args []string) error {
	if os.Geteuid() == 0 {
		return errors.New("web service must not run as root")
	}
	f := flag.NewFlagSet("web", flag.ContinueOnError)
	cfg := f.String("config", "", "config path")
	path := f.String("broker-socket", "", "broker path")
	if e := f.Parse(args); e != nil {
		return e
	}
	if os.Getenv("FNTERMINAL_LISTENER_FD") != "3" {
		return errors.New("web mode requires a supervisor-provided listener")
	}
	c, e := config.Load(*cfg)
	if e != nil {
		return e
	}
	file := os.NewFile(3, "gateway-listener")
	if file == nil {
		return errors.New("missing gateway FD")
	}
	defer file.Close()
	l, e := net.FileListener(file)
	if e != nil {
		return e
	}
	defer l.Close()
	if _, ok := l.(*net.UnixListener); !ok {
		return errors.New("gateway listener must be a Unix socket")
	}
	return serveHTTP(ctx, l, server.Options{Config: c, AppUID: uint32(os.Geteuid()), Connect: connector(*path)})
}
func supervise(ctx context.Context, args []string) error {
	if os.Geteuid() != 0 {
		return errors.New("production supervisor requires root lifecycle privileges")
	}
	f := flag.NewFlagSet("supervise", flag.ContinueOnError)
	cfg := f.String("config", "", "root-owned config")
	appUser := f.String("app-user", "", "package user")
	gateway := f.String("gateway-socket", "", "gateway socket path")
	path := f.String("broker-socket", "", "broker socket path")
	if e := f.Parse(args); e != nil {
		return e
	}
	if *cfg == "" || *appUser == "" || *gateway == "" || *path == "" || *path == *gateway {
		return errors.New("all production paths and app user are required")
	}
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	exe, e = launch.SecurePath(exe)
	if e != nil {
		return e
	}
	configPath, e := launch.SecurePath(*cfg)
	if e != nil {
		return e
	}
	c, e := config.Load(configPath)
	if e != nil {
		return e
	}
	account, e := launch.PackageUser(*appUser)
	if e != nil {
		return e
	}
	for _, p := range []string{*gateway, *path} {
		if !filepath.IsAbs(p) {
			return errors.New("socket paths must be absolute")
		}
		if _, e = launch.SecurePath(filepath.Dir(p)); e != nil {
			return e
		}
	}
	lock, e := lockFile(*path + ".lock")
	if e != nil {
		return e
	}
	defer lock.Close()
	g, e := listenProtected(*gateway, 0, 0, 0600)
	if e != nil {
		return e
	}
	defer g.Close()
	b, e := listenProtected(*path, 0, int(account.GID), 0660)
	if e != nil {
		return e
	}
	defer b.Close()
	fd, e := g.File()
	if e != nil {
		return e
	}
	defer fd.Close()
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	brokerDone := make(chan error, 1)
	go func() { brokerDone <- broker.New(c, account.UID).Serve(childCtx, b) }()
	command := exec.Command(exe, "web", "--config", configPath, "--broker-socket", *path)
	command.ExtraFiles = []*os.File{fd}
	command.Env = []string{"FNTERMINAL_LISTENER_FD=3", "HOME=" + account.Home, "USER=" + account.Username, "PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	command.Dir = "/"
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: account.UID, Gid: account.GID, Groups: account.Groups}, Pdeathsig: syscall.SIGKILL, Setsid: true}
	if e = command.Start(); e != nil {
		cancel()
		b.Close()
		<-brokerDone
		return e
	}
	childDone := make(chan error, 1)
	go func() { childDone <- command.Wait() }()
	slog.Info("production terminal started", "web_uid", account.UID, "version", version)
	brokerFinished, webFinished := false, false
	select {
	case <-ctx.Done():
	case e = <-brokerDone:
		brokerFinished = true
		cancel()
		if e == nil {
			e = errors.New("broker exited unexpectedly")
		}
	case e = <-childDone:
		webFinished = true
		cancel()
		if e == nil {
			e = errors.New("web service exited unexpectedly")
		}
	}
	cancel()
	g.Close()
	b.Close()
	_ = command.Process.Signal(syscall.SIGTERM)
	if !webFinished {
		select {
		case <-childDone:
		case <-time.After(3 * time.Second):
			_ = command.Process.Kill()
		}
	}
	// Closing broker context tears down terminal process groups before supervision exits.
	if !brokerFinished {
		select {
		case <-brokerDone:
		case <-time.After(4 * time.Second):
		}
	}
	return e
}
func lockFile(path string) (*os.File, error) {
	fd, e := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), path)
	if _, e = launch.SecurePath(path); e != nil {
		f.Close()
		return nil, e
	}
	if e = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); e != nil {
		f.Close()
		return nil, errors.New("another supervisor is already running")
	}
	return f, nil
}
func listenProtected(path string, uid, gid int, mode os.FileMode) (*net.UnixListener, error) {
	if info, e := os.Lstat(path); e == nil {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("refusing to replace non-root socket: %s", path)
		}
		if e = os.Remove(path); e != nil {
			return nil, e
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	l, e := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if e != nil {
		return nil, e
	}
	if e = os.Chown(path, uid, gid); e == nil {
		e = os.Chmod(path, mode)
	}
	if e != nil {
		l.Close()
		return nil, e
	}
	return l, nil
}

func configure(args []string) error {
	if os.Geteuid() != 0 {
		return errors.New("configuration changes require the root lifecycle")
	}
	f := flag.NewFlagSet("configure", flag.ContinueOnError)
	path := f.String("config", "", "configuration path")
	enabled := f.String("enabled", "", "true or false")
	automaticOrigins := f.Bool("automatic-origins", false, "clear manual origin restrictions")
	if e := f.Parse(args); e != nil {
		return e
	}
	if *enabled != "true" && *enabled != "false" {
		return errors.New("enabled must be true or false")
	}
	resolved, e := launch.SecurePath(*path)
	if e != nil {
		return e
	}
	return config.SetAccess(resolved, *enabled == "true", *automaticOrigins)
}

// Package broker exposes a narrow, kernel-peer-authenticated local PTY service.
package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fn-terminal/internal/config"
	"fn-terminal/internal/identity"
	"fn-terminal/internal/terminal"
	"fn-terminal/internal/wire"
	"golang.org/x/sys/unix"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"
)

type Hello struct {
	Claims identity.Claims `json:"claims"`
	Size   terminal.Size   `json:"size"`
}
type Server struct {
	Config      config.Config
	PeerUID     uint32
	Development bool
	quota       *terminal.Quota
}

func New(c config.Config, peerUID uint32) *Server {
	return &Server{Config: c, PeerUID: peerUID, quota: terminal.NewQuota(c.MaxPerUser, c.MaxTotal)}
}
func decode(b []byte, v any) error {
	if len(b) > 2048 {
		return errors.New("control message too large")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("extra control data")
	}
	return nil
}
func PeerUID(c *net.UnixConn) (uint32, error) {
	raw, e := c.SyscallConn()
	if e != nil {
		return 0, e
	}
	var uid uint32
	var inner error
	e = raw.Control(func(fd uintptr) {
		cred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		inner = err
		if err == nil {
			uid = cred.Uid
		}
	})
	if e != nil {
		return 0, e
	}
	return uid, inner
}
func (s *Server) Serve(ctx context.Context, l *net.UnixListener) error {
	var wg sync.WaitGroup
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			l.Close()
		case <-stop:
		}
	}()
	defer wg.Wait()
	slots := make(chan struct{}, s.Config.MaxTotal+8)
	for {
		c, e := l.AcceptUnix()
		if e != nil {
			if ctx.Err() != nil || errors.Is(e, net.ErrClosed) {
				return nil
			}
			return e
		}
		select {
		case slots <- struct{}{}:
			wg.Add(1)
			go func() { defer wg.Done(); defer func() { <-slots }(); s.handle(ctx, c) }()
		default:
			c.Close()
		}
	}
}
func (s *Server) handle(parent context.Context, c *net.UnixConn) {
	defer c.Close()
	uid, e := PeerUID(c)
	if e != nil || uid != s.PeerUID {
		return
	}
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	kind, b, e := wire.Read(c)
	if e != nil || kind != wire.Hello {
		return
	}
	fail := func() {
		_ = wire.Write(c, wire.Failure, []byte(`{"message":"无法创建终端：请检查账号、启用状态或会话上限"}`))
	}
	var h Hello
	if decode(b, &h) != nil || !s.Config.Enabled || !h.Size.Valid() {
		fail()
		return
	}
	policy := identity.Policy{MinUID: s.Config.MinUID, DeniedUID: s.PeerUID, AllowedUsers: s.Config.AllowedUsers}
	if s.Development {
		if h.Claims.UID != uint32(os.Geteuid()) {
			fail()
			return
		}
		policy.DeniedUID = 0
	}
	a, e := identity.Resolve(h.Claims, policy)
	if e != nil {
		fail()
		return
	}
	release, e := s.quota.Acquire(a.UID)
	if e != nil {
		fail()
		return
	}
	defer release()
	sess, e := terminal.Start(a, h.Size)
	if e != nil {
		slog.Warn("terminal start failed", "uid", a.UID, "error", e)
		fail()
		return
	}
	defer sess.Close()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	active := terminal.NewActivity()
	go active.Monitor(ctx, cancel, time.Duration(s.Config.IdleSeconds)*time.Second, time.Duration(s.Config.MaxSeconds)*time.Second)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			c.Close()
			sess.Close()
		case <-sess.Done():
			// A descendant may retain the slave after its shell exits. Allow bounded
			// final output draining, then terminate all remaining session job groups.
			select {
			case <-time.After(200 * time.Millisecond):
				sess.Close()
			case <-stop:
			}
		case <-stop:
		}
	}()
	_ = c.SetDeadline(time.Time{})
	var writeMu sync.Mutex
	send := func(k byte, p []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return wire.Write(c, k, p)
	}
	ready, _ := json.Marshal(h.Claims)
	if send(wire.Ready, ready) != nil {
		return
	}
	slog.Info("terminal opened", "uid", a.UID)
	defer slog.Info("terminal closed", "uid", a.UID)
	go func() {
		defer cancel()
		for {
			k, p, e := wire.Read(c)
			if e != nil {
				return
			}
			switch k {
			case wire.Data:
				if len(p) > 0 {
					active.Touch()
					if _, e = sess.PTY.Write(p); e != nil {
						return
					}
				}
			case wire.Resize:
				var size terminal.Size
				if decode(p, &size) != nil || sess.Resize(size) != nil {
					return
				}
			default:
				return
			}
		}
	}()
	buf := make([]byte, 16*1024)
	for {
		n, e := sess.PTY.Read(buf)
		if n > 0 {
			active.Touch()
			if send(wire.Data, buf[:n]) != nil {
				return
			}
		}
		if e != nil {
			ended, _ := json.Marshal(map[string]string{"message": "会话已结束", "reason": shellExitReason(ctx, sess.Done())})
			_ = send(wire.Exit, ended)
			return
		}
	}
}

func shellExitReason(ctx context.Context, done <-chan struct{}) string {
	if ctx.Err() != nil {
		return "session_end"
	}
	// PTY EOF can arrive just before cmd.Wait reaps the shell. A closed PTY
	// alone does not establish that the shell exited, and cancellation wins
	// even when cleanup has already reaped the child.
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-done:
		if ctx.Err() == nil {
			return "shell_exit"
		}
	case <-ctx.Done():
	case <-timer.C:
	}
	return "session_end"
}

// Dial completes the bounded handshake before returning a stream owned by one user.
func Dial(ctx context.Context, path string, c identity.Claims, size terminal.Size) (net.Conn, error) {
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, e := d.DialContext(ctx, "unix", path)
	if e != nil {
		return nil, e
	}
	ok := false
	defer func() {
		if !ok {
			conn.Close()
		}
	}()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	b, _ := json.Marshal(Hello{Claims: c, Size: size})
	if e = wire.Write(conn, wire.Hello, b); e != nil {
		return nil, e
	}
	k, _, e := wire.Read(conn)
	if e != nil {
		return nil, e
	}
	if k != wire.Ready {
		return nil, errors.New("terminal access denied or session limit reached")
	}
	_ = conn.SetDeadline(time.Time{})
	ok = true
	return conn, nil
}

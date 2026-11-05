package broker

import (
	"context"
	"encoding/json"
	"fn-terminal/internal/config"
	"fn-terminal/internal/identity"
	"fn-terminal/internal/terminal"
	"fn-terminal/internal/wire"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestBrokerChecksKernelPeerUID(t *testing.T) {
	p := filepath.Join(t.TempDir(), "b.sock")
	l, e := net.ListenUnix("unix", &net.UnixAddr{Name: p, Net: "unix"})
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := config.Defaults()
	c.Enabled = true
	s := New(c, uint32(os.Getuid()+1))
	go s.Serve(ctx, l)
	conn, e := net.DialTimeout("unix", p, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	if _, _, e = wire.Read(conn); e == nil {
		t.Fatal("untrusted peer received a usable broker connection")
	}
}
func TestBrokerRejectsDisabledAndForgedAccount(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "forged"}[enabled], func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "b.sock")
			l, e := net.ListenUnix("unix", &net.UnixAddr{Name: p, Net: "unix"})
			if e != nil {
				t.Fatal(e)
			}
			defer l.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c := config.Defaults()
			c.Enabled = enabled
			s := New(c, uint32(os.Getuid()))
			go s.Serve(ctx, l)
			_, e = Dial(ctx, p, identity.Claims{UID: uint32(os.Getuid()), Username: "definitely-not-this-account"}, terminal.Size{Rows: 24, Cols: 80})
			if e == nil {
				t.Fatal("accepted disabled/forged account")
			}
		})
	}
}
func TestBrokerRealSessionAndDisconnectCleanup(t *testing.T) {
	p := filepath.Join(t.TempDir(), "b.sock")
	l, e := net.ListenUnix("unix", &net.UnixAddr{Name: p, Net: "unix"})
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, e := identity.Lookup(uint32(os.Getuid()))
	if e != nil {
		t.Fatal(e)
	}
	c := config.Defaults()
	c.Enabled = true
	c.MaxPerUser = 1
	s := New(c, uint32(os.Getuid()))
	s.Development = true
	go s.Serve(ctx, l)
	conn, e := Dial(ctx, p, identity.Claims{UID: a.UID, Username: a.Username}, terminal.Size{Rows: 24, Cols: 80})
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if e = wire.Write(conn, wire.Data, []byte("printf '\\nBROKER_'; sleep 0.02; printf 'OK\\n'\n")); e != nil {
		t.Fatal(e)
	}
	found := false
	var output []byte
	for i := 0; i < 20; i++ {
		kind, b, e := wire.Read(conn)
		if e != nil {
			t.Fatal(e)
		}
		if kind != wire.Data {
			continue
		}
		// PTY reads and wire frames may split anywhere in a command's output.
		output = append(output, b...)
		if contains(output, []byte("\r\nBROKER_OK\r\n")) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no real shell output")
	}
	conn.Close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		next, e := Dial(ctx, p, identity.Claims{UID: a.UID, Username: a.Username}, terminal.Size{Rows: 24, Cols: 80})
		if e == nil {
			next.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("quota leaked after disconnect: %v", e)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
func contains(b, sub []byte) bool {
	for i := 0; i+len(sub) <= len(b); i++ {
		if string(b[i:i+len(sub)]) == string(sub) {
			return true
		}
	}
	return false
}
func TestHelloRejectsArbitraryFields(t *testing.T) {
	var h Hello
	b := []byte(`{"claims":{"uid":1000,"username":"alice"},"size":{"rows":24,"cols":80},"shell":"/bin/bash -c evil"}`)
	if decode(b, &h) == nil {
		t.Fatal("accepted arbitrary execution arguments")
	}
	_, _ = json.Marshal(h)
}
func TestShellExitReleasesSessionEvenWithInheritedSlave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "b.sock")
	l, e := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, e := identity.Lookup(uint32(os.Getuid()))
	if e != nil {
		t.Fatal(e)
	}
	c := config.Defaults()
	c.Enabled = true
	c.MaxPerUser = 1
	s := New(c, uint32(os.Getuid()))
	s.Development = true
	go s.Serve(ctx, l)
	cl := identity.Claims{UID: a.UID, Username: a.Username}
	conn, e := Dial(ctx, path, cl, terminal.Size{Rows: 24, Cols: 80})
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	e = wire.Write(conn, wire.Data, []byte("sh -c 'trap \"\" HUP; printf \"\\nCHILD:%s\\n\" \"$$\"; exec sleep 120' &\n"))
	if e != nil {
		t.Fatal(e)
	}
	var all string
	child := 0
	for child == 0 {
		_, b, e := wire.Read(conn)
		if e != nil {
			t.Fatal(e)
		}
		all += string(b)
		m := regexp.MustCompile(`\r\nCHILD:([0-9]+)\r\n`).FindStringSubmatch(all)
		if len(m) > 0 {
			child, _ = strconv.Atoi(m[1])
		}
	}
	defer syscall.Kill(child, syscall.SIGKILL)
	if e = wire.Write(conn, wire.Data, []byte("exit\n")); e != nil {
		t.Fatal(e)
	}
	for {
		k, payload, e := wire.Read(conn)
		if e != nil {
			t.Fatalf("shell exited but session was retained: %v", e)
		}
		if k == wire.Exit {
			var ended struct {
				Reason string `json:"reason"`
			}
			if e := json.Unmarshal(payload, &ended); e != nil || ended.Reason != "shell_exit" {
				t.Fatalf("shell exit reason = %q: %s (%v)", ended.Reason, payload, e)
			}
			break
		}
	}
	conn.Close()
	deadline := time.Now().Add(2 * time.Second)
	for {
		next, e := Dial(ctx, path, cl, terminal.Size{Rows: 24, Cols: 80})
		if e == nil {
			next.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("shell exit leaked quota", e)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestBrokerReportsRealShellExit(t *testing.T) {
	for _, code := range []int{0, 7} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "b.sock")
			listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			a, err := identity.Lookup(uint32(os.Getuid()))
			if err != nil {
				t.Fatal(err)
			}
			cfg := config.Defaults()
			cfg.Enabled = true
			s := New(cfg, uint32(os.Getuid()))
			s.Development = true
			go s.Serve(ctx, listener)
			conn, err := Dial(ctx, path, identity.Claims{UID: a.UID, Username: a.Username}, terminal.Size{Rows: 24, Cols: 80})
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			if err := wire.Write(conn, wire.Data, []byte("exit "+strconv.Itoa(code)+"\n")); err != nil {
				t.Fatal(err)
			}
			for {
				kind, payload, err := wire.Read(conn)
				if err != nil {
					t.Fatal(err)
				}
				if kind != wire.Exit {
					continue
				}
				var ended struct {
					Reason string `json:"reason"`
				}
				if err := json.Unmarshal(payload, &ended); err != nil || ended.Reason != "shell_exit" {
					t.Fatalf("exit %d reason = %q: %s (%v)", code, ended.Reason, payload, err)
				}
				break
			}
		})
	}
}

func TestShellExitReasonWaitsForReapingAfterPTYEOF(t *testing.T) {
	done := make(chan struct{})
	go func() {
		time.Sleep(10 * time.Millisecond)
		close(done)
	}()
	if reason := shellExitReason(context.Background(), done); reason != "shell_exit" {
		t.Fatalf("PTY EOF preceded reaping: reason = %q", reason)
	}
}

func TestShellExitReasonCancellationTakesPrecedence(t *testing.T) {
	for _, reaped := range []bool{false, true} {
		t.Run(strconv.FormatBool(reaped), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			done := make(chan struct{})
			if reaped {
				close(done)
			}
			if reason := shellExitReason(ctx, done); reason != "session_end" {
				t.Fatalf("cancelled session misreported as %q", reason)
			}
		})
	}
}

func TestShellExitReasonWaitIsBounded(t *testing.T) {
	result := make(chan string, 1)
	go func() { result <- shellExitReason(context.Background(), make(chan struct{})) }()
	select {
	case reason := <-result:
		if reason != "session_end" {
			t.Fatalf("live shell misreported as %q", reason)
		}
	case <-time.After(time.Second):
		t.Fatal("PTY EOF stalled waiting for a live shell")
	}
}

func TestShellExitReasonCancellationInterruptsReapingWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
		close(done)
	}()
	if reason := shellExitReason(ctx, done); reason != "session_end" {
		t.Fatalf("cancelled reaping wait misreported as %q", reason)
	}
}

// The web layer can only explain a disconnect if the Exit frame precedes the EOF.
func TestBrokerReportsIdleExpiryBeforeClosing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "b.sock")
	l, e := net.ListenUnix("unix", &net.UnixAddr{Name: p, Net: "unix"})
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, e := identity.Lookup(uint32(os.Getuid()))
	if e != nil {
		t.Fatal(e)
	}
	c := config.Defaults()
	c.Enabled = true
	c.IdleSeconds = 1
	s := New(c, uint32(os.Getuid()))
	s.Development = true
	go s.Serve(ctx, l)
	conn, e := Dial(ctx, p, identity.Claims{UID: a.UID, Username: a.Username}, terminal.Size{Rows: 24, Cols: 80})
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	for {
		kind, b, e := wire.Read(conn)
		if e != nil {
			t.Fatalf("stream ended without an expiry reason: %v", e)
		}
		if kind != wire.Exit {
			continue
		}
		var ended struct{ Reason string }
		if e = json.Unmarshal(b, &ended); e != nil || ended.Reason != wire.ReasonIdleTimeout {
			t.Fatalf("exit frame = %s (%v)", b, e)
		}
		return
	}
}

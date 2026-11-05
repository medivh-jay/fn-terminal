package terminal

import (
	"bytes"
	"context"
	"errors"
	"fn-terminal/internal/identity"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestShellStartsWhenAccountHomeIsMissing(t *testing.T) {
	a, err := identity.Lookup(uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	a.Shell = "/bin/sh"
	a.Home = filepath.Join(t.TempDir(), "missing-home")
	s, err := Start(a, Size{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatalf("missing home prevented shell startup: %v", err)
	}
	defer s.Close()
	out := make(chan []byte, 1)
	go func() { b, _ := io.ReadAll(s.PTY); out <- b }()
	if _, err := s.PTY.Write([]byte("printf '\\nCWD:%s\\nHOME:%s\\nUID:%s\\n' \"$(pwd -P)\" \"$HOME\" \"$(id -u)\"; exit\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case b := <-out:
		want := "\r\nCWD:/\r\nHOME:" + a.Home + "\r\nUID:" + strconv.Itoa(int(a.UID)) + "\r\n"
		if !bytes.Contains(b, []byte(want)) {
			t.Fatalf("unexpected fallback shell environment: %q", b)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("fallback shell did not exit")
	}
	if _, err := os.Stat(a.Home); !os.IsNotExist(err) {
		t.Fatalf("startup created the missing home: %v", err)
	}
}

func TestRealShellUnicodeSizeAndExit(t *testing.T) {
	a, e := identity.Lookup(uint32(os.Getuid()))
	if e != nil {
		t.Fatal(e)
	}
	a.Shell = "/bin/sh"
	s, e := Start(a, Size{Rows: 37, Cols: 111})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	out := make(chan []byte, 1)
	go func() { b, _ := io.ReadAll(s.PTY); out <- b }()
	_, e = s.PTY.Write([]byte("printf '\\n__BEGIN__\\n'; stty size; printf '中文🙂\\n'; exit\n"))
	if e != nil {
		t.Fatal(e)
	}
	select {
	case b := <-out:
		if !bytes.Contains(b, []byte("\r\n37 111\r\n")) || !bytes.Contains(b, []byte("中文🙂\r\n")) {
			t.Fatalf("unexpected output %q", b)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("shell did not exit")
	}
	select {
	case <-s.Done():
	case <-time.After(time.Second):
		t.Fatal("shell not reaped")
	}
}
func TestCloseKillsShellProcessGroup(t *testing.T) {
	a, e := identity.Lookup(uint32(os.Getuid()))
	if e != nil {
		t.Fatal(e)
	}
	a.Shell = "/bin/sh"
	s, e := Start(a, Size{Rows: 24, Cols: 80})
	if e != nil {
		t.Fatal(e)
	}
	pid := s.PID()
	s.Close()
	s.Close()
	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("shell survived close")
	}
	b, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if len(b) > 0 && !strings.Contains(string(b), ") Z ") {
		t.Fatalf("shell remains: %s", b)
	}
}
func TestCloseEscalatesWhenShellIgnoresHangup(t *testing.T) {
	a, e := identity.Lookup(uint32(os.Getuid()))
	if e != nil {
		t.Fatal(e)
	}
	a.Shell = "/bin/sh"
	s, e := Start(a, Size{Rows: 24, Cols: 80})
	if e != nil {
		t.Fatal(e)
	}
	output := make(chan string, 32)
	go func() {
		buf := make([]byte, 1024)
		for {
			n, e := s.PTY.Read(buf)
			if n > 0 {
				output <- string(buf[:n])
			}
			if e != nil {
				return
			}
		}
	}()
	_, e = s.PTY.Write([]byte("trap '' HUP; printf '\\nTRAP_READY\\n'\n"))
	if e != nil {
		t.Fatal(e)
	}
	var seen string
	for !strings.Contains(seen, "\r\nTRAP_READY\r\n") {
		select {
		case b := <-output:
			seen += b
		case <-time.After(2 * time.Second):
			t.Fatal("trap setup failed")
		}
	}
	done := make(chan struct{})
	go func() { s.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("PTY close blocked before SIGKILL escalation")
	}
}
func TestQuotaIsAtomicAndReleases(t *testing.T) {
	q := NewQuota(1, 2)
	release, e := q.Acquire(1000)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = q.Acquire(1000); e == nil {
		t.Fatal("per-user quota bypassed")
	}
	r2, e := q.Acquire(1001)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = q.Acquire(1002); e == nil {
		t.Fatal("global quota bypassed")
	}
	release()
	release()
	r2()
	r, e := q.Acquire(1000)
	if e != nil {
		t.Fatal(e)
	}
	r()
}
func TestConcurrentSessionsCannotExceedUserQuota(t *testing.T) {
	q := NewQuota(4, 32)
	start := make(chan struct{})
	release := make(chan struct{})
	results := make(chan bool, 64)
	var workers sync.WaitGroup
	for i := 0; i < 64; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			done, e := q.Acquire(1000)
			results <- e == nil
			if e == nil {
				<-release
				done()
			}
		}()
	}
	close(start)
	admitted := 0
	for i := 0; i < 64; i++ {
		if <-results {
			admitted++
		}
	}
	close(release)
	workers.Wait()
	if admitted != 4 {
		t.Fatalf("concurrent admission count=%d, want 4", admitted)
	}
	done, e := q.Acquire(1000)
	if e != nil {
		t.Fatal("quota not released", e)
	}
	done()
}
func TestCloseKillsHangupIgnoringJobInSeparateProcessGroup(t *testing.T) {
	a, e := identity.Lookup(uint32(os.Getuid()))
	if e != nil {
		t.Fatal(e)
	}
	a.Shell = "/bin/bash"
	s, e := Start(a, Size{Rows: 24, Cols: 80})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	_, e = s.PTY.Write([]byte("sh -c 'trap \"\" HUP; printf \"\\nCHILD:%s\\n\" \"$$\"; exec sleep 120' &\n"))
	if e != nil {
		t.Fatal(e)
	}
	result := make(chan int, 1)
	go func() {
		var all string
		buf := make([]byte, 2048)
		for {
			n, e := s.PTY.Read(buf)
			all += string(buf[:n])
			m := regexp.MustCompile(`\r\nCHILD:([0-9]+)\r\n`).FindStringSubmatch(all)
			if len(m) > 0 {
				pid, _ := strconv.Atoi(m[1])
				result <- pid
				return
			}
			if e != nil {
				result <- 0
				return
			}
		}
	}()
	var child int
	select {
	case child = <-result:
	case <-time.After(3 * time.Second):
		t.Fatal("child did not start")
	}
	if child == 0 {
		t.Fatal("no child PID")
	}
	defer syscall.Kill(child, syscall.SIGKILL)
	pg, e := syscall.Getpgid(child)
	if e != nil {
		t.Fatal(e)
	}
	if pg == s.PID() {
		t.Fatal("fixture did not create a distinct job-control process group")
	}
	s.Close()
	deadline := time.Now().Add(time.Second)
	for {
		b, _ := os.ReadFile("/proc/" + strconv.Itoa(child) + "/stat")
		if len(b) == 0 || strings.Contains(string(b), ") Z ") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("same-session job survived close: %s", b)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func TestMonitorExpiresIdleSession(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	m := NewActivity()
	go m.Monitor(ctx, cancel, 30*time.Millisecond, time.Minute)
	select {
	case <-ctx.Done():
		if !errors.Is(context.Cause(ctx), ErrIdleExpired) {
			t.Fatalf("cause = %v", context.Cause(ctx))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("idle session never expired")
	}
}
func TestMonitorExpiresLongSessionDespiteActivity(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	m := NewActivity()
	go m.Monitor(ctx, cancel, time.Minute, 30*time.Millisecond)
	select {
	case <-ctx.Done():
		if !errors.Is(context.Cause(ctx), ErrMaxExpired) {
			t.Fatalf("cause = %v", context.Cause(ctx))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("long session never expired")
	}
}

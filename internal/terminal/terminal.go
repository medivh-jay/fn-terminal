// Package terminal owns real Linux PTYs and their child process groups.
package terminal

import (
	"errors"
	"fmt"
	"fn-terminal/internal/identity"
	"github.com/creack/pty"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Size struct {
	Rows uint16 `json:"rows"`
	Cols uint16 `json:"cols"`
}

func (s Size) Valid() bool { return s.Rows >= 2 && s.Rows <= 500 && s.Cols >= 2 && s.Cols <= 1000 }

type Session struct {
	PTY  *os.File
	cmd  *exec.Cmd
	done chan struct{}
	once sync.Once
}

func Start(a identity.Account, size Size) (*Session, error) {
	if !size.Valid() || a.UID == 0 {
		return nil, errors.New("invalid terminal account or size")
	}
	cmd := exec.Command(a.Shell, "-l")
	cmd.Dir = a.Home
	// fnOS accounts can retain a passwd home that has never been created.
	// Keep HOME and the account identity intact while starting in a usable cwd.
	if _, err := os.Stat(a.Home); errors.Is(err, os.ErrNotExist) {
		cmd.Dir = "/"
	}
	cmd.Env = []string{"HOME=" + a.Home, "USER=" + a.Username, "LOGNAME=" + a.Username, "SHELL=" + a.Shell, "TERM=xterm-256color", "COLORTERM=truecolor", "PATH=/usr/local/bin:/usr/bin:/bin:/usr/local/sbin:/usr/sbin:/sbin", "LANG=C.UTF-8"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Pdeathsig: syscall.SIGKILL}
	if os.Geteuid() == 0 {
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: a.UID, Gid: a.GID, Groups: a.Groups}
	} else if a.UID != uint32(os.Geteuid()) {
		return nil, errors.New("cannot change identity without broker privileges")
	}
	master, slave, err := pty.Open()
	if err != nil {
		return nil, err
	}
	defer slave.Close()
	if os.Geteuid() == 0 {
		if err = slave.Chown(int(a.UID), int(a.GID)); err != nil {
			master.Close()
			return nil, err
		}
	}
	if err = pty.Setsize(master, &pty.Winsize{Rows: size.Rows, Cols: size.Cols}); err != nil {
		master.Close()
		return nil, err
	}
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	if err = cmd.Start(); err != nil {
		master.Close()
		return nil, fmt.Errorf("start shell %q in directory %q: %w", a.Shell, cmd.Dir, err)
	}
	s := &Session{PTY: master, cmd: cmd, done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(s.done) }()
	return s, nil
}
func (s *Session) Done() <-chan struct{} { return s.done }
func (s *Session) PID() int              { return s.cmd.Process.Pid }
func (s *Session) Resize(size Size) error {
	if !size.Valid() {
		return errors.New("invalid terminal dimensions")
	}
	return pty.Setsize(s.PTY, &pty.Winsize{Rows: size.Rows, Cols: size.Cols})
}
func (s *Session) Close() {
	s.once.Do(func() {
		pid := s.PID()
		groups := sessionGroups(pid)
		for group := range groups {
			_ = syscall.Kill(-group, syscall.SIGHUP)
		}
		_ = s.PTY.Close()
		// Interactive job control creates additional PGIDs in the same POSIX session.
		// Snapshot before HUP, then scan again for jobs created during the grace period.
		time.Sleep(150 * time.Millisecond)
		for group := range sessionGroups(pid) {
			groups[group] = true
		}
		for group := range groups {
			_ = syscall.Kill(-group, syscall.SIGKILL)
		}
		select {
		case <-s.done:
		case <-time.After(2 * time.Second):
		}
	})
}
func sessionGroups(sessionID int) map[int]bool {
	groups := map[int]bool{sessionID: true}
	entries, e := os.ReadDir("/proc")
	if e != nil {
		return groups
	}
	for _, entry := range entries {
		if _, e = strconv.Atoi(entry.Name()); e != nil {
			continue
		}
		data, e := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if e != nil {
			continue
		}
		// comm is parenthesized and may contain spaces or ')'; numeric fields follow its last ')'.
		end := strings.LastIndexByte(string(data), ')')
		if end < 0 {
			continue
		}
		fields := strings.Fields(string(data[end+1:]))
		if len(fields) < 4 {
			continue
		}
		sid, e := strconv.Atoi(fields[3])
		if e != nil || sid != sessionID {
			continue
		}
		group, e := strconv.Atoi(fields[2])
		if e == nil && group > 0 {
			groups[group] = true
		}
	}
	return groups
}
func (a Size) String() string { return strconv.Itoa(int(a.Cols)) + "x" + strconv.Itoa(int(a.Rows)) }

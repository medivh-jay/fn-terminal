// Package launch validates the privileged startup boundary. It never repairs unrelated system paths.
package launch

import (
	"errors"
	"fmt"
	"fn-terminal/internal/identity"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
)

func ValidateDevListen(address string) error {
	host, port, e := net.SplitHostPort(address)
	if e != nil {
		return e
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("development requires a literal loopback IP")
	}
	n, e := strconv.Atoi(port)
	if e != nil || n < 1 || n > 65535 {
		return errors.New("invalid development port")
	}
	return nil
}
func SecurePath(path string) (string, error) {
	original, e := filepath.Abs(path)
	if e != nil {
		return "", e
	}
	resolved, e := filepath.EvalSymlinks(original)
	if e != nil {
		return "", e
	}
	for _, start := range []string{original, resolved} {
		for p := start; ; p = filepath.Dir(p) {
			info, e := os.Lstat(p)
			if e != nil {
				return "", e
			}
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok || stat.Uid != 0 {
				return "", fmt.Errorf("path is not root-owned: %s", p)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				target, e := filepath.EvalSymlinks(p)
				if e != nil {
					return "", e
				}
				if _, e = checkResolved(target); e != nil {
					return "", e
				}
			} else if info.Mode().Perm()&0022 != 0 {
				return "", fmt.Errorf("path is writable by non-root users: %s", p)
			}
			if p == filepath.Dir(p) {
				break
			}
		}
	}
	return resolved, nil
}
func checkResolved(path string) (string, error) {
	for p := path; ; p = filepath.Dir(p) {
		info, e := os.Stat(p)
		if e != nil {
			return "", e
		}
		s, ok := info.Sys().(*syscall.Stat_t)
		if !ok || s.Uid != 0 || info.Mode().Perm()&0022 != 0 {
			return "", fmt.Errorf("insecure resolved path: %s", p)
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	return path, nil
}
func PackageUser(name string) (identity.Account, error) {
	u, e := user.Lookup(name)
	if e != nil {
		return identity.Account{}, e
	}
	id, e := strconv.ParseUint(u.Uid, 10, 32)
	if e != nil || id == 0 {
		return identity.Account{}, errors.New("web service user must not be root")
	}
	gid, e := strconv.ParseUint(u.Gid, 10, 32)
	if e != nil {
		return identity.Account{}, e
	}
	a := identity.Account{UID: uint32(id), GID: uint32(gid), Username: name, Home: u.HomeDir}
	groups, e := u.GroupIds()
	if e != nil {
		return a, e
	}
	for _, g := range groups {
		n, e := strconv.ParseUint(g, 10, 32)
		if e != nil {
			return a, e
		}
		a.Groups = append(a.Groups, uint32(n))
	}
	return a, nil
}

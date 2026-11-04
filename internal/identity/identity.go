package identity

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

type Claims struct {
	UID      uint32 `json:"uid"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
}
type Account struct {
	UID, GID              uint32
	Groups                []uint32
	Username, Home, Shell string
}
type Policy struct {
	MinUID       uint32
	DeniedUID    uint32
	AllowedUsers []string
}

func FromGateway(h http.Header) (Claims, error) {
	var c Claims
	for _, k := range []string{"X-Trim-Userid", "X-Trim-Username", "X-Trim-Isadmin"} {
		if len(h.Values(k)) != 1 {
			return c, errors.New("missing or repeated gateway identity")
		}
	}
	raw := h.Get("X-Trim-Userid")
	uid, err := strconv.ParseUint(raw, 10, 32)
	if err != nil || uid == 0 || strconv.FormatUint(uid, 10) != raw {
		return c, errors.New("invalid user ID")
	}
	name := h.Get("X-Trim-Username")
	if name == "" || len(name) > 256 || strings.ContainsAny(name, "\x00\r\n:,/\\ ") {
		return c, errors.New("invalid username")
	}
	admin := h.Get("X-Trim-Isadmin")
	if admin != "true" && admin != "false" {
		return c, errors.New("invalid administrator flag")
	}
	return Claims{UID: uint32(uid), Username: name, IsAdmin: admin == "true"}, nil
}

func Lookup(uid uint32) (Account, error) {
	u, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		return Account{}, err
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return Account{}, err
	}
	a := Account{UID: uid, GID: uint32(gid), Username: u.Username, Home: u.HomeDir}
	ids, err := u.GroupIds()
	if err != nil {
		return a, err
	}
	for _, s := range ids {
		id, e := strconv.ParseUint(s, 10, 32)
		if e != nil {
			return a, e
		}
		a.Groups = append(a.Groups, uint32(id))
	}
	// fnOS local accounts are backed by passwd. Do not invent an interactive shell for a disabled account.
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return a, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Split(line, ":")
		if len(f) == 7 && f[0] == a.Username && f[2] == u.Uid {
			a.Shell = f[6]
			break
		}
	}
	if a.Shell == "" {
		return a, errors.New("local account has no configured shell")
	}
	return a, nil
}

func Validate(a Account, c Claims, p Policy) error {
	if a.UID == 0 || a.UID < p.MinUID || a.UID == p.DeniedUID || a.UID != c.UID || a.Username != c.Username {
		return errors.New("account is not allowed")
	}
	if !filepath.IsAbs(a.Home) || strings.ContainsRune(a.Home, 0) {
		return errors.New("invalid home directory")
	}
	switch a.Shell {
	case "/bin/bash", "/usr/bin/bash", "/bin/sh", "/usr/bin/sh", "/bin/zsh", "/usr/bin/zsh", "/bin/fish", "/usr/bin/fish":
	default:
		return errors.New("account does not have an allowed interactive shell")
	}
	if len(p.AllowedUsers) > 0 {
		for _, s := range p.AllowedUsers {
			if s == a.Username {
				return nil
			}
		}
		return errors.New("account is not on the allowlist")
	}
	return nil
}

func Resolve(c Claims, p Policy) (Account, error) {
	a, err := Lookup(c.UID)
	if err != nil {
		return a, fmt.Errorf("resolve account: %w", err)
	}
	return a, Validate(a, c, p)
}

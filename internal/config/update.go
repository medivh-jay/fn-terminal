package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// SetEnabled updates only opt-in and preserves ownership, permissions and policy.
// Its privileged caller validates the full root-owned path before calling.
func SetEnabled(path string, enabled bool) error {
	return SetAccess(path, enabled, false)
}

// SetAccess changes opt-in and optionally clears a legacy manual origin list.
// Its privileged caller validates the root-owned path; all other policy survives.
func SetAccess(path string, enabled bool, automaticOrigins bool) error {
	info, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() {
		return errors.New("config must be a regular file")
	}
	c, e := Load(path)
	if e != nil {
		return e
	}
	c.Enabled = enabled
	if automaticOrigins {
		c.AllowedOrigins = nil
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".config-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("missing config ownership")
	}
	if e = f.Chown(int(stat.Uid), int(stat.Gid)); e != nil {
		return e
	}
	if e = f.Chmod(info.Mode().Perm()); e != nil {
		return e
	}
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if e = encoder.Encode(c); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	dir, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer dir.Close()
	return dir.Sync()
}

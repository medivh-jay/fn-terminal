package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultsRequireExplicitEnablement(t *testing.T) {
	c, err := Parse(strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Enabled || c.MaxPerUser != 4 || c.MaxTotal != 32 || c.IdleSeconds != 900 || c.MaxSeconds != 3600 {
		t.Fatalf("unsafe defaults: %+v", c)
	}
}
func TestEnablementUpdateRetainsPolicyAndMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if e := os.WriteFile(p, []byte(`{"enabled":false,"allowed_users":["alice"],"idle_seconds":60}`), 0640); e != nil {
		t.Fatal(e)
	}
	if e := SetEnabled(p, true); e != nil {
		t.Fatal(e)
	}
	c, e := Load(p)
	if e != nil || !c.Enabled || c.IdleSeconds != 60 || len(c.AllowedUsers) != 1 || c.AllowedUsers[0] != "alice" {
		t.Fatalf("policy lost: %+v %v", c, e)
	}
	info, e := os.Stat(p)
	if e != nil || info.Mode().Perm() != 0640 {
		t.Fatalf("permissions changed: %v %v", info, e)
	}
	link := filepath.Join(t.TempDir(), "link")
	if e = os.Symlink(p, link); e != nil {
		t.Fatal(e)
	}
	if SetEnabled(link, false) == nil {
		t.Fatal("followed config symlink during update")
	}
}
func TestRejectsUnknownFieldsAndInvalidLimits(t *testing.T) {
	for _, s := range []string{`{"enable":true}`, `{"max_per_user":0}`, `{"idle_seconds":-1}`, `{"max_total":999999}`, `{} {}`, `{"allowed_origins":["*"]}`} {
		if _, err := Parse(strings.NewReader(s)); err == nil {
			t.Errorf("accepted %s", s)
		}
	}
}

func TestRestoreAutomaticOriginsPreservesOtherPolicy(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"enabled":true,"allowed_origins":["https://nas.fnos.net"],"allowed_users":["alice"],"idle_seconds":60}`), 0640); err != nil {
		t.Fatal(err)
	}
	if err := SetAccess(p, true, true); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil || len(c.AllowedOrigins) != 0 || !c.Enabled || c.IdleSeconds != 60 || len(c.AllowedUsers) != 1 || c.AllowedUsers[0] != "alice" {
		t.Fatalf("policy not preserved: %+v %v", c, err)
	}
	info, err := os.Stat(p)
	if err != nil || info.Mode().Perm() != 0640 {
		t.Fatalf("permissions changed: %v %v", info, err)
	}
	link := filepath.Join(t.TempDir(), "config-link")
	if err := os.Symlink(p, link); err != nil {
		t.Fatal(err)
	}
	if err := SetAccess(link, true, true); err == nil {
		t.Fatal("followed config symlink")
	}
}

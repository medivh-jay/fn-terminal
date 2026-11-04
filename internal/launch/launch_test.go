package launch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDevelopmentListenMustBeLiteralLoopback(t *testing.T) {
	for _, s := range []string{":8088", "0.0.0.0:8088", "[::]:8088", "nas.local:8088", "localhost:8088"} {
		if ValidateDevListen(s) == nil {
			t.Errorf("accepted %q", s)
		}
	}
	for _, s := range []string{"127.0.0.1:8088", "[::1]:8088"} {
		if e := ValidateDevListen(s); e != nil {
			t.Errorf("rejected %s: %v", s, e)
		}
	}
}
func TestRejectsUserWritableExecutableAndAncestors(t *testing.T) {
	p := filepath.Join(t.TempDir(), "binary")
	if e := os.WriteFile(p, []byte("x"), 0755); e != nil {
		t.Fatal(e)
	}
	if _, e := SecurePath(p); e == nil {
		t.Fatal("accepted user-owned path under writable /tmp")
	}
	if _, e := SecurePath("/usr/bin/env"); e != nil {
		t.Fatalf("root protected system executable rejected: %v", e)
	}
}
func TestPackageUserCannotBeRootOrSystemUser(t *testing.T) {
	if _, e := PackageUser("root"); e == nil {
		t.Fatal("accepted root web account")
	}
	if _, e := PackageUser("does-not-exist-fnterminal"); e == nil {
		t.Fatal("accepted missing web account")
	}
}

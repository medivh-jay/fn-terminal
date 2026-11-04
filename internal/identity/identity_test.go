package identity

import (
	"net/http"
	"testing"
)

func TestGatewayRejectsMissingDuplicateAndInvalidIdentity(t *testing.T) {
	for _, h := range []http.Header{
		{}, {"X-Trim-Userid": {"1000", "1001"}, "X-Trim-Username": {"alice"}},
		{"X-Trim-Userid": {"0"}, "X-Trim-Username": {"root"}},
		{"X-Trim-Userid": {"1000,1001"}, "X-Trim-Username": {"alice"}},
		{"X-Trim-Userid": {"-1"}, "X-Trim-Username": {"alice"}},
		{"X-Trim-Userid": {"1000"}, "X-Trim-Username": {"alice"}, "X-Trim-Isadmin": {"yes"}},
	} {
		if _, err := FromGateway(h); err == nil {
			t.Errorf("accepted invalid headers: %v", h)
		}
	}
	h := http.Header{"X-Trim-Userid": {"1000"}, "X-Trim-Username": {"alice"}, "X-Trim-Isadmin": {"false"}}
	got, err := FromGateway(h)
	if err != nil || got.UID != 1000 || got.Username != "alice" || got.IsAdmin {
		t.Fatalf("claims = %+v, %v", got, err)
	}
}

func TestAccountPolicyCannotAdmitRootOrMismatchedUser(t *testing.T) {
	good := Account{UID: 1000, GID: 1000, Username: "alice", Home: "/home/alice", Shell: "/bin/bash"}
	policy := Policy{MinUID: 1000, DeniedUID: 2000}
	claims := Claims{UID: 1000, Username: "alice"}
	if err := Validate(good, claims, policy); err != nil {
		t.Fatal(err)
	}
	for _, a := range []Account{
		{UID: 0, GID: 0, Username: "root", Home: "/root", Shell: "/bin/bash"},
		{UID: 999, GID: 999, Username: "alice", Home: "/home/alice", Shell: "/bin/bash"},
		{UID: 1000, GID: 1000, Username: "bob", Home: "/home/alice", Shell: "/bin/bash"},
		{UID: 1000, GID: 1000, Username: "alice", Home: "relative", Shell: "/bin/bash"},
		{UID: 1000, GID: 1000, Username: "alice", Home: "/home/alice", Shell: "/usr/sbin/nologin"},
	} {
		if Validate(a, claims, policy) == nil {
			t.Errorf("accepted forbidden account %+v", a)
		}
	}
	policy.DeniedUID = 1000
	if Validate(good, claims, policy) == nil {
		t.Fatal("accepted package user")
	}
	policy.DeniedUID = 2000
	policy.AllowedUsers = []string{"bob"}
	if Validate(good, claims, policy) == nil {
		t.Fatal("bypassed user allowlist")
	}
}

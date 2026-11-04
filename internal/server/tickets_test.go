package server

import (
	"fn-terminal/internal/identity"
	"sync"
	"testing"
	"time"
)

func TestTicketBindsIdentityAndOriginAndCannotBeReplayed(t *testing.T) {
	s := newTicketStore()
	a := identity.Claims{UID: 1000, Username: "alice"}
	now := time.Now()
	token, err := s.issue(a, "http://192.168.1.2:5666", now)
	if err != nil || len(token) != 43 {
		t.Fatalf("issue: %q %v", token, err)
	}
	for _, bad := range []struct {
		claims identity.Claims
		origin string
	}{
		{identity.Claims{UID: 1001, Username: "bob"}, "http://192.168.1.2:5666"},
		{identity.Claims{UID: 1000, Username: "other"}, "http://192.168.1.2:5666"},
		{a, "https://nas.fnos.net"},
	} {
		if s.consume(token, bad.claims, bad.origin, now) {
			t.Fatal("accepted ticket for different identity or origin")
		}
	}
	if !s.consume(token, a, "http://192.168.1.2:5666", now) || s.consume(token, a, "http://192.168.1.2:5666", now) {
		t.Fatal("ticket was rejected or reusable")
	}
}

func TestTicketExpiresAndIssuanceIsBounded(t *testing.T) {
	s := newTicketStore()
	a := identity.Claims{UID: 1000, Username: "alice"}
	now := time.Now()
	var first string
	for i := 0; i < 8; i++ {
		token, err := s.issue(a, "https://nas.fnos.net", now)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = token
		}
	}
	if _, err := s.issue(a, "https://nas.fnos.net", now); err == nil {
		t.Fatal("per-user ticket limit bypassed")
	}
	if s.consume(first, a, "https://nas.fnos.net", now.Add(time.Minute)) {
		t.Fatal("expired ticket accepted")
	}
	if _, err := s.issue(a, "https://nas.fnos.net", now.Add(time.Minute)); err != nil {
		t.Fatalf("expired tickets retained capacity: %v", err)
	}
	s = newTicketStore()
	for i := 0; i < 1024; i++ {
		if _, err := s.issue(identity.Claims{UID: uint32(1000 + i), Username: "user"}, "https://nas.fnos.net", now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.issue(a, "https://nas.fnos.net", now); err == nil {
		t.Fatal("global ticket limit bypassed")
	}
}

func TestOnlyOneConcurrentConnectionCanConsumeTicket(t *testing.T) {
	s := newTicketStore()
	a := identity.Claims{UID: 1000, Username: "alice"}
	now := time.Now()
	token, err := s.issue(a, "https://nas.fnos.net", now)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan bool, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- s.consume(token, a, "https://nas.fnos.net", now) }()
	}
	wg.Wait()
	close(results)
	n := 0
	for ok := range results {
		if ok {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("ticket admitted %d connections", n)
	}
}

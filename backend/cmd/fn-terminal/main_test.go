package main

import (
	"context"
	"testing"
)

func TestCLIRefusesUnsafeDevelopmentAddress(t *testing.T) {
	if e := development(context.Background(), []string{"--listen", "0.0.0.0:8088"}); e == nil {
		t.Fatal("opened a publicly reachable shell")
	}
}
func TestWebCannotStartWithoutSupervisedListener(t *testing.T) {
	t.Setenv("FNTERMINAL_LISTENER_FD", "")
	if e := web(context.Background(), nil); e == nil {
		t.Fatal("started unauthenticated standalone web service")
	}
}

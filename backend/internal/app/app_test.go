package app

import "testing"

func TestGreeting(t *testing.T) {
	if got := Greeting("over-laid"); got != "Hello, over-laid!" {
		t.Fatalf("unexpected greeting: %s", got)
	}
}

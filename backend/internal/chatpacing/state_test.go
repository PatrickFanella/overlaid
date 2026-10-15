package chatpacing

import (
	"fmt"
	"testing"
	"time"
)

func TestThousandChannelSimulation(t *testing.T) {
	now := time.Unix(1700000000, 0)
	var a accountState
	channels := map[string]time.Time{}
	sent := []time.Time{}
	for i := range 1000 {
		channel := fmt.Sprint(i % 1000)
		if ready := a.readyAt(now, channels[channel]); ready.After(now) {
			now = ready
		}
		if ready := a.readyAt(now, channels[channel]); ready.After(now) {
			t.Fatal("not ready", ready, now)
		}
		if len(a.Attempts) >= 20 {
			t.Fatal("budget overrun")
		}
		a.begin(now)
		a.complete(now, 0)
		channels[channel] = now.Add(channelInterval)
		sent = append(sent, now)
	}
	for i, at := range sent {
		count := 0
		for j := i; j >= 0 && sent[j].After(at.Add(-30*time.Second)); j-- {
			count++
		}
		if count > 20 {
			t.Fatalf("window contains %d attempts", count)
		}
	}
	if span := sent[999].Sub(sent[0]); span != 49*30*time.Second {
		t.Fatalf("unexpected simulated load span %v", span)
	}
}

func TestChannelCooldownClockAndLeaseBoundaries(t *testing.T) {
	now := time.Unix(1700000000, 0)
	a := accountState{}
	if ready := a.readyAt(now, now.Add(time.Second)); !ready.Equal(now.Add(time.Second)) {
		t.Fatal(ready)
	}
	a.begin(now)
	if ready := a.readyAt(now, now); !ready.Equal(now.Add(leaseDuration)) {
		t.Fatal("lease bypass", ready)
	}
	a.complete(now.Add(5*time.Second), 42*time.Second)
	if ready := a.readyAt(now.Add(5*time.Second), now); !ready.Equal(now.Add(47 * time.Second)) {
		t.Fatal("cooldown lost", ready)
	}
	if a.Attempts[0] != now.Add(5*time.Second).UnixMicro() {
		t.Fatal("slow attempt refunded")
	}
	a = accountState{Attempts: []int64{now.UnixMicro()}}
	if ready := a.readyAt(now.Add(-time.Hour), time.Time{}); !ready.Equal(now.Add(accountWindow)) {
		t.Fatal("backward clock reopened budget", ready)
	}
	a.complete(now, time.Duration(1<<63-1))
	if !a.BlockedUntil.After(now.Add(100 * 365 * 24 * time.Hour)) {
		t.Fatal("cooldown overflow")
	}
}

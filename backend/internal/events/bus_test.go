package events

import (
	"testing"
	"time"
)

func TestBusPublishesToSubscribers(t *testing.T) {
	bus := NewBus(4)
	client := bus.Subscribe()
	defer bus.Unsubscribe(client.ID)

	event := NewAlert("manual", "Test alert", "Hello overlay", SeverityInfo)
	bus.Publish(event)

	select {
	case got := <-client.Events:
		if got.ID == "" {
			t.Fatal("expected event ID to be set")
		}
		if got.Type != EventTypeAlert {
			t.Fatalf("expected alert event, got %q", got.Type)
		}
		if got.Title != "Test alert" {
			t.Fatalf("expected title %q, got %q", "Test alert", got.Title)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestBusDoesNotBlockWhenSubscriberBufferIsFull(t *testing.T) {
	bus := NewBus(1)
	client := bus.Subscribe()
	defer bus.Unsubscribe(client.ID)

	bus.Publish(NewAlert("manual", "one", "one", SeverityInfo))
	bus.Publish(NewAlert("manual", "two", "two", SeverityInfo))

	stats := bus.Stats()
	if stats.Published != 2 || stats.Dropped != 1 || stats.Subscribers != 1 {
		t.Fatalf("unexpected stats: %#v", stats)
	}

	select {
	case <-client.Events:
	case <-time.After(time.Second):
		t.Fatal("expected at least one buffered event")
	}
}

func TestBusUnsubscribeClosesChannel(t *testing.T) {
	bus := NewBus(1)
	client := bus.Subscribe()

	bus.Unsubscribe(client.ID)

	_, ok := <-client.Events
	if ok {
		t.Fatal("expected client event channel to be closed")
	}
}

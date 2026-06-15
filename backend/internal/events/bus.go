package events

import "sync"

type Client struct {
	ID     string
	Events chan Event
}

type Bus struct {
	mu         sync.RWMutex
	bufferSize int
	clients    map[string]Client
	published  uint64
	dropped    uint64
}

type BusStats struct {
	Subscribers int    `json:"subscribers"`
	Published   uint64 `json:"published"`
	Dropped     uint64 `json:"dropped"`
}

func NewBus(bufferSize int) *Bus {
	if bufferSize < 1 {
		bufferSize = 1
	}
	return &Bus{
		bufferSize: bufferSize,
		clients:    map[string]Client{},
	}
}

func (b *Bus) Subscribe() Client {
	client := Client{ID: newID(), Events: make(chan Event, b.bufferSize)}

	b.mu.Lock()
	b.clients[client.ID] = client
	b.mu.Unlock()

	return client
}

func (b *Bus) Unsubscribe(id string) {
	b.mu.Lock()
	client, ok := b.clients[id]
	if ok {
		delete(b.clients, id)
		close(client.Events)
	}
	b.mu.Unlock()
}

func (b *Bus) Publish(event Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.published++

	for _, client := range b.clients {
		select {
		case client.Events <- event:
		default:
			b.dropped++
		}
	}
}

func (b *Bus) Stats() BusStats {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return BusStats{Subscribers: len(b.clients), Published: b.published, Dropped: b.dropped}
}

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
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, client := range b.clients {
		select {
		case client.Events <- event:
		default:
		}
	}
}

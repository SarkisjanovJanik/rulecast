package eval

import (
	"sync"
	"sync/atomic"
)

type Registry struct {
	current atomic.Pointer[Snapshot]

	mu          sync.Mutex
	subscribers map[chan *Snapshot]struct{}
}

func NewRegistry() *Registry {
	return &Registry{subscribers: make(map[chan *Snapshot]struct{})}
}

func (r *Registry) Snapshot() *Snapshot {
	return r.current.Load()
}

func (r *Registry) Publish(snapshot *Snapshot) {
	r.current.Store(snapshot)

	r.mu.Lock()
	defer r.mu.Unlock()
	for subscriber := range r.subscribers {
		select {
		case subscriber <- snapshot:
		default:
		}
	}
}

func (r *Registry) Subscribe() (<-chan *Snapshot, func()) {
	channel := make(chan *Snapshot, 1)

	r.mu.Lock()
	r.subscribers[channel] = struct{}{}
	r.mu.Unlock()

	return channel, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if _, ok := r.subscribers[channel]; ok {
			delete(r.subscribers, channel)
			close(channel)
		}
	}
}

func (r *Registry) Subscribers() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.subscribers)
}

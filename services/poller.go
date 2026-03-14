package services

import (
	"context"
	"log"
	"sync"
	"time"
)

// StateUpdate is broadcast to all SSE subscribers when a service status changes.
type StateUpdate struct {
	States []ServiceState `json:"states"`
}

// Poller periodically polls all service statuses and notifies subscribers.
type Poller struct {
	registry *Registry
	manager  *Manager
	interval time.Duration

	mu          sync.RWMutex
	lastStates  map[string]ServiceState
	subscribers map[chan StateUpdate]struct{}

	trigger chan struct{}
}

// NewPoller creates a Poller. interval is the polling frequency.
func NewPoller(registry *Registry, manager *Manager, interval time.Duration) *Poller {
	return &Poller{
		registry:    registry,
		manager:     manager,
		interval:    interval,
		lastStates:  make(map[string]ServiceState),
		subscribers: make(map[chan StateUpdate]struct{}),
		trigger:     make(chan struct{}, 1),
	}
}

// Subscribe returns a channel that receives a StateUpdate whenever any
// service status changes. Call Unsubscribe when done.
func (p *Poller) Subscribe() chan StateUpdate {
	ch := make(chan StateUpdate, 4)
	p.mu.Lock()
	p.subscribers[ch] = struct{}{}
	p.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber channel.
func (p *Poller) Unsubscribe(ch chan StateUpdate) {
	p.mu.Lock()
	delete(p.subscribers, ch)
	p.mu.Unlock()
	close(ch)
}

// CurrentStates returns the most recently polled states for all services.
func (p *Poller) CurrentStates() []ServiceState {
	p.mu.RLock()
	defer p.mu.RUnlock()

	defs := p.registry.All()
	states := make([]ServiceState, 0, len(defs))
	for _, def := range defs {
		if s, ok := p.lastStates[def.ID]; ok {
			states = append(states, s)
		} else {
			// No poll result yet — return static fields only, status unknown.
			states = append(states, ServiceState{
				ID:             def.ID,
				Label:          def.Label,
				Description:    def.Description,
				InstallVersion: def.InstallVersion,
				HasCredentials: def.HasCredentials,
				Status:         StatusUnknown,
				Log:            def.Log,
				Installable:    def.Installable,
			})
		}
	}
	return states
}

// Poll triggers an immediate out-of-band poll without waiting for the next
// timer tick. Safe to call from multiple goroutines concurrently.
func (p *Poller) Poll() {
	select {
	case p.trigger <- struct{}{}:
	default: // already a pending trigger, no need to queue another
	}
}

// Run starts the polling loop. It blocks until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	// Poll immediately on start.
	p.poll()

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.trigger:
			p.poll()
		case <-ticker.C:
			p.poll()
		}
	}
}

func (p *Poller) poll() {
	defs := p.registry.All()

	// Poll all services concurrently.
	type result struct {
		state ServiceState
	}
	results := make([]result, len(defs))

	var wg sync.WaitGroup
	for i, def := range defs {
		wg.Add(1)
		go func(i int, def Definition) {
			defer wg.Done()
			results[i] = result{state: p.manager.GetState(def)}
		}(i, def)
	}
	wg.Wait()

	p.mu.Lock()
	defer p.mu.Unlock()

	states := make([]ServiceState, 0, len(results))
	for _, r := range results {
		p.lastStates[r.state.ID] = r.state
		states = append(states, r.state)
	}

	update := StateUpdate{States: states}
	for ch := range p.subscribers {
		select {
		case ch <- update:
		default:
			log.Printf("poller: subscriber channel full, dropping update")
		}
	}

	// If any service is in a transitional state, schedule a follow-up poll
	// after a short delay so the status resolves without waiting for the
	// next scheduled tick.
	for _, s := range states {
		if s.Status == StatusPending {
			time.AfterFunc(600*time.Millisecond, func() {
				select {
				case p.trigger <- struct{}{}:
				default:
				}
			})
			break
		}
	}
}

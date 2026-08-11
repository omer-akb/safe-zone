package policy

import (
	"context"
	"sync"
	"testing"
)

func TestNewActivatorRequiresRepository(t *testing.T) {
	if _, err := NewActivator(nil, &recordingPublisher{}); err == nil {
		t.Fatal("NewActivator(nil) error = nil")
	}
}

func TestNewActivatorRequiresPublisher(t *testing.T) {
	if _, err := NewActivator(&stubRepository{}, nil); err == nil {
		t.Fatal("NewActivator(repository, nil) error = nil")
	}
}

type stubRepository struct{ Repository }

type recordingPublisher struct {
	mu     sync.Mutex
	events []ActivationEvent
	err    error
}

func (p *recordingPublisher) PublishActivation(_ context.Context, event ActivationEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, event)
	return p.err
}

func (p *recordingPublisher) Events() []ActivationEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]ActivationEvent(nil), p.events...)
}

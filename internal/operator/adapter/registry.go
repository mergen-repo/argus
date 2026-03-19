package adapter

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

type AdapterFactory func(config json.RawMessage) (Adapter, error)

type Registry struct {
	mu        sync.RWMutex
	factories map[string]AdapterFactory
	adapters  map[uuid.UUID]Adapter
}

func NewRegistry() *Registry {
	r := &Registry{
		factories: make(map[string]AdapterFactory),
		adapters:  make(map[uuid.UUID]Adapter),
	}

	r.factories["mock"] = func(cfg json.RawMessage) (Adapter, error) {
		return NewMockAdapter(cfg)
	}
	r.factories["radius"] = func(cfg json.RawMessage) (Adapter, error) {
		return NewRADIUSAdapter(cfg)
	}
	r.factories["diameter"] = func(cfg json.RawMessage) (Adapter, error) {
		return NewDiameterAdapter(cfg)
	}

	return r
}

func (r *Registry) RegisterFactory(protocolType string, factory AdapterFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[protocolType] = factory
}

func (r *Registry) CreateAdapter(adapterType string, config json.RawMessage) (Adapter, error) {
	r.mu.RLock()
	factory, ok := r.factories[adapterType]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProtocol, adapterType)
	}

	return factory(config)
}

func (r *Registry) GetOrCreate(operatorID uuid.UUID, adapterType string, config json.RawMessage) (Adapter, error) {
	r.mu.RLock()
	a, ok := r.adapters[operatorID]
	r.mu.RUnlock()

	if ok {
		return a, nil
	}

	a, err := r.CreateAdapter(adapterType, config)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	if existing, ok := r.adapters[operatorID]; ok {
		r.mu.Unlock()
		return existing, nil
	}
	r.adapters[operatorID] = a
	r.mu.Unlock()

	return a, nil
}

func (r *Registry) Set(operatorID uuid.UUID, a Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[operatorID] = a
}

func (r *Registry) Get(operatorID uuid.UUID) (Adapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[operatorID]
	return a, ok
}

func (r *Registry) Remove(operatorID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.adapters, operatorID)
}

func (r *Registry) HasFactory(adapterType string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.factories[adapterType]
	return ok
}

func NewAdapter(adapterType string, config json.RawMessage) (Adapter, error) {
	switch adapterType {
	case "mock":
		return NewMockAdapter(config)
	case "radius":
		return NewRADIUSAdapter(config)
	case "diameter":
		return NewDiameterAdapter(config)
	default:
		return NewMockAdapter(json.RawMessage(`{"latency_ms":10}`))
	}
}

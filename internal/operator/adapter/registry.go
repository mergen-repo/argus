package adapter

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

type AdapterFactory func(config json.RawMessage) (Adapter, error)

// adapterKey uniquely identifies an adapter instance in the registry.
// STORY-090 Wave 2 Task 3: moves from per-operator single adapter to
// per-(operator, protocol) multi-adapter keying. One operator may now
// have multiple concurrent adapters (e.g. RADIUS + Diameter + SBA)
// each tracked independently.
type adapterKey struct {
	OperatorID uuid.UUID
	Protocol   string
}

type Registry struct {
	mu        sync.RWMutex
	factories map[string]AdapterFactory
	adapters  map[adapterKey]Adapter
}

func NewRegistry() *Registry {
	r := &Registry{
		factories: make(map[string]AdapterFactory),
		adapters:  make(map[adapterKey]Adapter),
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
	r.factories["sba"] = func(cfg json.RawMessage) (Adapter, error) {
		return NewSBAAdapter(cfg)
	}
	r.factories["http"] = func(cfg json.RawMessage) (Adapter, error) {
		return NewHTTPAdapter(cfg)
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

// GetOrCreate returns (or lazily constructs) the adapter for the given
// (operatorID, adapterType) tuple. Post-090 Wave 2: the key includes
// the protocol type so an operator may host multiple adapters
// concurrently.
func (r *Registry) GetOrCreate(operatorID uuid.UUID, adapterType string, config json.RawMessage) (Adapter, error) {
	k := adapterKey{OperatorID: operatorID, Protocol: adapterType}

	r.mu.RLock()
	a, ok := r.adapters[k]
	r.mu.RUnlock()

	if ok {
		return a, nil
	}

	a, err := r.CreateAdapter(adapterType, config)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	if existing, ok := r.adapters[k]; ok {
		r.mu.Unlock()
		return existing, nil
	}
	r.adapters[k] = a
	r.mu.Unlock()

	return a, nil
}

// Set stores an adapter under the (operatorID, protocol) key. STORY-090
// Wave 2 Task 3: GAINS protocol parameter; callers must supply the
// protocol an adapter represents.
func (r *Registry) Set(operatorID uuid.UUID, protocol string, a Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[adapterKey{OperatorID: operatorID, Protocol: protocol}] = a
}

// Get looks up an adapter by (operatorID, protocol). STORY-090 Wave 2
// Task 3: GAINS protocol parameter.
func (r *Registry) Get(operatorID uuid.UUID, protocol string) (Adapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[adapterKey{OperatorID: operatorID, Protocol: protocol}]
	return a, ok
}

// Remove drops EVERY adapter for the operator, regardless of protocol.
// Kept as a convenience for operator-delete / config-change paths; use
// RemoveProtocol for per-protocol invalidation.
func (r *Registry) Remove(operatorID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k := range r.adapters {
		if k.OperatorID == operatorID {
			delete(r.adapters, k)
		}
	}
}

// RemoveProtocol drops a single (operatorID, protocol) adapter. Used
// when only one protocol's config changes and peers remain valid.
func (r *Registry) RemoveProtocol(operatorID uuid.UUID, protocol string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.adapters, adapterKey{OperatorID: operatorID, Protocol: protocol})
}

// GetAllForOperator returns a snapshot of every adapter currently
// registered for the operator, keyed by protocol. Used by the router
// and health-fanout paths that must enumerate all active protocols
// for a given operator.
func (r *Registry) GetAllForOperator(operatorID uuid.UUID) map[string]Adapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]Adapter)
	for k, a := range r.adapters {
		if k.OperatorID == operatorID {
			out[k.Protocol] = a
		}
	}
	return out
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
	case "sba":
		return NewSBAAdapter(config)
	case "http":
		return NewHTTPAdapter(config)
	default:
		return NewMockAdapter(json.RawMessage(`{"latency_ms":10}`))
	}
}

package bus

import (
	"errors"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/severity"
	"github.com/google/uuid"
)

// CurrentEventVersion is the envelope schema version shipped with FIX-212.
// Consumers that see a different value (or missing field) invoke their legacy-shape shim.
const CurrentEventVersion = 1

// MaxDedupKeyLength matches the alerts.dedup_key column width (FIX-210).
const MaxDedupKeyLength = 255

// Envelope is the canonical schema for every argus NATS event in the FIX-212
// in-scope subject set. Publishers construct via NewEnvelope(...) and MUST set
// Type, TenantID, Severity, Source, Title. Consumers strict-parse and call
// Validate(); on failure, the consumer's legacy-shape shim handles the event
// and increments argus_events_legacy_shape_total{subject}.
type Envelope struct {
	EventVersion int                    `json:"event_version"`
	ID           string                 `json:"id"`
	Type         string                 `json:"type"`
	Timestamp    time.Time              `json:"timestamp"`
	TenantID     string                 `json:"tenant_id"`
	Severity     string                 `json:"severity"`
	Source       string                 `json:"source"`
	Title        string                 `json:"title"`
	Message      string                 `json:"message,omitempty"`
	Entity       *EntityRef             `json:"entity,omitempty"`
	DedupKey     *string                `json:"dedup_key,omitempty"`
	Meta         map[string]interface{} `json:"meta,omitempty"`
}

// EntityRef references a primary domain entity associated with an event.
// display_name is filled by the publisher (AC-6); subscribers fall back to id
// when display_name is absent.
type EntityRef struct {
	Type        string `json:"type"`
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
}

// Typed errors returned by Validate. Callers branch on errors.Is to increment
// the appropriate argus_events_invalid_total{reason} label.
var (
	ErrLegacyShape     = errors.New("bus: envelope has legacy event_version (expected current)")
	ErrInvalidSeverity = errors.New("bus: envelope severity is not canonical")
	ErrInvalidTenant   = errors.New("bus: envelope tenant_id is not a valid UUID")
	ErrMissingField    = errors.New("bus: envelope is missing a mandatory field")
	ErrInvalidEntity   = errors.New("bus: envelope entity has empty type or id")
	ErrDedupKeyTooLong = errors.New("bus: envelope dedup_key exceeds max length")
)

// Validate enforces mandatory envelope fields and returns a typed error on
// failure. A valid envelope returns nil.
func (e *Envelope) Validate() error {
	if e.EventVersion != CurrentEventVersion {
		return fmt.Errorf("%w: got %d want %d", ErrLegacyShape, e.EventVersion, CurrentEventVersion)
	}
	if e.ID == "" {
		return fmt.Errorf("%w: id", ErrMissingField)
	}
	if e.Type == "" {
		return fmt.Errorf("%w: type", ErrMissingField)
	}
	if e.Timestamp.IsZero() {
		return fmt.Errorf("%w: timestamp", ErrMissingField)
	}
	if e.TenantID == "" {
		return fmt.Errorf("%w: tenant_id", ErrMissingField)
	}
	if _, err := uuid.Parse(e.TenantID); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTenant, err)
	}
	if e.Severity == "" {
		return fmt.Errorf("%w: severity", ErrMissingField)
	}
	if err := severity.Validate(e.Severity); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidSeverity, e.Severity)
	}
	if e.Source == "" {
		return fmt.Errorf("%w: source", ErrMissingField)
	}
	if e.Title == "" {
		return fmt.Errorf("%w: title", ErrMissingField)
	}
	if e.Entity != nil {
		if e.Entity.Type == "" || e.Entity.ID == "" {
			return ErrInvalidEntity
		}
	}
	if e.DedupKey != nil && len(*e.DedupKey) > MaxDedupKeyLength {
		return ErrDedupKeyTooLong
	}
	return nil
}

// NewEnvelope constructs an Envelope pre-populated with EventVersion, a fresh
// UUID, and Timestamp=now.UTC(). Callers set remaining fields (Source, Title,
// etc.) via direct assignment or chainable builders (SetEntity, WithMeta).
func NewEnvelope(evtType, tenantID, sev string) *Envelope {
	return &Envelope{
		EventVersion: CurrentEventVersion,
		ID:           uuid.NewString(),
		Type:         evtType,
		Timestamp:    time.Now().UTC(),
		TenantID:     tenantID,
		Severity:     sev,
		Meta:         map[string]interface{}{},
	}
}

// SetEntity is a chainable builder that sets the primary entity reference.
// Empty displayName is permitted (subscriber falls back to id).
func (e *Envelope) SetEntity(entityType, id, displayName string) *Envelope {
	e.Entity = &EntityRef{Type: entityType, ID: id, DisplayName: displayName}
	return e
}

// WithMeta is a chainable builder that sets a single key on the Meta map,
// allocating the map on first call.
func (e *Envelope) WithMeta(key string, value interface{}) *Envelope {
	if e.Meta == nil {
		e.Meta = map[string]interface{}{}
	}
	e.Meta[key] = value
	return e
}

// WithSource sets the Source field and returns the envelope for chaining.
func (e *Envelope) WithSource(source string) *Envelope {
	e.Source = source
	return e
}

// WithTitle sets the Title field and returns the envelope for chaining.
func (e *Envelope) WithTitle(title string) *Envelope {
	e.Title = title
	return e
}

// WithMessage sets the Message field and returns the envelope for chaining.
func (e *Envelope) WithMessage(message string) *Envelope {
	e.Message = message
	return e
}

// WithDedupKey sets the DedupKey pointer. Pass empty string to clear.
func (e *Envelope) WithDedupKey(key string) *Envelope {
	if key == "" {
		e.DedupKey = nil
		return e
	}
	e.DedupKey = &key
	return e
}

package adapter

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

var (
	ErrAdapterNotFound     = errors.New("adapter not found")
	ErrCircuitOpen         = errors.New("circuit breaker open")
	ErrAdapterTimeout      = errors.New("adapter timeout")
	ErrUnsupportedProtocol = errors.New("unsupported protocol")
)

const (
	AcctStart  = "start"
	AcctInterim = "interim"
	AcctStop   = "stop"
)

type AdapterError struct {
	OperatorID   uuid.UUID
	ProtocolType string
	Err          error
}

func (e *AdapterError) Error() string {
	return fmt.Sprintf("adapter [%s]: %v", e.ProtocolType, e.Err)
}

func (e *AdapterError) Unwrap() error {
	return e.Err
}

type Adapter interface {
	Type() string
	HealthCheck(ctx context.Context) HealthResult
	ForwardAuth(ctx context.Context, req AuthRequest) (*AuthResponse, error)
	ForwardAcct(ctx context.Context, req AcctRequest) error
	SendCoA(ctx context.Context, req CoARequest) error
	SendDM(ctx context.Context, req DMRequest) error
}

const (
	AuthAccept = "accept"
	AuthReject = "reject"
)

type HealthResult struct {
	Success   bool   `json:"success"`
	LatencyMs int    `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

type AuthRequest struct {
	IMSI      string
	MSISDN    string
	NASId     string
	NASIP     string
	APN       string
	SessionID string
}

type AuthResponse struct {
	Code           string
	FramedIP       string
	SessionTimeout int
	IdleTimeout    int
	FilterID       string
	Attributes     map[string]interface{}
}

type AcctRequest struct {
	IMSI          string
	SessionID     string
	StatusType    string
	InputOctets   uint64
	OutputOctets  uint64
	SessionTime   int
	NASIP         string
}

type CoARequest struct {
	NASIP          string
	NASCoAPort     int
	SessionID      string
	IMSI           string
	SessionTimeout *int
	Attributes     map[string]interface{}
}

type DMRequest struct {
	NASIP      string
	NASCoAPort int
	SessionID  string
	IMSI       string
}

func WrapError(operatorID uuid.UUID, protocolType string, err error) error {
	return fmt.Errorf("operator %s [%s]: %w", operatorID, protocolType, err)
}

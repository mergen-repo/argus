package smsr

import (
	"context"
	"errors"
	"time"
)

var (
	ErrSMSRConnectionFailed = errors.New("smsr: connection failed")
	ErrSMSRRateLimit        = errors.New("smsr: rate limit exceeded")
	ErrSMSRRejected         = errors.New("smsr: command rejected by SM-SR")
)

type CommandType string

const (
	CommandTypeEnable  CommandType = "enable"
	CommandTypeDisable CommandType = "disable"
	CommandTypeSwitch  CommandType = "switch"
	CommandTypeDelete  CommandType = "delete"
)

type PushRequest struct {
	EID           string
	CommandType   CommandType
	SourceProfile string
	TargetProfile string
	CommandID     string
	CorrelationID string
}

type PushResponse struct {
	SMSRCommandID string
	AcceptedAt    time.Time
}

type Client interface {
	Push(ctx context.Context, req PushRequest) (PushResponse, error)
	Health(ctx context.Context) error
}

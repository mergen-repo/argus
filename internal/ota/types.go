package ota

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type CommandType string

const (
	CmdUpdateFile    CommandType = "UPDATE_FILE"
	CmdInstallApplet CommandType = "INSTALL_APPLET"
	CmdDeleteApplet  CommandType = "DELETE_APPLET"
	CmdReadFile      CommandType = "READ_FILE"
	CmdSIMToolkit    CommandType = "SIM_TOOLKIT"
)

var ValidCommandTypes = map[CommandType]bool{
	CmdUpdateFile:    true,
	CmdInstallApplet: true,
	CmdDeleteApplet:  true,
	CmdReadFile:      true,
	CmdSIMToolkit:    true,
}

type DeliveryChannel string

const (
	ChannelSMSPP DeliveryChannel = "sms_pp"
	ChannelBIP   DeliveryChannel = "bip"
)

var ValidChannels = map[DeliveryChannel]bool{
	ChannelSMSPP: true,
	ChannelBIP:   true,
}

type CommandStatus string

const (
	StatusQueued    CommandStatus = "queued"
	StatusSent      CommandStatus = "sent"
	StatusDelivered CommandStatus = "delivered"
	StatusExecuted  CommandStatus = "executed"
	StatusConfirmed CommandStatus = "confirmed"
	StatusFailed    CommandStatus = "failed"
)

type SecurityMode string

const (
	SecurityNone    SecurityMode = "none"
	SecurityKIC     SecurityMode = "kic"
	SecurityKID     SecurityMode = "kid"
	SecurityKICKID  SecurityMode = "kic_kid"
)

type OTACommand struct {
	ID              uuid.UUID       `json:"id"`
	TenantID        uuid.UUID       `json:"tenant_id"`
	SimID           uuid.UUID       `json:"sim_id"`
	CommandType     CommandType     `json:"command_type"`
	Channel         DeliveryChannel `json:"channel"`
	Status          CommandStatus   `json:"status"`
	APDUData        []byte          `json:"apdu_data"`
	SecurityMode    SecurityMode    `json:"security_mode"`
	Payload         json.RawMessage `json:"payload"`
	ResponseData    json.RawMessage `json:"response_data,omitempty"`
	ErrorMessage    *string         `json:"error_message,omitempty"`
	JobID           *uuid.UUID      `json:"job_id,omitempty"`
	RetryCount      int             `json:"retry_count"`
	MaxRetries      int             `json:"max_retries"`
	CreatedBy       *uuid.UUID      `json:"created_by,omitempty"`
	SentAt          *time.Time      `json:"sent_at,omitempty"`
	DeliveredAt     *time.Time      `json:"delivered_at,omitempty"`
	ExecutedAt      *time.Time      `json:"executed_at,omitempty"`
	CompletedAt     *time.Time      `json:"completed_at,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

type CreateCommandParams struct {
	SimID        uuid.UUID       `json:"sim_id"`
	CommandType  CommandType     `json:"command_type"`
	Channel      DeliveryChannel `json:"channel"`
	SecurityMode SecurityMode    `json:"security_mode"`
	Payload      json.RawMessage `json:"payload"`
	MaxRetries   int             `json:"max_retries"`
	CreatedBy    *uuid.UUID      `json:"created_by,omitempty"`
	JobID        *uuid.UUID      `json:"job_id,omitempty"`
}

type BulkOTAPayload struct {
	SimIDs       []string        `json:"sim_ids"`
	SegmentID    *string         `json:"segment_id,omitempty"`
	CommandType  CommandType     `json:"command_type"`
	Channel      DeliveryChannel `json:"channel"`
	SecurityMode SecurityMode    `json:"security_mode"`
	Payload      json.RawMessage `json:"payload"`
	MaxRetries   int             `json:"max_retries"`
}

type BulkOTAResult struct {
	TotalSIMs    int `json:"total_sims"`
	QueuedCount  int `json:"queued_count"`
	FailedCount  int `json:"failed_count"`
}

func (ct CommandType) Validate() error {
	if !ValidCommandTypes[ct] {
		return fmt.Errorf("invalid command type: %s", ct)
	}
	return nil
}

func (ch DeliveryChannel) Validate() error {
	if !ValidChannels[ch] {
		return fmt.Errorf("invalid delivery channel: %s", ch)
	}
	return nil
}

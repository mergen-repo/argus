package job

import (
	"testing"

	"github.com/rs/zerolog"
)

func TestS3ArchivalProcessor_Type(t *testing.T) {
	p := &S3ArchivalProcessor{}
	if p.Type() != JobTypeS3Archival {
		t.Errorf("expected type %s, got %s", JobTypeS3Archival, p.Type())
	}
}

func TestS3ArchivalPayload_Defaults(t *testing.T) {
	payload := s3ArchivalPayload{}
	if payload.HypertableName != "" {
		t.Errorf("expected empty hypertable name, got %s", payload.HypertableName)
	}
	if payload.DaysOlderThan != 0 {
		t.Errorf("expected 0 days older than, got %d", payload.DaysOlderThan)
	}
}

func TestS3ArchivalProcessor_Constructor(t *testing.T) {
	p := NewS3ArchivalProcessor(nil, nil, nil, nil, nil, nil, "test-bucket", zerolog.Nop())
	if p.defaultBucket != "test-bucket" {
		t.Errorf("expected default bucket test-bucket, got %s", p.defaultBucket)
	}
}

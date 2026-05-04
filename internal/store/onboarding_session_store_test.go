package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestOnboardingSessionStore_New(t *testing.T) {
	s := NewOnboardingSessionStore(nil)
	if s == nil {
		t.Fatal("expected non-nil OnboardingSessionStore")
	}
}

func TestOnboardingSessionStore_UpdateStep_InvalidStepN(t *testing.T) {
	s := NewOnboardingSessionStore(nil)
	ctx := context.Background()
	id := uuid.New()

	for _, bad := range []int{0, 6, -1, 100} {
		err := s.UpdateStep(ctx, id, bad, []byte(`{}`), 2)
		if err == nil {
			t.Errorf("stepN=%d: expected error, got nil", bad)
		}
	}
}

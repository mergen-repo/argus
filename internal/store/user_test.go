package store

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestUserStructNewFields(t *testing.T) {
	now := time.Now()
	u := User{
		ID:                     uuid.New(),
		TenantID:               uuid.New(),
		Email:                  "user@test.com",
		PasswordHash:           "hash",
		Name:                   "Test User",
		Role:                   "admin",
		TOTPEnabled:            false,
		State:                  "active",
		FailedLoginCount:       0,
		PasswordChangeRequired: true,
		PasswordChangedAt:      &now,
		CreatedAt:              now,
		UpdatedAt:              now,
	}

	if !u.PasswordChangeRequired {
		t.Error("PasswordChangeRequired should be true")
	}
	if u.PasswordChangedAt == nil {
		t.Fatal("PasswordChangedAt should not be nil")
	}
	if !u.PasswordChangedAt.Equal(now) {
		t.Errorf("PasswordChangedAt = %v, want %v", u.PasswordChangedAt, now)
	}
}

func TestUserStructPasswordChangeRequiredDefault(t *testing.T) {
	u := User{}
	if u.PasswordChangeRequired {
		t.Error("PasswordChangeRequired default should be false")
	}
	if u.PasswordChangedAt != nil {
		t.Error("PasswordChangedAt default should be nil")
	}
}

func TestUserStructLockedUntilNilable(t *testing.T) {
	u := User{
		ID:               uuid.New(),
		FailedLoginCount: 3,
	}
	if u.LockedUntil != nil {
		t.Error("LockedUntil should be nil")
	}
	future := time.Now().Add(15 * time.Minute)
	u.LockedUntil = &future
	if u.LockedUntil == nil {
		t.Error("LockedUntil should be set")
	}
}

func TestCreateUserParams(t *testing.T) {
	p := CreateUserParams{
		Email: "new@test.com",
		Name:  "New User",
		Role:  "viewer",
	}
	if p.Email != "new@test.com" {
		t.Errorf("Email = %q, want %q", p.Email, "new@test.com")
	}
	if p.Role != "viewer" {
		t.Errorf("Role = %q, want %q", p.Role, "viewer")
	}
}

func TestUpdateUserParams(t *testing.T) {
	name := "Updated"
	role := "admin"
	state := "suspended"
	p := UpdateUserParams{
		Name:  &name,
		Role:  &role,
		State: &state,
	}
	if p.Name == nil || *p.Name != "Updated" {
		t.Error("Name should be set to 'Updated'")
	}
	if p.Role == nil || *p.Role != "admin" {
		t.Error("Role should be set to 'admin'")
	}
	if p.State == nil || *p.State != "suspended" {
		t.Error("State should be set to 'suspended'")
	}
}

func TestErrUserNotFound(t *testing.T) {
	if ErrUserNotFound.Error() != "store: user not found" {
		t.Errorf("ErrUserNotFound = %q", ErrUserNotFound.Error())
	}
}

func TestErrSessionNotFound(t *testing.T) {
	if ErrSessionNotFound.Error() != "store: session not found" {
		t.Errorf("ErrSessionNotFound = %q", ErrSessionNotFound.Error())
	}
}

func TestErrEmailExists(t *testing.T) {
	if ErrEmailExists.Error() != "store: email already exists in tenant" {
		t.Errorf("ErrEmailExists = %q", ErrEmailExists.Error())
	}
}

package store

import (
	"testing"
)

func TestTenantStruct(t *testing.T) {
	tenant := Tenant{
		Name:         "Test Tenant",
		ContactEmail: "admin@test.com",
		MaxSims:      100000,
		MaxApns:      100,
		MaxUsers:     50,
		MaxAPIKeys:   20,
		State:        "active",
	}

	if tenant.Name != "Test Tenant" {
		t.Errorf("Name = %q, want %q", tenant.Name, "Test Tenant")
	}
	if tenant.MaxUsers != 50 {
		t.Errorf("MaxUsers = %d, want %d", tenant.MaxUsers, 50)
	}
	if tenant.MaxAPIKeys != 20 {
		t.Errorf("MaxAPIKeys = %d, want %d", tenant.MaxAPIKeys, 20)
	}
}

func TestTenantMaxAPIKeysDefault(t *testing.T) {
	tenant := Tenant{}
	if tenant.MaxAPIKeys != 0 {
		t.Errorf("MaxAPIKeys zero-value = %d, want 0", tenant.MaxAPIKeys)
	}
}

func TestCreateTenantParamsDefaults(t *testing.T) {
	p := CreateTenantParams{
		Name:         "Test",
		ContactEmail: "admin@test.com",
	}

	if p.MaxSims != nil {
		t.Error("MaxSims should be nil (default applied in Create)")
	}
	if p.MaxApns != nil {
		t.Error("MaxApns should be nil (default applied in Create)")
	}
	if p.MaxUsers != nil {
		t.Error("MaxUsers should be nil (default applied in Create)")
	}
}

func TestUpdateTenantParamsOptional(t *testing.T) {
	name := "Updated Name"
	p := UpdateTenantParams{
		Name: &name,
	}

	if p.Name == nil || *p.Name != "Updated Name" {
		t.Error("Name should be set")
	}
	if p.ContactEmail != nil {
		t.Error("ContactEmail should be nil")
	}
	if p.State != nil {
		t.Error("State should be nil")
	}
}

func TestTenantStatsDefaults(t *testing.T) {
	stats := TenantStats{}

	if stats.SimCount != 0 {
		t.Errorf("SimCount = %d, want 0", stats.SimCount)
	}
	if stats.UserCount != 0 {
		t.Errorf("UserCount = %d, want 0", stats.UserCount)
	}
	if stats.APNCount != 0 {
		t.Errorf("APNCount = %d, want 0", stats.APNCount)
	}
	if stats.ActiveSessions != 0 {
		t.Errorf("ActiveSessions = %d, want 0", stats.ActiveSessions)
	}
}

func TestErrDomainExists(t *testing.T) {
	if ErrDomainExists.Error() != "store: domain already exists" {
		t.Errorf("ErrDomainExists = %q", ErrDomainExists.Error())
	}
}

func TestErrTenantNotFound(t *testing.T) {
	if ErrTenantNotFound.Error() != "store: tenant not found" {
		t.Errorf("ErrTenantNotFound = %q", ErrTenantNotFound.Error())
	}
}

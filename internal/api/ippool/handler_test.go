package ippool

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func withTenantCtx(ctx context.Context) context.Context {
	return context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
}

func TestToPoolResponse(t *testing.T) {
	cidrV4 := "10.0.0.0/24"
	p := &store.IPPool{
		ID:                      uuid.New(),
		TenantID:                uuid.New(),
		APNID:                   uuid.New(),
		Name:                    "pool-1",
		CIDRv4:                  &cidrV4,
		TotalAddresses:          254,
		UsedAddresses:           50,
		AlertThresholdWarning:   80,
		AlertThresholdCritical:  90,
		ReclaimGracePeriodDays:  7,
		State:                   "active",
		CreatedAt:               time.Now(),
	}

	resp := toPoolResponse(p)

	if resp.ID != p.ID.String() {
		t.Errorf("ID = %q, want %q", resp.ID, p.ID.String())
	}
	if resp.Name != "pool-1" {
		t.Errorf("Name = %q, want %q", resp.Name, "pool-1")
	}
	if resp.CIDRv4 == nil || *resp.CIDRv4 != "10.0.0.0/24" {
		t.Error("CIDRv4 should be '10.0.0.0/24'")
	}
	if resp.CIDRv6 != nil {
		t.Error("CIDRv6 should be nil")
	}
	if resp.TotalAddresses != 254 {
		t.Errorf("TotalAddresses = %d, want 254", resp.TotalAddresses)
	}
	if resp.UsedAddresses != 50 {
		t.Errorf("UsedAddresses = %d, want 50", resp.UsedAddresses)
	}
	expectedUtil := float64(50) / float64(254) * 100.0
	if resp.UtilizationPct != expectedUtil {
		t.Errorf("UtilizationPct = %f, want %f", resp.UtilizationPct, expectedUtil)
	}
	if resp.State != "active" {
		t.Errorf("State = %q, want %q", resp.State, "active")
	}
}

func TestToPoolResponseZeroTotal(t *testing.T) {
	p := &store.IPPool{
		ID:             uuid.New(),
		TotalAddresses: 0,
		UsedAddresses:  0,
		CreatedAt:      time.Now(),
	}

	resp := toPoolResponse(p)
	if resp.UtilizationPct != 0.0 {
		t.Errorf("UtilizationPct = %f, want 0.0 when total is 0", resp.UtilizationPct)
	}
}

func TestToAddressResponse(t *testing.T) {
	simID := uuid.New()
	now := time.Now()
	addrV4 := "10.0.0.1"

	a := &store.IPAddress{
		ID:             uuid.New(),
		PoolID:         uuid.New(),
		AddressV4:      &addrV4,
		AllocationType: "static",
		SimID:          &simID,
		State:          "reserved",
		AllocatedAt:    &now,
	}

	resp := toAddressResponse(a)

	if resp.ID != a.ID.String() {
		t.Errorf("ID = %q, want %q", resp.ID, a.ID.String())
	}
	if resp.AddressV4 == nil || *resp.AddressV4 != "10.0.0.1" {
		t.Error("AddressV4 should be '10.0.0.1'")
	}
	if resp.AddressV6 != nil {
		t.Error("AddressV6 should be nil")
	}
	if resp.AllocationType != "static" {
		t.Errorf("AllocationType = %q, want %q", resp.AllocationType, "static")
	}
	if resp.SimID == nil || *resp.SimID != simID.String() {
		t.Error("SimID should match")
	}
	if resp.State != "reserved" {
		t.Errorf("State = %q, want %q", resp.State, "reserved")
	}
	if resp.AllocatedAt == nil {
		t.Error("AllocatedAt should not be nil")
	}
	if resp.ReclaimAt != nil {
		t.Error("ReclaimAt should be nil when not set")
	}
}

func TestToAddressResponseNilFields(t *testing.T) {
	a := &store.IPAddress{
		ID:             uuid.New(),
		PoolID:         uuid.New(),
		AllocationType: "dynamic",
		State:          "available",
	}

	resp := toAddressResponse(a)

	if resp.AddressV4 != nil {
		t.Error("AddressV4 should be nil")
	}
	if resp.AddressV6 != nil {
		t.Error("AddressV6 should be nil")
	}
	if resp.SimID != nil {
		t.Error("SimID should be nil")
	}
	if resp.AllocatedAt != nil {
		t.Error("AllocatedAt should be nil")
	}
	if resp.ReclaimAt != nil {
		t.Error("ReclaimAt should be nil")
	}
}

func TestCreatePoolValidation(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode int
	}{
		{
			name:     "empty body",
			body:     `{}`,
			wantCode: 422,
		},
		{
			name:     "missing apn_id",
			body:     `{"name":"pool","cidr_v4":"10.0.0.0/24"}`,
			wantCode: 422,
		},
		{
			name:     "missing name",
			body:     `{"apn_id":"` + uuid.New().String() + `","cidr_v4":"10.0.0.0/24"}`,
			wantCode: 422,
		},
		{
			name:     "no CIDR provided",
			body:     `{"apn_id":"` + uuid.New().String() + `","name":"pool"}`,
			wantCode: 422,
		},
		{
			name:     "invalid cidr_v4",
			body:     `{"apn_id":"` + uuid.New().String() + `","name":"pool","cidr_v4":"not-a-cidr"}`,
			wantCode: 422,
		},
		{
			name:     "invalid cidr_v6",
			body:     `{"apn_id":"` + uuid.New().String() + `","name":"pool","cidr_v6":"not-a-cidr"}`,
			wantCode: 422,
		},
		{
			name:     "alert_threshold_warning out of range",
			body:     `{"apn_id":"` + uuid.New().String() + `","name":"pool","cidr_v4":"10.0.0.0/24","alert_threshold_warning":150}`,
			wantCode: 422,
		},
		{
			name:     "alert_threshold_critical out of range",
			body:     `{"apn_id":"` + uuid.New().String() + `","name":"pool","cidr_v4":"10.0.0.0/24","alert_threshold_critical":-1}`,
			wantCode: 422,
		},
		{
			name:     "name too long",
			body:     `{"apn_id":"` + uuid.New().String() + `","name":"` + strings.Repeat("x", 101) + `","cidr_v4":"10.0.0.0/24"}`,
			wantCode: 422,
		},
		{
			name:     "invalid json",
			body:     `not json`,
			wantCode: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{
				logger: zerolog.Nop(),
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/ip-pools", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			ctx := withTenantCtx(req.Context())
			req = req.WithContext(ctx)
			w := httptest.NewRecorder()

			h.Create(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("Create(%s) status = %d, want %d, body: %s", tt.name, w.Code, tt.wantCode, w.Body.String())
			}
		})
	}
}

func TestUpdatePoolValidation(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		body     string
		wantCode int
	}{
		{
			name:     "invalid id format",
			id:       "not-a-uuid",
			body:     `{"name":"test"}`,
			wantCode: 400,
		},
		{
			name:     "invalid json",
			id:       uuid.New().String(),
			body:     `not json`,
			wantCode: 400,
		},
		{
			name:     "invalid state",
			id:       uuid.New().String(),
			body:     `{"state":"deleted"}`,
			wantCode: 422,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{
				logger: zerolog.Nop(),
			}

			req := httptest.NewRequest(http.MethodPatch, "/api/v1/ip-pools/"+tt.id, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			ctx := withTenantCtx(req.Context())
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", tt.id)
			ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
			req = req.WithContext(ctx)
			w := httptest.NewRecorder()

			h.Update(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("Update(%s) status = %d, want %d, body: %s", tt.name, w.Code, tt.wantCode, w.Body.String())
			}
		})
	}
}

func TestReserveIPValidation(t *testing.T) {
	tests := []struct {
		name     string
		poolID   string
		body     string
		wantCode int
	}{
		{
			name:     "invalid pool id",
			poolID:   "not-a-uuid",
			body:     `{"sim_id":"` + uuid.New().String() + `"}`,
			wantCode: 400,
		},
		{
			name:     "missing sim_id",
			poolID:   uuid.New().String(),
			body:     `{}`,
			wantCode: 422,
		},
		{
			name:     "invalid json",
			poolID:   uuid.New().String(),
			body:     `not json`,
			wantCode: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{
				logger: zerolog.Nop(),
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/ip-pools/"+tt.poolID+"/addresses/reserve", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			ctx := withTenantCtx(req.Context())
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", tt.poolID)
			ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
			req = req.WithContext(ctx)
			w := httptest.NewRecorder()

			h.ReserveIP(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("ReserveIP(%s) status = %d, want %d, body: %s", tt.name, w.Code, tt.wantCode, w.Body.String())
			}
		})
	}
}

func TestIPv4Generation(t *testing.T) {
	tests := []struct {
		name     string
		cidr     string
		wantLen  int
		wantErr  bool
	}{
		{
			name:    "/24 network",
			cidr:    "10.0.0.0/24",
			wantLen: 254,
		},
		{
			name:    "/30 network",
			cidr:    "10.0.0.0/30",
			wantLen: 2,
		},
		{
			name:    "/31 point-to-point",
			cidr:    "10.0.0.0/31",
			wantLen: 2,
		},
		{
			name:    "/32 single host",
			cidr:    "10.0.0.1/32",
			wantLen: 1,
		},
		{
			name:    "/28 network",
			cidr:    "192.168.1.0/28",
			wantLen: 14,
		},
		{
			name:    "invalid cidr",
			cidr:    "not-a-cidr",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addrs, err := store.GenerateIPv4Addresses(tt.cidr)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(addrs) != tt.wantLen {
				t.Errorf("GenerateIPv4Addresses(%s) len = %d, want %d", tt.cidr, len(addrs), tt.wantLen)
			}
		})
	}
}

func TestIPv6Generation(t *testing.T) {
	tests := []struct {
		name     string
		cidr     string
		wantLen  int
		wantErr  bool
	}{
		{
			name:    "/120 network",
			cidr:    "2001:db8::/120",
			wantLen: 256,
		},
		{
			name:    "/126 network",
			cidr:    "2001:db8::/126",
			wantLen: 4,
		},
		{
			name:    "invalid cidr",
			cidr:    "not-a-cidr",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addrs, total, err := store.GenerateIPv6Addresses(tt.cidr)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(addrs) != tt.wantLen {
				t.Errorf("GenerateIPv6Addresses(%s) len = %d, want %d", tt.cidr, len(addrs), tt.wantLen)
			}
			if total != tt.wantLen {
				t.Errorf("GenerateIPv6Addresses(%s) total = %d, want %d", tt.cidr, total, tt.wantLen)
			}
		})
	}
}

func TestValidPoolStates(t *testing.T) {
	if !validPoolStates["active"] {
		t.Error("active should be valid")
	}
	if !validPoolStates["disabled"] {
		t.Error("disabled should be valid")
	}
	if validPoolStates["exhausted"] {
		t.Error("exhausted should not be directly settable")
	}
	if validPoolStates["deleted"] {
		t.Error("deleted should not be valid")
	}
}

func TestGetPoolInvalidID(t *testing.T) {
	h := &Handler{
		logger: zerolog.Nop(),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ip-pools/not-uuid", nil)
	ctx := withTenantCtx(req.Context())
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "not-uuid")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != 400 {
		t.Errorf("Get(invalid id) status = %d, want 400", w.Code)
	}
}

func TestListAddressesInvalidID(t *testing.T) {
	h := &Handler{
		logger: zerolog.Nop(),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ip-pools/not-uuid/addresses", nil)
	ctx := withTenantCtx(req.Context())
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "not-uuid")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.ListAddresses(w, req)

	if w.Code != 400 {
		t.Errorf("ListAddresses(invalid id) status = %d, want 400", w.Code)
	}
}

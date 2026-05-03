package dsl

import (
	"testing"
)

// STORY-094 Phase 11 — DSL device & SIM-binding field evaluation.
//
// These tests cover:
//   - tac() helper (length guard + happy path)
//   - The 8 STORY-094 condition-field cases in getConditionFieldValue
//   - The device.imei_in_pool() placeholder (always false; STORY-095
//     wires the real lookup against the pool table)
//   - AC-13 end-to-end policy evaluation: a strict/tac-lock binding
//     with binding_status="mismatch" rejects; soft mode or verified
//     status allows.

func TestTac_15Digits(t *testing.T) {
	got := tac("359211089765432")
	if got != "35921108" {
		t.Errorf("tac(15-digit IMEI): got %q, want %q", got, "35921108")
	}
}

func TestTac_Empty(t *testing.T) {
	if got := tac(""); got != "" {
		t.Errorf("tac(empty): got %q, want empty", got)
	}
}

func TestTac_TooShort(t *testing.T) {
	if got := tac("123"); got != "" {
		t.Errorf("tac(too-short): got %q, want empty", got)
	}
}

func TestTac_TooLong(t *testing.T) {
	// 16 digits — also rejected (defensive length check).
	if got := tac("3592110897654321"); got != "" {
		t.Errorf("tac(too-long): got %q, want empty", got)
	}
}

func TestEvaluator_DeviceFields_Resolved(t *testing.T) {
	ctx := SessionContext{
		IMEI:              "359211089765432",
		SoftwareVersion:   "23",
		BindingMode:       "strict",
		BoundIMEI:         "359211089765432",
		BindingStatus:     "verified",
		BindingVerifiedAt: "2026-04-30T12:00:00Z",
	}

	e := NewEvaluator()

	tests := []struct {
		name  string
		field string
		want  interface{}
	}{
		{"device.imei", "device.imei", "359211089765432"},
		{"device.imeisv", "device.imeisv", "35921108976543223"},
		{"device.software_version", "device.software_version", "23"},
		{"device.tac", "device.tac", "35921108"},
		{"device.binding_status", "device.binding_status", "verified"},
		{"sim.binding_mode", "sim.binding_mode", "strict"},
		{"sim.bound_imei", "sim.bound_imei", "359211089765432"},
		{"sim.binding_verified_at", "sim.binding_verified_at", "2026-04-30T12:00:00Z"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := e.getConditionFieldValue(ctx, tt.field)
			if got != tt.want {
				t.Errorf("getConditionFieldValue(%q): got %v (%T), want %v (%T)",
					tt.field, got, got, tt.want, tt.want)
			}
		})
	}
}

func TestEvaluator_DeviceImeisv_EmptyWhenIncomplete(t *testing.T) {
	e := NewEvaluator()

	// IMEI present, SV missing → empty (cannot synthesize partial IMEISV).
	ctx := SessionContext{IMEI: "359211089765432", SoftwareVersion: ""}
	if got := e.getConditionFieldValue(ctx, "device.imeisv"); got != "" {
		t.Errorf("imei without SV: got %v, want empty", got)
	}

	// SV present, IMEI missing → empty.
	ctx = SessionContext{IMEI: "", SoftwareVersion: "23"}
	if got := e.getConditionFieldValue(ctx, "device.imeisv"); got != "" {
		t.Errorf("SV without IMEI: got %v, want empty", got)
	}

	// Both empty → empty.
	ctx = SessionContext{}
	if got := e.getConditionFieldValue(ctx, "device.imeisv"); got != "" {
		t.Errorf("both empty: got %v, want empty", got)
	}
}

func TestEvaluator_DeviceTac_DerivedFromCtxIMEI(t *testing.T) {
	e := NewEvaluator()

	// Valid 15-digit IMEI → first 8 digits.
	ctx := SessionContext{IMEI: "490154203237518"}
	if got := e.getConditionFieldValue(ctx, "device.tac"); got != "49015420" {
		t.Errorf("device.tac valid IMEI: got %v, want %q", got, "49015420")
	}

	// Empty IMEI → empty TAC.
	ctx = SessionContext{}
	if got := e.getConditionFieldValue(ctx, "device.tac"); got != "" {
		t.Errorf("device.tac empty IMEI: got %v, want empty", got)
	}
}

func TestEvaluator_DeviceImeiInPool_Placeholder(t *testing.T) {
	e := NewEvaluator()
	ctx := SessionContext{IMEI: "359211089765432"}

	// Placeholder ALWAYS returns false in STORY-094. STORY-095 wires
	// real lookup. Verify across the canonical pool names plus an
	// arbitrary one to lock in the placeholder semantics.
	pools := []string{"whitelist", "greylist", "blacklist", "custom-pool", ""}
	for _, pool := range pools {
		t.Run("pool="+pool, func(t *testing.T) {
			field := "device.imei_in_pool(" + pool + ")"
			got := e.getConditionFieldValue(ctx, field)
			b, ok := got.(bool)
			if !ok {
				t.Fatalf("device.imei_in_pool(%q): got %v (%T), want bool", pool, got, got)
			}
			if b != false {
				t.Errorf("device.imei_in_pool(%q): got %v, want false (placeholder)", pool, b)
			}
		})
	}
}

func TestEvaluator_TacFunctionCall_Dispatch(t *testing.T) {
	// Field encoding produced by the parser for `tac(device.imei)`.
	e := NewEvaluator()
	ctx := SessionContext{IMEI: "359211089765432"}

	got := e.getConditionFieldValue(ctx, "tac(device.imei)")
	if got != "35921108" {
		t.Errorf("tac(device.imei) dispatch: got %v, want %q", got, "35921108")
	}

	// Empty IMEI → tac returns empty.
	ctx = SessionContext{}
	got = e.getConditionFieldValue(ctx, "tac(device.imei)")
	if got != "" {
		t.Errorf("tac(empty): got %v, want empty", got)
	}
}

func TestEvaluator_AC13_FullPolicy(t *testing.T) {
	// AC-13: WHEN device.binding_status == "mismatch"
	//   AND  sim.binding_mode IN ("strict","tac-lock") THEN reject (block).
	src := `POLICY "ac-13-binding-enforce" {
    MATCH { apn = "iot.data" }
    RULES {
        WHEN device.binding_status = "mismatch" AND sim.binding_mode IN ("strict", "tac-lock") {
            ACTION block()
        }
    }
}`

	compiled, errs, err := CompileSource(src)
	if err != nil {
		t.Fatalf("CompileSource: %v", err)
	}
	for _, e := range errs {
		if e.Severity == "error" {
			t.Fatalf("compile error: %s", e.Error())
		}
	}

	tests := []struct {
		name      string
		ctx       SessionContext
		wantAllow bool
		wantMatch int
	}{
		{
			name: "mismatch_strict_rejects",
			ctx: SessionContext{
				APN:           "iot.data",
				BindingStatus: "mismatch",
				BindingMode:   "strict",
			},
			wantAllow: false,
			wantMatch: 1,
		},
		{
			name: "mismatch_taclock_rejects",
			ctx: SessionContext{
				APN:           "iot.data",
				BindingStatus: "mismatch",
				BindingMode:   "tac-lock",
			},
			wantAllow: false,
			wantMatch: 1,
		},
		{
			name: "verified_strict_allows",
			ctx: SessionContext{
				APN:           "iot.data",
				BindingStatus: "verified",
				BindingMode:   "strict",
			},
			wantAllow: true,
			wantMatch: 0,
		},
		{
			name: "mismatch_soft_allows",
			ctx: SessionContext{
				APN:           "iot.data",
				BindingStatus: "mismatch",
				BindingMode:   "soft",
			},
			wantAllow: true,
			wantMatch: 0,
		},
	}

	e := NewEvaluator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := e.Evaluate(tt.ctx, compiled)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if res.Allow != tt.wantAllow {
				t.Errorf("Allow: got %v, want %v", res.Allow, tt.wantAllow)
			}
			if res.MatchedRules != tt.wantMatch {
				t.Errorf("MatchedRules: got %d, want %d", res.MatchedRules, tt.wantMatch)
			}
		})
	}
}

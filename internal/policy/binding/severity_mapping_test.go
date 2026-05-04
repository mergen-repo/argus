package binding

// severity_mapping_test.go — STORY-097 Task 6 Part B.
//
// Table-driven tests for AC-5: each binding mode + scenario combination
// produces the correct Severity and Reason on the returned Verdict.
//
// Fixtures (makeSIM, makeSession, mockAllowlist, mockBlacklist,
// fixedClock, fixedNow, imeiA, imeiB, ptrStr, ptrTime) are package-level
// symbols defined in enforcer_test.go.

import (
	"context"
	"testing"
	"time"
)

func TestSeverityMapping_Table(t *testing.T) {
	cases := []struct {
		name           string
		bindingMode    string
		observedIMEI   string
		boundIMEI      *string
		graceExpires   *time.Time
		useBlacklist   bool
		useAllowlist   bool
		allowlistHit   bool
		expectedSev    Severity
		expectedReason string
		expectedKind   VerdictKind
	}{
		// strict + mismatch → High / BINDING_MISMATCH_STRICT / Reject
		{
			name:           "strict-mismatch",
			bindingMode:    "strict",
			observedIMEI:   imeiB,
			boundIMEI:      ptrStr(imeiA),
			expectedSev:    SeverityHigh,
			expectedReason: RejectReasonMismatchStrict,
			expectedKind:   VerdictReject,
		},
		// strict + blacklist override → High / BINDING_BLACKLIST / Reject
		// (blacklist runs before mode; mode is irrelevant for the blacklist path)
		{
			name:           "strict-blacklist",
			bindingMode:    "strict",
			observedIMEI:   imeiA,
			boundIMEI:      ptrStr(imeiA),
			useBlacklist:   true,
			expectedSev:    SeverityHigh,
			expectedReason: RejectReasonBlacklist,
			expectedKind:   VerdictReject,
		},
		// allowlist + not in list → High / BINDING_MISMATCH_ALLOWLIST / Reject
		{
			name:           "allowlist-mismatch",
			bindingMode:    "allowlist",
			observedIMEI:   imeiA,
			boundIMEI:      nil,
			useAllowlist:   true,
			allowlistHit:   false,
			expectedSev:    SeverityHigh,
			expectedReason: RejectReasonMismatchAllowlist,
			expectedKind:   VerdictReject,
		},
		// tac-lock + different TAC → Medium / BINDING_MISMATCH_TAC / Reject
		{
			name:           "tac-lock-mismatch",
			bindingMode:    "tac-lock",
			observedIMEI:   imeiB,
			boundIMEI:      ptrStr(imeiA),
			expectedSev:    SeverityMedium,
			expectedReason: RejectReasonMismatchTAC,
			expectedKind:   VerdictReject,
		},
		// grace-period + expired window + differ → High / BINDING_GRACE_EXPIRED / Reject
		// NOTE: dispatch spec listed SeverityMedium but enforcer.go:299 uses
		// rejectMismatch(RejectReasonGraceExpired, SeverityHigh). Code is truth.
		{
			name:           "grace-period-expired",
			bindingMode:    "grace-period",
			observedIMEI:   imeiB,
			boundIMEI:      ptrStr(imeiA),
			graceExpires:   ptrTime(fixedNow.Add(-time.Hour)), // expired 1h ago
			expectedSev:    SeverityHigh,
			expectedReason: RejectReasonGraceExpired,
			expectedKind:   VerdictReject,
		},
		// soft + mismatch → Info / "" (AllowWithAlarm — never rejects)
		{
			name:           "soft-mismatch",
			bindingMode:    "soft",
			observedIMEI:   imeiB,
			boundIMEI:      ptrStr(imeiA),
			expectedSev:    SeverityInfo,
			expectedReason: "",
			expectedKind:   VerdictAllowWithAlarm,
		},
		// first-use + unbound (first capture) → Info / "" (Allow with LockBoundIMEI)
		{
			name:           "first-use-locked",
			bindingMode:    "first-use",
			observedIMEI:   imeiA,
			boundIMEI:      nil,
			expectedSev:    SeverityInfo,
			expectedReason: "",
			expectedKind:   VerdictAllow,
		},
		// null-mode + blacklist override → High / BINDING_BLACKLIST / Reject
		{
			name:           "null-blacklist",
			bindingMode:    "",
			observedIMEI:   imeiA,
			boundIMEI:      nil,
			useBlacklist:   true,
			expectedSev:    SeverityHigh,
			expectedReason: RejectReasonBlacklist,
			expectedKind:   VerdictReject,
		},
		// grace-period + within-window mismatch → Medium / "" / AllowWithAlarm
		// (evalGracePeriod row #19: accepted change, not a reject)
		{
			name:           "grace-period-within-window",
			bindingMode:    "grace-period",
			observedIMEI:   imeiB,
			boundIMEI:      ptrStr(imeiA),
			graceExpires:   ptrTime(fixedNow.Add(48 * time.Hour)), // 48h ahead
			expectedSev:    SeverityMedium,
			expectedReason: "",
			expectedKind:   VerdictAllowWithAlarm,
		},
		// tac-lock + same TAC → "" / "" / Allow (matching TAC accepted)
		{
			name:           "tac-lock-same-tac",
			bindingMode:    "tac-lock",
			observedIMEI:   imeiACousin, // same first-8 TAC as imeiA, different SVN
			boundIMEI:      ptrStr(imeiA),
			expectedSev:    "",
			expectedReason: "",
			expectedKind:   VerdictAllow,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var opts []Option
			opts = append(opts, WithClock(fixedClock))

			if tc.useBlacklist {
				opts = append(opts, WithBlacklistChecker(&mockBlacklist{hit: true}))
			}
			if tc.useAllowlist {
				opts = append(opts, WithAllowlistChecker(&mockAllowlist{allowed: tc.allowlistHit}))
			}

			enforcer := New(opts...)

			sim := makeSIM(tc.bindingMode, tc.boundIMEI, tc.graceExpires)
			session := SessionContext{
				TenantID:        sim.TenantID,
				SIMID:           sim.ID,
				IMEI:            tc.observedIMEI,
				SoftwareVersion: "1.0",
			}

			v, err := enforcer.Evaluate(context.Background(), session, sim)
			if err != nil {
				t.Fatalf("Evaluate: unexpected err: %v", err)
			}

			if v.Kind != tc.expectedKind {
				t.Errorf("Kind = %v, want %v", v.Kind, tc.expectedKind)
			}
			if v.Severity != tc.expectedSev {
				t.Errorf("Severity = %q, want %q", v.Severity, tc.expectedSev)
			}
			if v.Reason != tc.expectedReason {
				t.Errorf("Reason = %q, want %q", v.Reason, tc.expectedReason)
			}
		})
	}
}

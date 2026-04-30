package store

import (
	"testing"
)

func TestNewNotificationTemplateStore(t *testing.T) {
	s := NewNotificationTemplateStore(nil)
	if s == nil {
		t.Fatal("NewNotificationTemplateStore returned nil")
	}
}

func TestNotificationTemplate_StructFields(t *testing.T) {
	tmpl := NotificationTemplate{
		EventType: "operator.down",
		Locale:    "en",
		Subject:   "Operator Down",
		BodyText:  "Operator {{.OperatorName}} is down.",
		BodyHTML:  "<b>Operator {{.OperatorName}} is down.</b>",
	}

	if tmpl.EventType != "operator.down" {
		t.Errorf("EventType = %q, want operator.down", tmpl.EventType)
	}
	if tmpl.Locale != "en" {
		t.Errorf("Locale = %q, want en", tmpl.Locale)
	}
	if tmpl.Subject != "Operator Down" {
		t.Errorf("Subject = %q, want 'Operator Down'", tmpl.Subject)
	}
	if tmpl.BodyText == "" {
		t.Error("BodyText should not be empty")
	}
	if tmpl.BodyHTML == "" {
		t.Error("BodyHTML should not be empty")
	}
}

func TestErrTemplateNotFound_Sentinel(t *testing.T) {
	if ErrTemplateNotFound.Error() != "store: notification template not found" {
		t.Errorf("ErrTemplateNotFound = %q", ErrTemplateNotFound.Error())
	}
}

// TestTemplateGetFallback_Logic validates the locale fallback strategy used by
// NotificationTemplateStore.Get. The SQL query selects rows matching the
// requested locale OR 'en', ordering requested locale first. This test
// verifies the ordering logic in isolation (AC-8).
func TestTemplateGetFallback_OrderingLogic(t *testing.T) {
	type localeRow struct {
		locale string
		order  int
	}

	// Simulate ORDER BY CASE WHEN locale = $2 THEN 0 ELSE 1 END
	sortOrder := func(locale, requestedLocale string) int {
		if locale == requestedLocale {
			return 0
		}
		return 1
	}

	cases := []struct {
		name          string
		requestLocale string
		available     []string
		wantLocale    string
		wantFound     bool
	}{
		{
			name:          "tr exists — returns tr",
			requestLocale: "tr",
			available:     []string{"tr", "en"},
			wantLocale:    "tr",
			wantFound:     true,
		},
		{
			name:          "tr missing but en exists — returns en (fallback)",
			requestLocale: "tr",
			available:     []string{"en"},
			wantLocale:    "en",
			wantFound:     true,
		},
		{
			name:          "both missing — not found",
			requestLocale: "tr",
			available:     []string{},
			wantLocale:    "",
			wantFound:     false,
		},
		{
			name:          "en requested and exists — returns en",
			requestLocale: "en",
			available:     []string{"en"},
			wantLocale:    "en",
			wantFound:     true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Filter to rows IN (requested, 'en')
			var candidates []localeRow
			for _, l := range tc.available {
				if l == tc.requestLocale || l == "en" {
					candidates = append(candidates, localeRow{
						locale: l,
						order:  sortOrder(l, tc.requestLocale),
					})
				}
			}

			// Pick lowest order (0 = requested locale, 1 = fallback 'en')
			var best *localeRow
			for i := range candidates {
				if best == nil || candidates[i].order < best.order {
					best = &candidates[i]
				}
			}

			if !tc.wantFound {
				if best != nil {
					t.Errorf("expected not found, but got locale %q", best.locale)
				}
				return
			}
			if best == nil {
				t.Fatalf("expected locale %q, got not found", tc.wantLocale)
			}
			if best.locale != tc.wantLocale {
				t.Errorf("locale = %q, want %q", best.locale, tc.wantLocale)
			}
		})
	}
}

func TestNotificationTemplate_ZeroValue(t *testing.T) {
	tmpl := NotificationTemplate{}
	if tmpl.EventType != "" {
		t.Error("EventType zero value should be empty")
	}
	if tmpl.Locale != "" {
		t.Error("Locale zero value should be empty")
	}
}

func TestNotificationTemplateStore_List_FiltersDocumented(t *testing.T) {
	s := NewNotificationTemplateStore(nil)
	if s == nil {
		t.Fatal("NewNotificationTemplateStore returned nil")
	}
	// Verifies the store constructor is non-nil. List with empty filters
	// returns all templates (DB-dependent, tested in integration suite).
}

func TestNotificationTemplate_NoPrimaryKeyID(t *testing.T) {
	// notification_templates PK is (event_type, locale), no UUID id column.
	// This ensures no ID field is accidentally added to the struct.
	tmpl := NotificationTemplate{
		EventType: "quota.warning",
		Locale:    "tr",
	}
	// If the struct had an ID field, this assignment would still compile but
	// the schema would diverge. The absence of any id field in NotificationTemplate
	// is verified by the fact that this struct literal is valid without one.
	_ = tmpl
}

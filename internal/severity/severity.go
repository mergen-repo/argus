// Package severity defines the canonical 5-level severity taxonomy used across
// Argus: alerts, anomalies, policy violations, notifications, notification preferences.
// Values (strictly ordered, lowest to highest urgency): info < low < medium < high < critical.
package severity

import "errors"

const (
	Critical = "critical"
	High     = "high"
	Medium   = "medium"
	Low      = "low"
	Info     = "info"
)

// Values lists all canonical severity values ordered from highest urgency to lowest.
// Used by UI dropdowns and error messages.
var Values = []string{Critical, High, Medium, Low, Info}

// OrdinalMap maps each canonical severity value to a numeric ordinal (1=lowest, 5=highest).
var OrdinalMap = map[string]int{
	Info:     1,
	Low:      2,
	Medium:   3,
	High:     4,
	Critical: 5,
}

// ErrInvalidSeverity is returned when a severity value is not one of the 5 canonical values.
var ErrInvalidSeverity = errors.New("invalid severity value")

// Validate returns ErrInvalidSeverity when s is not one of the 5 canonical severity values.
// Empty string is not valid; callers handling optional filters must check for empty before calling Validate.
func Validate(s string) error {
	if _, ok := OrdinalMap[s]; !ok {
		return ErrInvalidSeverity
	}
	return nil
}

// IsValid reports whether s is a canonical severity value.
func IsValid(s string) bool {
	return Validate(s) == nil
}

// Ordinal returns the numeric ordinal for s (1=info .. 5=critical), or 0 for invalid/empty values.
func Ordinal(s string) int {
	return OrdinalMap[s]
}

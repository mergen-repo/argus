package job

import (
	"fmt"
	"time"
)

// NextRunAfter returns the first time at or after now+1 minute that the cron
// schedule would fire. It supports the same syntax as shouldFire:
// @hourly, @daily, @weekly, @monthly, and standard 5-field cron expressions
// using *, */N, integer literals, comma-separated lists, and ranges.
//
// Returns an error if no matching time is found within 365 days, which also
// covers syntactically or semantically invalid expressions.
func NextRunAfter(schedule string, now time.Time) (time.Time, error) {
	candidate := now.Truncate(time.Minute).Add(time.Minute)
	deadline := now.Add(365 * 24 * time.Hour)

	for candidate.Before(deadline) {
		if shouldFire(schedule, candidate) {
			return candidate, nil
		}
		candidate = candidate.Add(time.Minute)
	}
	return time.Time{}, fmt.Errorf("cron: no matching time found within 365 days for schedule %q", schedule)
}

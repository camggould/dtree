package cli

import (
	"fmt"
	"strings"
	"time"
)

// ParseTimeFlag parses a time string used in CLI flags (--since, --until, --at).
//
// Accepted forms:
//   - ISO durations relative to now: "7d", "24h", "30m" — returns time.Now().UTC() minus the duration.
//   - RFC3339 absolute: "2026-04-22T14:32:11Z"
//   - Date-only (start of day UTC): "2026-04-22"
//
// "d" (days) is expanded to hours before calling time.ParseDuration.
func ParseTimeFlag(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time flag")
	}

	// Relative duration: ends with a duration unit and contains no dashes in the
	// right positions to be a date. We detect this by trying duration parsing first
	// after expanding "d" → hours.
	if dur, ok := parseDurationFlag(s); ok {
		return time.Now().UTC().Add(-dur), nil
	}

	// RFC3339 / ISO 8601 with time component.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}

	// Date-only: "2026-04-22" → start of day UTC.
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC(), nil
	}

	return time.Time{}, fmt.Errorf("cannot parse time flag %q: accepted forms are 7d/24h/30m, RFC3339, or YYYY-MM-DD", s)
}

// parseDurationFlag tries to parse s as a relative duration (e.g. "7d", "24h",
// "30m"). Returns (duration, true) on success or (0, false) if s is not a
// duration string.
func parseDurationFlag(s string) (time.Duration, bool) {
	// Expand "d" (days) into hours so time.ParseDuration can handle it.
	expanded := expandDays(s)
	dur, err := time.ParseDuration(expanded)
	if err != nil {
		return 0, false
	}
	if dur < 0 {
		return 0, false
	}
	return dur, true
}

// expandDays replaces numeric "d" suffixes with the equivalent hours.
// E.g. "7d" → "168h", "1d30m" → "24h30m".
func expandDays(s string) string {
	// Walk through the string looking for digit runs followed by 'd'.
	var b strings.Builder
	i := 0
	for i < len(s) {
		// Find a digit run.
		if s[i] >= '0' && s[i] <= '9' {
			j := i
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			// Check what follows the digit run.
			if j < len(s) && s[j] == 'd' {
				// Parse the number of days.
				var days int
				for _, c := range s[i:j] {
					days = days*10 + int(c-'0')
				}
				hours := days * 24
				fmt.Fprintf(&b, "%dh", hours)
				i = j + 1
				continue
			}
			// Not a day suffix — write the digits as-is.
			b.WriteString(s[i:j])
			i = j
		} else {
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

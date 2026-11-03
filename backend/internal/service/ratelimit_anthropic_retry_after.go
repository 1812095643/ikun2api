package service

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Accept delta-seconds or HTTP-date, including production lowercase raw headers.
// Bound before converting to time.Duration to prevent integer overflow and an
// invalid response freezing an account indefinitely.
func anthropic429RetryAfter(headers http.Header, now time.Time) (time.Time, bool) {
	raw := strings.TrimSpace(getHeaderRaw(headers, "retry-after"))
	const maxDelay = 8 * 24 * time.Hour
	if seconds, err := strconv.ParseUint(raw, 10, 64); err == nil && seconds > 0 && seconds <= uint64(maxDelay/time.Second) {
		return now.Add(time.Duration(seconds) * time.Second), true
	}
	if resetAt, err := http.ParseTime(raw); err == nil && resetAt.After(now) && !resetAt.After(now.Add(maxDelay)) {
		return resetAt, true
	}
	return time.Time{}, false
}

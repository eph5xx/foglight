package linear

import (
	"fmt"
	"time"
	"unicode/utf8"
)

const (
	defaultLimit = 30
	maxLimit     = 100
)

// clampLimit normalizes a caller-supplied page size for Linear's `first`
// argument. Linear caps `first` at 250; we cap lower at 100 to keep tool
// responses friendly to agent context windows.
func clampLimit(v int) int {
	if v <= 0 {
		return defaultLimit
	}
	if v > maxLimit {
		return maxLimit
	}
	return v
}

func truncateString(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}

// parseUpdatedSince validates an RFC3339 timestamp from tool input. It
// returns the original string (Linear accepts RFC3339Nano on the wire) so
// callers don't lose precision.
func parseUpdatedSince(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	if _, err := time.Parse(time.RFC3339, s); err != nil {
		return "", fmt.Errorf("invalid RFC3339 timestamp %q: %w", s, err)
	}
	return s, nil
}

// eqFilter is the {key: {eq: value}} shape that Linear's filter inputs use
// for scalar equality. Several handlers compose filters from these.
func eqFilter(value any) map[string]any {
	return map[string]any{"eq": value}
}

// PageInfo is Linear's standard cursor pagination shape.
type PageInfo struct {
	EndCursor   string `json:"endCursor,omitempty"`
	HasNextPage bool   `json:"hasNextPage"`
}

// pageVars builds the standard {first, after} variable map used by every
// list query, optionally merged with extra variables.
func pageVars(limit int, cursor string, extra map[string]any) map[string]any {
	vars := map[string]any{"first": clampLimit(limit)}
	if cursor != "" {
		vars["after"] = cursor
	}
	for k, v := range extra {
		vars[k] = v
	}
	return vars
}

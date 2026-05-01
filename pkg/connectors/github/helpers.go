package github

import (
	"time"
	"unicode/utf8"

	gh "github.com/google/go-github/v82/github"
)

const (
	defaultPerPage = 30
	maxPerPage     = 100
	listPerPageCap = 100
)

func defaultStr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func defaultInt(v, fallback int) int {
	if v <= 0 {
		return fallback
	}
	return v
}

func clampPerPage(v int) int {
	if v <= 0 {
		return defaultPerPage
	}
	if v > maxPerPage {
		return maxPerPage
	}
	return v
}

func formatTime(t gh.Timestamp) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
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

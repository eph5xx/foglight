package slack

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultLimit    = 30
	maxLimit        = 100
	maxMessageChars = 8000
)

// clampLimit normalizes a caller-supplied page size against a default
// and a hard cap. Slack's per-method caps differ (history is 999, search
// is 100), but agent context windows make >100 painful, so callers
// generally pass maxLimit as the cap.
func clampLimit(v, dflt, max int) int {
	if v <= 0 {
		return dflt
	}
	if v > max {
		return max
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

// slackTSToRFC3339 converts a Slack message timestamp ("1704067200.123456")
// to RFC3339. Returns "" if the input is empty or unparseable so callers
// don't have to guard every field.
func slackTSToRFC3339(ts string) string {
	if ts == "" {
		return ""
	}
	dot := strings.IndexByte(ts, '.')
	whole := ts
	if dot >= 0 {
		whole = ts[:dot]
	}
	sec, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return ""
	}
	return time.Unix(sec, 0).UTC().Format(time.RFC3339)
}

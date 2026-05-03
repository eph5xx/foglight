package notion

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultLimit = 25
	maxLimit     = 100
)

// clampLimit normalizes a caller-supplied page size for Notion's `page_size`
// argument. Notion caps at 100 across all paginated endpoints.
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

// parseUpdatedSince validates an RFC3339 timestamp from tool input. Returns
// the original string so callers don't lose precision (Notion accepts
// ISO 8601 / RFC3339 on the wire).
func parseUpdatedSince(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	if _, err := time.Parse(time.RFC3339, s); err != nil {
		return "", fmt.Errorf("invalid RFC3339 timestamp %q: %w", s, err)
	}
	return s, nil
}

// richText is the minimal shape of Notion's rich_text item shared across
// every prose-bearing block. We only need plain_text — annotations, links,
// and mention details are intentionally dropped on the way to plain text.
type richText struct {
	PlainText string `json:"plain_text"`
}

// richTextPlain concatenates the plain_text values from a rich_text array.
func richTextPlain(rt []richText) string {
	if len(rt) == 0 {
		return ""
	}
	parts := make([]string, 0, len(rt))
	for _, r := range rt {
		if r.PlainText != "" {
			parts = append(parts, r.PlainText)
		}
	}
	return strings.Join(parts, "")
}

// extractTitle pulls the title string out of a Notion page's properties map.
// Database pages may name the title property anything; standalone pages use
// "title". We scan for the first property whose type is "title".
func extractTitle(properties map[string]pageProperty) string {
	for _, p := range properties {
		if p.Type == "title" {
			return richTextPlain(p.Title)
		}
	}
	return ""
}

// pageProperty is the minimal schema needed to find and read a page's title.
// Other property types are ignored for v1.
type pageProperty struct {
	ID    string     `json:"id,omitempty"`
	Type  string     `json:"type"`
	Title []richText `json:"title,omitempty"`
}

// uuidHex matches a 32-char hex run, used to extract a page ID from URLs.
var uuidHex = regexp.MustCompile(`[0-9a-fA-F]{32}`)

// normalizeID accepts a Notion ID in any of the forms an agent or human is
// likely to paste — bare 32-char hex, dashed UUID, or a notion.so /
// notion.site URL — and returns a dashed UUID. Empty input returns "".
func normalizeID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// If already a dashed UUID, keep as-is (Notion accepts both forms; we
	// pick dashed for canonical output).
	if len(s) == 36 && strings.Count(s, "-") == 4 {
		return strings.ToLower(s)
	}
	// Strip dashes from a 32-undashed run, then re-dash below.
	candidate := strings.ReplaceAll(s, "-", "")
	if len(candidate) == 32 && isHex(candidate) {
		return dashUUID(strings.ToLower(candidate))
	}
	// Otherwise, scan the string for the last 32-hex match (handles URLs
	// like .../Page-Title-1234567890abcdef1234567890abcdef).
	matches := uuidHex.FindAllString(s, -1)
	if len(matches) == 0 {
		return s // give up; let the API return its own 4xx
	}
	return dashUUID(strings.ToLower(matches[len(matches)-1]))
}

func isHex(s string) bool {
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func dashUUID(hex32 string) string {
	return hex32[0:8] + "-" + hex32[8:12] + "-" + hex32[12:16] + "-" + hex32[16:20] + "-" + hex32[20:32]
}

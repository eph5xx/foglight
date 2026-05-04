package notion

import (
	"regexp"
	"strings"
)

// kindHint is the entity kind a caller already knows or has narrowed to.
// "auto" means the resolver should probe to find out.
type kindHint string

const (
	kindAuto       kindHint = "auto"
	kindPage       kindHint = "page"
	kindDatabase   kindHint = "database"
	kindDataSource kindHint = "data_source"
	kindBlock      kindHint = "block"
)

// collectionScheme is foglight's reference scheme for data sources, mirroring
// the hosted Notion MCP. Maps 1:1 to /v1/data_sources/<uuid>.
const collectionScheme = "collection://"

// uuidHex matches a 32-char hex run, used to extract a Notion ID from URLs.
var uuidHex = regexp.MustCompile(`[0-9a-fA-F]{32}`)

// normalizeID accepts a Notion ID in any form an agent or human is likely to
// supply — bare 32-hex, dashed UUID, notion.so / notion.site URL, or a
// collection://<uuid> reference — and returns a dashed lowercase UUID. The
// query string is stripped before hex scanning so the ?v=<view_id> on a page
// URL can't be picked up instead of the page UUID.
func normalizeID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, collectionScheme) {
		s = strings.TrimPrefix(s, collectionScheme)
	}
	// Drop everything after the first '?' — Notion URLs frequently carry
	// ?v=<view_id> or ?p=<peek_id> hex strings that would otherwise win the
	// "last 32-hex match" race against the path UUID.
	if i := strings.IndexByte(s, '?'); i >= 0 {
		s = s[:i]
	}
	// Also drop the fragment.
	if i := strings.IndexByte(s, '#'); i >= 0 {
		s = s[:i]
	}
	if len(s) == 36 && strings.Count(s, "-") == 4 {
		return strings.ToLower(s)
	}
	candidate := strings.ReplaceAll(s, "-", "")
	if len(candidate) == 32 && isHex(candidate) {
		return dashUUID(strings.ToLower(candidate))
	}
	matches := uuidHex.FindAllString(s, -1)
	if len(matches) == 0 {
		return ""
	}
	return dashUUID(strings.ToLower(matches[len(matches)-1]))
}

// parseURI normalizes a caller-supplied identifier and returns a kind hint
// alongside the dashed UUID. A bare UUID or notion.so URL yields kindAuto;
// a collection:// URI yields kindDataSource.
func parseURI(s string) (kindHint, string) {
	t := strings.TrimSpace(s)
	if strings.HasPrefix(t, collectionScheme) {
		return kindDataSource, normalizeID(t)
	}
	return kindAuto, normalizeID(t)
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

// collectionURI returns the canonical collection:// reference for a data
// source UUID. Used in error messages and synthesized markdown so agents can
// paste the URI back into the next call.
func collectionURI(id string) string {
	return collectionScheme + id
}

package notion

import (
	"encoding/json"
	"fmt"
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

// truncateString cuts s to at most max bytes, preserving valid UTF-8 by
// rewinding to the previous rune start. Returns the truncated string and a
// flag indicating whether truncation occurred.
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
// the original string so callers don't lose precision.
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
// every prose-bearing block, property, and comment. We only need plain_text;
// annotations and link details are intentionally dropped on the way to plain
// text.
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

// pageProperty captures the union of Notion property shapes. Each property
// type populates exactly one of the typed fields; the rest are nil/empty.
// Lazy decoded — the page response wires every property through this struct,
// and propertySummary picks the right field by Type.
type pageProperty struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"-"` // populated from the map key, not the wire
	Type string `json:"type"`

	Title       []richText      `json:"title,omitempty"`
	RichText    []richText      `json:"rich_text,omitempty"`
	Number      *float64        `json:"number,omitempty"`
	Select      *namedOption    `json:"select,omitempty"`
	MultiSelect []namedOption   `json:"multi_select,omitempty"`
	Status      *namedOption    `json:"status,omitempty"`
	Date        *propertyDate   `json:"date,omitempty"`
	People      []propertyUser  `json:"people,omitempty"`
	Files       []propertyFile  `json:"files,omitempty"`
	Checkbox    *bool           `json:"checkbox,omitempty"`
	URL         string          `json:"url,omitempty"`
	Email       string          `json:"email,omitempty"`
	PhoneNumber string          `json:"phone_number,omitempty"`
	Formula     json.RawMessage `json:"formula,omitempty"`
	Relation    []idOnly        `json:"relation,omitempty"`
	Rollup      json.RawMessage `json:"rollup,omitempty"`
	CreatedTime string          `json:"created_time,omitempty"`
	CreatedBy   *propertyUser   `json:"created_by,omitempty"`
	LastEditedTime string       `json:"last_edited_time,omitempty"`
	LastEditedBy  *propertyUser `json:"last_edited_by,omitempty"`
	UniqueID    *uniqueID       `json:"unique_id,omitempty"`
	Verification *verification  `json:"verification,omitempty"`
	HasMore     bool            `json:"has_more,omitempty"`
}

type namedOption struct {
	Name string `json:"name"`
}

type propertyDate struct {
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

type propertyUser struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type propertyFile struct {
	Name string `json:"name,omitempty"`
}

type idOnly struct {
	ID string `json:"id"`
}

type uniqueID struct {
	Prefix string `json:"prefix,omitempty"`
	Number int    `json:"number"`
}

type verification struct {
	State string `json:"state,omitempty"`
}

// extractTitle pulls the title string out of a Notion page's properties map.
// Database pages may name the title property anything; standalone pages use
// "title". Scan for the first property whose type is "title".
func extractTitle(properties map[string]pageProperty) string {
	for _, p := range properties {
		if p.Type == "title" {
			return richTextPlain(p.Title)
		}
	}
	return ""
}

// propertySummary renders a single property as a one-line agent-readable
// string: "- Status: select=Done", "- Owner: people=Aleksandr Sarantsev",
// "- Tags: multi_select=urgent, infra". Unknown types fall back to
// "- Name: <type>" so the agent at least knows the property exists.
func propertySummary(name string, p pageProperty) string {
	body := propertySummaryBody(p)
	if body == "" {
		return fmt.Sprintf("- %s: %s", name, p.Type)
	}
	suffix := ""
	if p.HasMore {
		suffix = " (has_more)"
	}
	return fmt.Sprintf("- %s: %s=%s%s", name, p.Type, body, suffix)
}

func propertySummaryBody(p pageProperty) string {
	switch p.Type {
	case "title":
		return richTextPlain(p.Title)
	case "rich_text":
		return richTextPlain(p.RichText)
	case "number":
		if p.Number == nil {
			return ""
		}
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", *p.Number), "0"), ".")
	case "select":
		if p.Select == nil {
			return ""
		}
		return p.Select.Name
	case "status":
		if p.Status == nil {
			return ""
		}
		return p.Status.Name
	case "multi_select":
		return joinNames(p.MultiSelect)
	case "date":
		if p.Date == nil {
			return ""
		}
		if p.Date.End != "" {
			return p.Date.Start + "→" + p.Date.End
		}
		return p.Date.Start
	case "people":
		names := make([]string, 0, len(p.People))
		for _, u := range p.People {
			if u.Name != "" {
				names = append(names, u.Name)
			} else {
				names = append(names, u.ID)
			}
		}
		return strings.Join(names, ", ")
	case "files":
		names := make([]string, 0, len(p.Files))
		for _, f := range p.Files {
			if f.Name != "" {
				names = append(names, f.Name)
			}
		}
		return strings.Join(names, ", ")
	case "checkbox":
		if p.Checkbox == nil {
			return ""
		}
		if *p.Checkbox {
			return "true"
		}
		return "false"
	case "url":
		return p.URL
	case "email":
		return p.Email
	case "phone_number":
		return p.PhoneNumber
	case "relation":
		ids := make([]string, 0, len(p.Relation))
		for _, r := range p.Relation {
			ids = append(ids, r.ID)
		}
		return strings.Join(ids, ", ")
	case "created_time":
		return p.CreatedTime
	case "last_edited_time":
		return p.LastEditedTime
	case "created_by":
		if p.CreatedBy == nil {
			return ""
		}
		if p.CreatedBy.Name != "" {
			return p.CreatedBy.Name
		}
		return p.CreatedBy.ID
	case "last_edited_by":
		if p.LastEditedBy == nil {
			return ""
		}
		if p.LastEditedBy.Name != "" {
			return p.LastEditedBy.Name
		}
		return p.LastEditedBy.ID
	case "unique_id":
		if p.UniqueID == nil {
			return ""
		}
		if p.UniqueID.Prefix != "" {
			return fmt.Sprintf("%s-%d", p.UniqueID.Prefix, p.UniqueID.Number)
		}
		return fmt.Sprintf("%d", p.UniqueID.Number)
	case "verification":
		if p.Verification == nil {
			return ""
		}
		return p.Verification.State
	case "formula":
		// Formulas have a discriminated payload (string/number/boolean/date).
		// Surface the JSON so the agent at least sees the value shape.
		return strings.TrimSpace(string(p.Formula))
	case "rollup":
		// Same story as formula. Rollups >25 entries set has_more; the
		// caller should follow up with notion_get_page_property.
		return strings.TrimSpace(string(p.Rollup))
	}
	return ""
}

func joinNames(opts []namedOption) string {
	parts := make([]string, 0, len(opts))
	for _, o := range opts {
		parts = append(parts, o.Name)
	}
	return strings.Join(parts, ", ")
}

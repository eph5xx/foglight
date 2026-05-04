package notion

import "regexp"

// Ref is one item in notion_fetch's refs[] sidecar — a chainable pointer
// extracted from the enhanced markdown body. Agents can follow refs without
// regexing the prose.
//
// URI translation table (research §9):
//
//	collection://<uuid>                       -> GET /v1/data_sources/<uuid>
//	https://notion.so/<title>-<uuid>          -> GET /v1/pages/<uuid> or /v1/databases/<uuid>
//	dashed/undashed UUID                      -> probe in order: page, database, data_source, block
//
// type "unknown" indicates a block whose markdown rendering Notion couldn't
// produce; alt carries the block_type so the agent can decide what to do.
type Ref struct {
	Type string `json:"type"`
	URI  string `json:"uri,omitempty"`
	Alt  string `json:"alt,omitempty"`
}

// Notion's enhanced markdown uses simple double-quoted attributes. These
// regexes are deliberately narrow per tag rather than a generic HTML parser.
var (
	refPageRe       = regexp.MustCompile(`<page\s+url="([^"]+)"`)
	refDatabaseRe   = regexp.MustCompile(`<database\s+url="([^"]+)"`)
	refDataSourceRe = regexp.MustCompile(`<data-source\s+url="([^"]+)"`)
	refUnknownRe    = regexp.MustCompile(`<unknown\s+alt="([^"]*)"`)
)

// extractRefs scans the markdown body and pulls every chainable pointer
// into a structured slice. Order is preserved within tag type but not
// across types — agents looking for a specific URI should iterate.
func extractRefs(markdown string) []Ref {
	if markdown == "" {
		return nil
	}
	var refs []Ref
	for _, m := range refPageRe.FindAllStringSubmatch(markdown, -1) {
		refs = append(refs, Ref{Type: "page", URI: m[1]})
	}
	for _, m := range refDatabaseRe.FindAllStringSubmatch(markdown, -1) {
		refs = append(refs, Ref{Type: "database", URI: m[1]})
	}
	for _, m := range refDataSourceRe.FindAllStringSubmatch(markdown, -1) {
		refs = append(refs, Ref{Type: "data_source", URI: m[1]})
	}
	for _, m := range refUnknownRe.FindAllStringSubmatch(markdown, -1) {
		refs = append(refs, Ref{Type: "unknown", Alt: m[1]})
	}
	return refs
}

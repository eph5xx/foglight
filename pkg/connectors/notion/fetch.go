package notion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultMaxChars = 8000
	maxMaxChars     = 32000
)

func addFetchTools(server *mcp.Server, name string, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name: name + "_fetch",
		Description: "Fetch a Notion resource by id, URL, or collection:// reference. Polymorphic: " +
			"resolves to a page, database, data source, or block. " +
			"Pages are returned as enhanced markdown via Notion's /v1/pages/{id}/markdown endpoint " +
			"with a structured refs[] sidecar listing every <page>/<database>/<data-source> link " +
			"in the body. Databases return container metadata and one data-source ref per source. " +
			"Data sources return the property schema rendered as a small Markdown table. " +
			"Note: rollups and relations with more than 25 entries truncate in the page response — " +
			"call notion_get_page_property with the property's id when has_more is set. " +
			"format='json' is an escape hatch that returns Notion's raw response under raw[]. " +
			"On 404, the most common cause is that the integration has not been explicitly shared " +
			"with the page or database in the Notion UI.",
	}, fetch(client))
}

// ---------- types ----------

type FetchInput struct {
	ID                 string `json:"id"                            jsonschema:"page/database/data_source/block UUID, notion.so URL, or collection://<uuid>"`
	IncludeDiscussions bool   `json:"include_discussions,omitempty" jsonschema:"page kind: inline un-resolved discussion markers (default false)"`
	IncludeChildren    *bool  `json:"include_children,omitempty"    jsonschema:"database kind: also fetch each data source's metadata (default true)"`
	MaxChars           int    `json:"max_chars,omitempty"           jsonschema:"truncate markdown body (default 8000, cap 32000)"`
	Format             string `json:"format,omitempty"              jsonschema:"markdown (default) | json"`
}

type Parent struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
}

type FetchOutput struct {
	Kind           string          `json:"kind"`
	ID             string          `json:"id"`
	URL            string          `json:"url,omitempty"`
	Title          string          `json:"title,omitempty"`
	CreatedTime    string          `json:"created_time,omitempty"`
	LastEditedTime string          `json:"last_edited_time,omitempty"`
	Archived       bool            `json:"archived,omitempty"`
	InTrash        bool            `json:"in_trash,omitempty"`
	Parent         *Parent         `json:"parent,omitempty"`
	Markdown       string          `json:"markdown,omitempty"`
	Refs           []Ref           `json:"refs,omitempty"`
	Truncated      bool            `json:"truncated,omitempty"`
	Raw            json.RawMessage `json:"raw,omitempty"`
}

// ---------- raw decoders shared with query.go ----------

type rawPage struct {
	Object         string                  `json:"object"`
	ID             string                  `json:"id"`
	URL            string                  `json:"url"`
	PublicURL      string                  `json:"public_url,omitempty"`
	CreatedTime    string                  `json:"created_time"`
	LastEditedTime string                  `json:"last_edited_time"`
	Archived       bool                    `json:"archived"`
	InTrash        bool                    `json:"in_trash"`
	Properties     map[string]pageProperty `json:"properties"`
	Parent         struct {
		Type         string `json:"type"`
		PageID       string `json:"page_id,omitempty"`
		DatabaseID   string `json:"database_id,omitempty"`
		DataSourceID string `json:"data_source_id,omitempty"`
		BlockID      string `json:"block_id,omitempty"`
		Workspace    bool   `json:"workspace,omitempty"`
	} `json:"parent"`
}

type rawDatabase struct {
	Object         string     `json:"object"`
	ID             string     `json:"id"`
	URL            string     `json:"url,omitempty"`
	Title          []richText `json:"title,omitempty"`
	Description    []richText `json:"description,omitempty"`
	CreatedTime    string     `json:"created_time"`
	LastEditedTime string     `json:"last_edited_time"`
	InTrash        bool       `json:"in_trash"`
	IsInline       bool       `json:"is_inline,omitempty"`
	IsLocked       bool       `json:"is_locked,omitempty"`
	DataSources    []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"data_sources"`
	Parent struct {
		Type      string `json:"type"`
		PageID    string `json:"page_id,omitempty"`
		BlockID   string `json:"block_id,omitempty"`
		Workspace bool   `json:"workspace,omitempty"`
	} `json:"parent"`
}

type rawDataSource struct {
	Object         string                  `json:"object"`
	ID             string                  `json:"id"`
	Title          []richText              `json:"title,omitempty"`
	Description    []richText              `json:"description,omitempty"`
	CreatedTime    string                  `json:"created_time"`
	LastEditedTime string                  `json:"last_edited_time"`
	InTrash        bool                    `json:"in_trash"`
	Properties     map[string]schemaColumn `json:"properties"`
	Parent         struct {
		Type       string `json:"type"`
		DatabaseID string `json:"database_id,omitempty"`
	} `json:"parent"`
	DatabaseParent json.RawMessage `json:"database_parent,omitempty"`
}

type schemaColumn struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// ---------- handler ----------

func fetch(client *Client) mcp.ToolHandlerFor[FetchInput, FetchOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in FetchInput) (*mcp.CallToolResult, FetchOutput, error) {
		hint, id := parseURI(in.ID)
		if id == "" {
			return nil, FetchOutput{}, fmt.Errorf("notion: id is required")
		}
		kind, err := resolveKind(ctx, client, id, hint)
		if err != nil {
			return nil, FetchOutput{}, err
		}

		maxChars := in.MaxChars
		if maxChars <= 0 {
			maxChars = defaultMaxChars
		}
		if maxChars > maxMaxChars {
			maxChars = maxMaxChars
		}
		jsonMode := strings.EqualFold(strings.TrimSpace(in.Format), "json")

		switch kind {
		case kindPage:
			return fetchPage(ctx, client, id, in.IncludeDiscussions, maxChars, jsonMode)
		case kindDatabase:
			return fetchDatabase(ctx, client, id, includeChildren(in.IncludeChildren), maxChars, jsonMode)
		case kindDataSource:
			return fetchDataSource(ctx, client, id, maxChars, jsonMode)
		case kindBlock:
			return fetchBlock(ctx, client, id, jsonMode)
		}
		return nil, FetchOutput{}, fmt.Errorf("notion: unexpected kind %q for %s", kind, id)
	}
}

func includeChildren(opt *bool) bool {
	if opt == nil {
		return true
	}
	return *opt
}

// ---------- page ----------

// markdownEnvelope is the shape /v1/pages/{id}/markdown returns: a wrapper
// around a markdown string. The discussions field is present (and non-empty)
// when include_discussions=true is requested.
type markdownEnvelope struct {
	Object   string `json:"object"`
	Markdown string `json:"markdown"`
}

func fetchPage(ctx context.Context, c *Client, id string, includeDiscussions bool, maxChars int, jsonMode bool) (*mcp.CallToolResult, FetchOutput, error) {
	// Fetch metadata and (when not in json mode) markdown body in parallel.
	var (
		page       rawPage
		pageRaw    []byte
		mdBody     []byte
		mdErr      error
		pageErr    error
		wg         sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		body, err := c.doRaw(ctx, http.MethodGet, "/pages/"+id)
		if err != nil {
			pageErr = err
			return
		}
		pageRaw = body
		if err := json.Unmarshal(body, &page); err != nil {
			pageErr = fmt.Errorf("notion: decode page: %w", err)
		}
	}()
	if !jsonMode {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path := "/pages/" + id + "/markdown"
			if includeDiscussions {
				path += "?include_discussions=true"
			}
			body, err := c.doRaw(ctx, http.MethodGet, path)
			if err != nil {
				mdErr = err
				return
			}
			mdBody = body
		}()
	}
	wg.Wait()
	if pageErr != nil {
		return nil, FetchOutput{}, fmt.Errorf("notion: get page %q: %w", id, pageErr)
	}

	out := FetchOutput{
		Kind:           "page",
		ID:             page.ID,
		URL:            page.URL,
		Title:          extractTitle(page.Properties),
		CreatedTime:    page.CreatedTime,
		LastEditedTime: page.LastEditedTime,
		Archived:       page.Archived,
		InTrash:        page.InTrash,
		Parent:         pageParentOf(page),
	}
	if jsonMode {
		out.Raw = pageRaw
		return nil, out, nil
	}
	if mdErr != nil {
		return nil, FetchOutput{}, fmt.Errorf("notion: get page markdown %q: %w", id, mdErr)
	}
	var env markdownEnvelope
	if err := json.Unmarshal(mdBody, &env); err != nil {
		return nil, FetchOutput{}, fmt.Errorf("notion: decode page markdown: %w", err)
	}
	md, truncated := truncateString(env.Markdown, maxChars)
	out.Markdown = md
	out.Truncated = truncated
	out.Refs = extractRefs(env.Markdown) // refs over the full body, not the truncated form
	return nil, out, nil
}

func pageParentOf(p rawPage) *Parent {
	switch p.Parent.Type {
	case "page_id":
		return &Parent{Type: "page_id", ID: p.Parent.PageID}
	case "database_id":
		return &Parent{Type: "database_id", ID: p.Parent.DatabaseID}
	case "data_source_id":
		return &Parent{Type: "data_source_id", ID: p.Parent.DataSourceID}
	case "block_id":
		return &Parent{Type: "block_id", ID: p.Parent.BlockID}
	case "workspace":
		return &Parent{Type: "workspace"}
	}
	return nil
}

// ---------- database ----------

func fetchDatabase(ctx context.Context, c *Client, id string, includeChildren bool, maxChars int, jsonMode bool) (*mcp.CallToolResult, FetchOutput, error) {
	body, err := c.doRaw(ctx, http.MethodGet, "/databases/"+id)
	if err != nil {
		return nil, FetchOutput{}, fmt.Errorf("notion: get database %q: %w", id, err)
	}
	var db rawDatabase
	if err := json.Unmarshal(body, &db); err != nil {
		return nil, FetchOutput{}, fmt.Errorf("notion: decode database: %w", err)
	}
	out := FetchOutput{
		Kind:           "database",
		ID:             db.ID,
		URL:            db.URL,
		Title:          richTextPlain(db.Title),
		CreatedTime:    db.CreatedTime,
		LastEditedTime: db.LastEditedTime,
		InTrash:        db.InTrash,
		Parent:         databaseParentOf(db),
	}
	if jsonMode {
		out.Raw = body
		return nil, out, nil
	}

	var sb strings.Builder
	if t := strings.TrimSpace(out.Title); t != "" {
		fmt.Fprintf(&sb, "# %s\n\n", t)
	}
	if d := richTextPlain(db.Description); d != "" {
		fmt.Fprintf(&sb, "%s\n\n", d)
	}
	sb.WriteString("## Data sources\n\n")
	for _, ds := range db.DataSources {
		name := ds.Name
		if name == "" {
			name = "(unnamed)"
		}
		fmt.Fprintf(&sb, "- <data-source url=\"%s\">%s</data-source>\n", collectionURI(ds.ID), name)
	}

	if includeChildren && len(db.DataSources) > 0 {
		sb.WriteString("\n## Schemas\n\n")
		for _, ds := range db.DataSources {
			schema, err := fetchDataSourceSchema(ctx, c, ds.ID)
			if err != nil {
				fmt.Fprintf(&sb, "### %s\n\n_Error fetching schema: %v_\n\n", ds.Name, err)
				continue
			}
			fmt.Fprintf(&sb, "### %s (%s)\n\n", ds.Name, collectionURI(ds.ID))
			writeSchemaTable(&sb, schema)
			sb.WriteString("\n")
		}
	}

	md, truncated := truncateString(sb.String(), maxChars)
	out.Markdown = md
	out.Truncated = truncated
	out.Refs = extractRefs(sb.String())
	return nil, out, nil
}

func databaseParentOf(db rawDatabase) *Parent {
	switch db.Parent.Type {
	case "page_id":
		return &Parent{Type: "page_id", ID: db.Parent.PageID}
	case "block_id":
		return &Parent{Type: "block_id", ID: db.Parent.BlockID}
	case "workspace":
		return &Parent{Type: "workspace"}
	}
	return nil
}

// ---------- data source ----------

func fetchDataSource(ctx context.Context, c *Client, id string, maxChars int, jsonMode bool) (*mcp.CallToolResult, FetchOutput, error) {
	body, err := c.doRaw(ctx, http.MethodGet, "/data_sources/"+id)
	if err != nil {
		return nil, FetchOutput{}, fmt.Errorf("notion: get data source %q: %w", id, err)
	}
	var ds rawDataSource
	if err := json.Unmarshal(body, &ds); err != nil {
		return nil, FetchOutput{}, fmt.Errorf("notion: decode data source: %w", err)
	}
	out := FetchOutput{
		Kind:           "data_source",
		ID:             ds.ID,
		URL:            collectionURI(ds.ID),
		Title:          richTextPlain(ds.Title),
		CreatedTime:    ds.CreatedTime,
		LastEditedTime: ds.LastEditedTime,
		InTrash:        ds.InTrash,
	}
	if ds.Parent.Type == "database_id" {
		out.Parent = &Parent{Type: "database_id", ID: ds.Parent.DatabaseID}
	}
	if jsonMode {
		out.Raw = body
		return nil, out, nil
	}
	var sb strings.Builder
	title := strings.TrimSpace(out.Title)
	if title == "" {
		title = "(unnamed data source)"
	}
	fmt.Fprintf(&sb, "# %s\n\n", title)
	if d := richTextPlain(ds.Description); d != "" {
		fmt.Fprintf(&sb, "%s\n\n", d)
	}
	if out.Parent != nil {
		fmt.Fprintf(&sb, "Container: <database url=\"%s\">database</database>\n\n", out.Parent.ID)
	}
	sb.WriteString("## Properties\n\n")
	writeSchemaTable(&sb, ds.Properties)
	md, truncated := truncateString(sb.String(), maxChars)
	out.Markdown = md
	out.Truncated = truncated
	out.Refs = extractRefs(sb.String())
	return nil, out, nil
}

func fetchDataSourceSchema(ctx context.Context, c *Client, id string) (map[string]schemaColumn, error) {
	var ds rawDataSource
	if err := c.do(ctx, http.MethodGet, "/data_sources/"+id, nil, &ds); err != nil {
		return nil, err
	}
	return ds.Properties, nil
}

func writeSchemaTable(sb *strings.Builder, props map[string]schemaColumn) {
	if len(props) == 0 {
		sb.WriteString("_(no properties)_\n")
		return
	}
	names := make([]string, 0, len(props))
	for k := range props {
		names = append(names, k)
	}
	// Stable, name-sorted order so the table is deterministic across runs.
	sort.Strings(names)
	sb.WriteString("| Name | Type | ID |\n|---|---|---|\n")
	for _, n := range names {
		c := props[n]
		fmt.Fprintf(sb, "| %s | %s | %s |\n", n, c.Type, c.ID)
	}
}

// ---------- block ----------

func fetchBlock(ctx context.Context, c *Client, id string, jsonMode bool) (*mcp.CallToolResult, FetchOutput, error) {
	body, err := c.doRaw(ctx, http.MethodGet, "/blocks/"+id)
	if err != nil {
		return nil, FetchOutput{}, fmt.Errorf("notion: get block %q: %w", id, err)
	}
	var rb rawBlock
	if err := json.Unmarshal(body, &rb); err != nil {
		return nil, FetchOutput{}, fmt.Errorf("notion: decode block: %w", err)
	}
	out := FetchOutput{
		Kind: "block",
		ID:   rb.ID,
	}
	if jsonMode {
		out.Raw = body
		return nil, out, nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "<%s>", rb.Type)
	if payload, ok := rb.Payloads[rb.Type]; ok {
		// One-line preview: surface the type and let the JSON payload come
		// through verbatim. notion_get_block_children is the proper tool
		// for walking subtrees.
		sb.WriteString("\n```json\n")
		sb.Write(payload)
		sb.WriteString("\n```")
	}
	if rb.HasChildren {
		fmt.Fprintf(&sb, "\n_has_children — call notion_get_block_children with block_id=%s_", rb.ID)
	}
	out.Markdown = sb.String()
	out.Refs = extractRefs(out.Markdown)
	return nil, out, nil
}

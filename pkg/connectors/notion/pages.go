package notion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultMaxChars = 8000
	maxMaxChars     = 32000

	// blockChildrenDepth caps how deep we recurse into nested blocks. The
	// initial call reads the page's direct children at depth 0, so this
	// constant is the number of levels we'll traverse (0, 1, 2). Most
	// docs are flat or one level deep; deeper pages rarely add value once
	// truncation kicks in.
	blockChildrenDepth = 3

	// maxBlockFetches caps the total number of /v1/blocks/{id}/children
	// requests for one notion_get_page call. Notion's rate limit is ~3
	// req/sec, so a runaway tree would otherwise stall the response.
	maxBlockFetches = 32

	// childPageSize is what we ask for per /v1/blocks/{id}/children call.
	// Notion's max is 100; one page is enough for typical docs.
	childPageSize = 100
)

func addPageTools(server *mcp.Server, name string, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name: name + "_get_page",
		Description: "Get a Notion page's metadata and prose content (flattened to plain text " +
			"with light Markdown-ish prefixes for headings, lists, todos, quotes, and code). " +
			"Accepts a UUID (dashed or undashed) or a notion.so / notion.site URL. " +
			"Block children are walked depth-first (depth-capped); exotic block types " +
			"render as [<type>] placeholders.",
	}, getPage(client))
}

// ---------- notion_get_page ----------

type GetPageInput struct {
	ID       string `json:"id" jsonschema:"page UUID (dashed or 32-char) or notion.so/notion.site URL"`
	MaxChars int    `json:"max_chars,omitempty" jsonschema:"truncate body to this many chars (default 8000, cap 32000)"`
}

type pageParent struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
}

type GetPageOutput struct {
	ID               string      `json:"id"`
	URL              string      `json:"url,omitempty"`
	Title            string      `json:"title,omitempty"`
	CreatedTime      string      `json:"createdTime,omitempty"`
	LastEditedTime   string      `json:"lastEditedTime,omitempty"`
	Archived         bool        `json:"archived,omitempty"`
	Parent           *pageParent `json:"parent,omitempty"`
	Content          string      `json:"content,omitempty"`
	ContentTruncated bool        `json:"contentTruncated,omitempty"`
}

// rawPage captures the page-level fields we need from GET /v1/pages/{id}.
type rawPage struct {
	Object         string                  `json:"object"`
	ID             string                  `json:"id"`
	URL            string                  `json:"url"`
	CreatedTime    string                  `json:"created_time"`
	LastEditedTime string                  `json:"last_edited_time"`
	Archived       bool                    `json:"archived"`
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

func getPage(client *Client) mcp.ToolHandlerFor[GetPageInput, GetPageOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in GetPageInput) (*mcp.CallToolResult, GetPageOutput, error) {
		id := normalizeID(in.ID)
		if id == "" {
			return nil, GetPageOutput{}, fmt.Errorf("notion: id is required")
		}
		maxChars := in.MaxChars
		if maxChars <= 0 {
			maxChars = defaultMaxChars
		}
		if maxChars > maxMaxChars {
			maxChars = maxMaxChars
		}

		var page rawPage
		if err := client.do(ctx, http.MethodGet, "/pages/"+id, nil, &page); err != nil {
			return nil, GetPageOutput{}, fmt.Errorf("notion: get page %q: %w", id, err)
		}

		// Walk the block tree, accumulating into a string builder until
		// we exceed maxChars or hit the fetch cap.
		var sb strings.Builder
		fetches := 0
		flattenChildren(ctx, client, page.ID, 0, &sb, maxChars, &fetches)

		content := sb.String()
		content, truncated := truncateString(content, maxChars)

		out := GetPageOutput{
			ID:             page.ID,
			URL:            page.URL,
			Title:          extractTitle(page.Properties),
			CreatedTime:    page.CreatedTime,
			LastEditedTime: page.LastEditedTime,
			Archived:       page.Archived,
			Parent:         buildParent(page),
			Content:        content,
		}
		if truncated {
			out.ContentTruncated = true
		}
		return nil, out, nil
	}
}

func buildParent(p rawPage) *pageParent {
	switch p.Parent.Type {
	case "page_id":
		return &pageParent{Type: "page_id", ID: p.Parent.PageID}
	case "database_id":
		return &pageParent{Type: "database_id", ID: p.Parent.DatabaseID}
	case "data_source_id":
		return &pageParent{Type: "data_source_id", ID: p.Parent.DataSourceID}
	case "block_id":
		return &pageParent{Type: "block_id", ID: p.Parent.BlockID}
	case "workspace":
		return &pageParent{Type: "workspace"}
	}
	return nil
}

// rawBlock is one entry in /v1/blocks/{id}/children. The type-specific
// payload lives under a key matching the block's type — we decode it
// lazily as json.RawMessage and parse only when we recognize the type.
type rawBlock struct {
	Object      string                     `json:"object"`
	ID          string                     `json:"id"`
	Type        string                     `json:"type"`
	HasChildren bool                       `json:"has_children"`
	Archived    bool                       `json:"archived"`
	Payloads    map[string]json.RawMessage `json:"-"`
}

// UnmarshalJSON keeps the standard fields and stashes every other top-level
// key (which includes the type-specific payload like "paragraph", "code")
// into Payloads for later access.
func (b *rawBlock) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	b.Payloads = make(map[string]json.RawMessage, len(raw))
	for k, v := range raw {
		switch k {
		case "object":
			_ = json.Unmarshal(v, &b.Object)
		case "id":
			_ = json.Unmarshal(v, &b.ID)
		case "type":
			_ = json.Unmarshal(v, &b.Type)
		case "has_children":
			_ = json.Unmarshal(v, &b.HasChildren)
		case "archived":
			_ = json.Unmarshal(v, &b.Archived)
		default:
			b.Payloads[k] = v
		}
	}
	return nil
}

// flattenChildren fetches one page (up to childPageSize) of /v1/blocks/{parentID}/children
// and appends a plain-text rendering of each block to sb. Recurses into
// has_children blocks up to blockChildrenDepth. Stops early once sb exceeds
// maxChars or the global fetch budget is exhausted.
func flattenChildren(ctx context.Context, client *Client, parentID string, depth int, sb *strings.Builder, maxChars int, fetches *int) {
	if depth >= blockChildrenDepth || sb.Len() >= maxChars || *fetches >= maxBlockFetches {
		return
	}
	*fetches++

	path := fmt.Sprintf("/blocks/%s/children?page_size=%d", parentID, childPageSize)
	var resp struct {
		Results    []rawBlock `json:"results"`
		NextCursor string     `json:"next_cursor"`
		HasMore    bool       `json:"has_more"`
	}
	if err := client.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		// Don't abort the whole tool call on a single failed children
		// fetch — emit a marker and move on.
		fmt.Fprintf(sb, "[error fetching children: %v]\n", err)
		return
	}

	for i := range resp.Results {
		if sb.Len() >= maxChars {
			return
		}
		b := &resp.Results[i]
		renderBlock(b, sb)
		if b.HasChildren {
			flattenChildren(ctx, client, b.ID, depth+1, sb, maxChars, fetches)
		}
	}
}

// renderBlock writes a one-line-ish representation of a block to sb. Exotic
// or unsupported types render as "[<type>]" so the agent sees something but
// we don't pretend to support what we can't.
func renderBlock(b *rawBlock, sb *strings.Builder) {
	rt := readRichText(b)
	switch b.Type {
	case "paragraph":
		writeLine(sb, rt)
	case "heading_1":
		writeLine(sb, "# "+rt)
	case "heading_2":
		writeLine(sb, "## "+rt)
	case "heading_3":
		writeLine(sb, "### "+rt)
	case "bulleted_list_item":
		writeLine(sb, "- "+rt)
	case "numbered_list_item":
		writeLine(sb, "1. "+rt)
	case "to_do":
		checked := readChecked(b)
		marker := "- [ ] "
		if checked {
			marker = "- [x] "
		}
		writeLine(sb, marker+rt)
	case "toggle":
		writeLine(sb, "▸ "+rt)
	case "quote", "callout":
		writeLine(sb, "> "+rt)
	case "code":
		lang := readCodeLanguage(b)
		writeLine(sb, "```"+lang)
		writeLine(sb, rt)
		writeLine(sb, "```")
	case "divider":
		writeLine(sb, "---")
	case "equation":
		writeLine(sb, "$"+readEquationExpression(b)+"$")
	case "child_page":
		writeLine(sb, "→ child page: "+readChildPageTitle(b))
	case "child_database":
		writeLine(sb, "→ child database: "+readChildDatabaseTitle(b))
	case "bookmark", "embed", "link_preview", "image", "video", "file", "pdf":
		if url := readMediaURL(b); url != "" {
			writeLine(sb, "["+b.Type+": "+url+"]")
		} else {
			writeLine(sb, "["+b.Type+"]")
		}
	case "column_list", "column", "synced_block":
		// no inline text; children carry the content
	default:
		writeLine(sb, "["+b.Type+"]")
	}
}

func writeLine(sb *strings.Builder, s string) {
	if s == "" {
		sb.WriteByte('\n')
		return
	}
	sb.WriteString(s)
	sb.WriteByte('\n')
}

// readRichText pulls rich_text out of the block's type-specific payload, if
// any. Returns "" when the block type doesn't carry rich_text.
func readRichText(b *rawBlock) string {
	payload, ok := b.Payloads[b.Type]
	if !ok {
		return ""
	}
	var data struct {
		RichText []richText `json:"rich_text"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		return ""
	}
	return richTextPlain(data.RichText)
}

func readChecked(b *rawBlock) bool {
	payload, ok := b.Payloads[b.Type]
	if !ok {
		return false
	}
	var data struct {
		Checked bool `json:"checked"`
	}
	_ = json.Unmarshal(payload, &data)
	return data.Checked
}

func readCodeLanguage(b *rawBlock) string {
	payload, ok := b.Payloads["code"]
	if !ok {
		return ""
	}
	var data struct {
		Language string `json:"language"`
	}
	_ = json.Unmarshal(payload, &data)
	return data.Language
}

func readEquationExpression(b *rawBlock) string {
	payload, ok := b.Payloads["equation"]
	if !ok {
		return ""
	}
	var data struct {
		Expression string `json:"expression"`
	}
	_ = json.Unmarshal(payload, &data)
	return data.Expression
}

func readChildPageTitle(b *rawBlock) string {
	payload, ok := b.Payloads["child_page"]
	if !ok {
		return ""
	}
	var data struct {
		Title string `json:"title"`
	}
	_ = json.Unmarshal(payload, &data)
	return data.Title
}

func readChildDatabaseTitle(b *rawBlock) string {
	payload, ok := b.Payloads["child_database"]
	if !ok {
		return ""
	}
	var data struct {
		Title string `json:"title"`
	}
	_ = json.Unmarshal(payload, &data)
	return data.Title
}

// readMediaURL extracts a URL from bookmark/embed/link_preview (top-level
// "url") or image/video/file/pdf (nested under "external" or "file").
func readMediaURL(b *rawBlock) string {
	payload, ok := b.Payloads[b.Type]
	if !ok {
		return ""
	}
	var data struct {
		URL      string `json:"url,omitempty"`
		External *struct {
			URL string `json:"url"`
		} `json:"external,omitempty"`
		File *struct {
			URL string `json:"url"`
		} `json:"file,omitempty"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		return ""
	}
	if data.URL != "" {
		return data.URL
	}
	if data.External != nil && data.External.URL != "" {
		return data.External.URL
	}
	if data.File != nil && data.File.URL != "" {
		return data.File.URL
	}
	return ""
}

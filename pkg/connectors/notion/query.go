package notion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func addQueryTools(server *mcp.Server, name string, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name: name + "_query_data_source",
		Description: "Query rows of a Notion data source with filters, sorts, and cursor pagination. " +
			"data_source accepts a data source UUID, a collection://<uuid> reference, or a database " +
			"URL/UUID — single-source databases resolve automatically; multi-source databases return " +
			"an error listing each data source's collection:// URI so the agent can pick one. " +
			"filter and sorts are passed through verbatim using Notion's database-filter syntax. " +
			"request_status='incomplete' surfaces Notion's 10,000-row session cap so an agent can " +
			"narrow the filter or stream by last_edited_time. Properties are summarized one-line " +
			"per property; rollups/relations with more than 25 entries set has_more on their summary " +
			"and require notion_get_page_property for the full list.",
	}, queryDataSource(client))
}

// ---------- notion_query_data_source ----------

type QueryInput struct {
	DataSource string          `json:"data_source"          jsonschema:"data source UUID, collection://<uuid>, database URL, or database UUID"`
	Filter     json.RawMessage `json:"filter,omitempty"     jsonschema:"Notion filter object, passed through verbatim"`
	Sorts      json.RawMessage `json:"sorts,omitempty"      jsonschema:"Notion sorts array, passed through verbatim"`
	Cursor     string          `json:"cursor,omitempty"`
	Limit      int             `json:"limit,omitempty"      jsonschema:"1-100 (default 25)"`
	InTrash    bool            `json:"in_trash,omitempty"   jsonschema:"include trashed rows (default false)"`
}

type QueryRow struct {
	ID                string   `json:"id"`
	URL               string   `json:"url,omitempty"`
	Title             string   `json:"title,omitempty"`
	CreatedTime       string   `json:"created_time,omitempty"`
	LastEditedTime    string   `json:"last_edited_time,omitempty"`
	Archived          bool     `json:"archived,omitempty"`
	InTrash           bool     `json:"in_trash,omitempty"`
	PropertiesSummary []string `json:"properties_summary"`
}

type QueryOutput struct {
	DataSourceID  string     `json:"data_source_id"`
	Results       []QueryRow `json:"results"`
	NextCursor    string     `json:"next_cursor,omitempty"`
	HasMore       bool       `json:"has_more"`
	RequestStatus string     `json:"request_status,omitempty"`
}

func queryDataSource(client *Client) mcp.ToolHandlerFor[QueryInput, QueryOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in QueryInput) (*mcp.CallToolResult, QueryOutput, error) {
		hint, id := parseURI(in.DataSource)
		if id == "" {
			return nil, QueryOutput{}, fmt.Errorf("notion: data_source is required")
		}

		dataSourceID, err := resolveDataSourceID(ctx, client, id, hint)
		if err != nil {
			return nil, QueryOutput{}, err
		}

		body := map[string]any{
			"page_size": clampLimit(in.Limit),
		}
		if len(in.Filter) > 0 {
			body["filter"] = json.RawMessage(in.Filter)
		}
		if len(in.Sorts) > 0 {
			body["sorts"] = json.RawMessage(in.Sorts)
		}
		if in.Cursor != "" {
			body["start_cursor"] = in.Cursor
		}
		if in.InTrash {
			body["in_trash"] = true
		}

		var resp struct {
			Results       []rawPage `json:"results"`
			NextCursor    string    `json:"next_cursor"`
			HasMore       bool      `json:"has_more"`
			RequestStatus string    `json:"request_status,omitempty"`
		}
		path := fmt.Sprintf("/data_sources/%s/query", dataSourceID)
		if err := client.do(ctx, http.MethodPost, path, body, &resp); err != nil {
			return nil, QueryOutput{}, fmt.Errorf("notion: query data_source %q: %w", dataSourceID, err)
		}

		out := QueryOutput{
			DataSourceID:  dataSourceID,
			Results:       make([]QueryRow, 0, len(resp.Results)),
			NextCursor:    resp.NextCursor,
			HasMore:       resp.HasMore,
			RequestStatus: resp.RequestStatus,
		}
		for _, p := range resp.Results {
			out.Results = append(out.Results, QueryRow{
				ID:                p.ID,
				URL:               p.URL,
				Title:             extractTitle(p.Properties),
				CreatedTime:       p.CreatedTime,
				LastEditedTime:    p.LastEditedTime,
				Archived:          p.Archived,
				InTrash:           p.InTrash,
				PropertiesSummary: summarizeProperties(p.Properties),
			})
		}
		return nil, out, nil
	}
}

// resolveDataSourceID maps the caller's input to a data source UUID.
//
//   - Caller passed collection://<uuid> → use it directly.
//   - Caller passed a UUID we already know is a data source (kindDataSource hint) → use it.
//   - Caller passed a UUID/URL with no hint → try /v1/databases/{id}; if it resolves and has
//     exactly one data source, use that. Multiple sources → return a structured error listing
//     each one as collection://<id>. If /v1/databases/{id} 404s, fall back to assuming the
//     caller passed a data source UUID directly.
//   - Anything else 404s through the regular query path.
func resolveDataSourceID(ctx context.Context, c *Client, id string, hint kindHint) (string, error) {
	if hint == kindDataSource {
		return id, nil
	}
	// Probe as a database first. If it works, decide based on data_sources[].
	var db rawDatabase
	err := c.do(ctx, http.MethodGet, "/databases/"+id, nil, &db)
	if err == nil {
		switch len(db.DataSources) {
		case 0:
			return "", fmt.Errorf("notion: database %s has no data sources", id)
		case 1:
			return db.DataSources[0].ID, nil
		default:
			lines := make([]string, 0, len(db.DataSources))
			for _, ds := range db.DataSources {
				name := ds.Name
				if name == "" {
					name = "(unnamed)"
				}
				lines = append(lines, fmt.Sprintf("  - %s (%s)", name, collectionURI(ds.ID)))
			}
			return "", fmt.Errorf(
				"notion: database %s has %d data sources; pass one of these as data_source and re-call:\n%s",
				id, len(db.DataSources), strings.Join(lines, "\n"),
			)
		}
	}
	if !isNotFound(err) {
		return "", fmt.Errorf("notion: probe database %s: %w", id, err)
	}
	// Not a database — assume the caller handed us a data source UUID directly.
	return id, nil
}

func summarizeProperties(props map[string]pageProperty) []string {
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		p := props[k]
		out = append(out, propertySummary(k, p))
	}
	return out
}

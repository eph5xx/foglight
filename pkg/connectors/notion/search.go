package notion

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func addSearchTools(server *mcp.Server, name string, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name: name + "_search",
		Description: "Search pages and data sources shared with the integration. " +
			"Notion's search is title-substring, not full-text or semantic — phrase queries " +
			"against page/data-source titles for best recall. Empty query returns everything " +
			"the integration can see. " +
			"object_type: page | data_source | any (default any). The legacy value 'database' " +
			"is silently normalized to 'data_source' to match the 2025-09-03+ API. " +
			"Returns summaries; use notion_fetch to read content.",
	}, runSearch(client))
}

// ---------- notion_search ----------

type SearchInput struct {
	Query        string `json:"query,omitempty"         jsonschema:"title-substring query (optional; empty returns all shared resources)"`
	ObjectType   string `json:"object_type,omitempty"   jsonschema:"page | data_source | any (default any). 'database' is accepted and normalized to 'data_source'."`
	UpdatedSince string `json:"updated_since,omitempty" jsonschema:"RFC3339 lower bound on last_edited_time (client-side filter)"`
	Limit        int    `json:"limit,omitempty"         jsonschema:"1-100 (default 25)"`
	Cursor       string `json:"cursor,omitempty"        jsonschema:"opaque start_cursor from a prior call's next_cursor"`
}

type SearchResult struct {
	ID             string `json:"id"`
	Object         string `json:"object"`
	URL            string `json:"url,omitempty"`
	Title          string `json:"title,omitempty"`
	ParentType     string `json:"parent_type,omitempty"`
	CreatedTime    string `json:"created_time,omitempty"`
	LastEditedTime string `json:"last_edited_time,omitempty"`
	Archived       bool   `json:"archived,omitempty"`
}

type SearchOutput struct {
	Results    []SearchResult `json:"results"`
	NextCursor string         `json:"next_cursor,omitempty"`
	HasMore    bool           `json:"has_more"`
}

// rawSearchHit captures the union shape of /v1/search results across pages
// and data sources. Both populate the same top-level fields; titles come
// from different places — pages have a properties map with a title-typed
// entry, data sources have a top-level title rich_text array.
type rawSearchHit struct {
	Object         string                  `json:"object"`
	ID             string                  `json:"id"`
	URL            string                  `json:"url"`
	CreatedTime    string                  `json:"created_time"`
	LastEditedTime string                  `json:"last_edited_time"`
	Archived       bool                    `json:"archived"`
	InTrash        bool                    `json:"in_trash"`
	Parent         struct {
		Type string `json:"type"`
	} `json:"parent"`
	Properties map[string]pageProperty `json:"properties,omitempty"` // pages
	Title      []richText              `json:"title,omitempty"`      // data sources / databases
}

func runSearch(client *Client) mcp.ToolHandlerFor[SearchInput, SearchOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
		updatedSince, err := parseUpdatedSince(in.UpdatedSince)
		if err != nil {
			return nil, SearchOutput{}, fmt.Errorf("notion: %w", err)
		}

		objectType := strings.TrimSpace(in.ObjectType)
		if objectType == "" {
			objectType = "any"
		}
		switch objectType {
		case "any", "page":
			// ok
		case "data_source", "database":
			objectType = "data_source"
		default:
			return nil, SearchOutput{}, fmt.Errorf("notion: object_type must be 'page', 'data_source', or 'any' (got %q)", in.ObjectType)
		}

		body := map[string]any{
			"page_size": clampLimit(in.Limit),
			"sort": map[string]any{
				"direction": "descending",
				"timestamp": "last_edited_time",
			},
		}
		// Only attach the filter when narrowing — Notion treats an absent
		// filter as "both kinds", which is what object_type=any wants.
		if objectType != "any" {
			body["filter"] = map[string]any{
				"property": "object",
				"value":    objectType,
			}
		}
		if q := strings.TrimSpace(in.Query); q != "" {
			body["query"] = q
		}
		if in.Cursor != "" {
			body["start_cursor"] = in.Cursor
		}

		var resp struct {
			Results    []rawSearchHit `json:"results"`
			NextCursor string         `json:"next_cursor"`
			HasMore    bool           `json:"has_more"`
		}
		if err := client.do(ctx, http.MethodPost, "/search", body, &resp); err != nil {
			return nil, SearchOutput{}, fmt.Errorf("notion: search: %w", err)
		}

		out := SearchOutput{
			Results:    make([]SearchResult, 0, len(resp.Results)),
			NextCursor: resp.NextCursor,
			HasMore:    resp.HasMore,
		}
		for _, hit := range resp.Results {
			if updatedSince != "" && hit.LastEditedTime != "" && hit.LastEditedTime < updatedSince {
				continue
			}
			title := ""
			switch hit.Object {
			case "page":
				title = extractTitle(hit.Properties)
			default:
				// data_source / database — top-level title array
				title = richTextPlain(hit.Title)
			}
			out.Results = append(out.Results, SearchResult{
				ID:             hit.ID,
				Object:         hit.Object,
				URL:            hit.URL,
				Title:          title,
				ParentType:     hit.Parent.Type,
				CreatedTime:    hit.CreatedTime,
				LastEditedTime: hit.LastEditedTime,
				Archived:       hit.Archived || hit.InTrash,
			})
		}
		return nil, out, nil
	}
}

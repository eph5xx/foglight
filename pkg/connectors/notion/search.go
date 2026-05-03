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
		Description: "Search Notion pages and databases shared with the integration. " +
			"Notion's search is title-prefix-y, not full-text or semantic — phrase " +
			"queries against page titles for best recall. Filters by object_type " +
			"(page|database) and updated_since (RFC3339, client-side filter). " +
			"Cursor-paginated; returns summaries without body content.",
	}, runSearch(client))
}

// ---------- notion_search ----------

type SearchInput struct {
	Query        string `json:"query,omitempty" jsonschema:"title-prefix query (optional)"`
	ObjectType   string `json:"object_type,omitempty" jsonschema:"page|database (default: page)"`
	UpdatedSince string `json:"updated_since,omitempty" jsonschema:"RFC3339 lower bound on last_edited_time"`
	Limit        int    `json:"limit,omitempty" jsonschema:"1-100 (default 25)"`
	Cursor       string `json:"cursor,omitempty" jsonschema:"opaque start_cursor from a prior call's next_cursor"`
}

type searchResult struct {
	ID             string  `json:"id"`
	Object         string  `json:"object"`
	URL            string  `json:"url,omitempty"`
	Title          string  `json:"title,omitempty"`
	ParentType     string  `json:"parentType,omitempty"`
	CreatedTime    string  `json:"createdTime,omitempty"`
	LastEditedTime string  `json:"lastEditedTime,omitempty"`
	Archived       bool    `json:"archived,omitempty"`
}

type SearchOutput struct {
	Results    []searchResult `json:"results"`
	NextCursor string         `json:"nextCursor,omitempty"`
	HasMore    bool           `json:"hasMore"`
}

// rawSearchHit captures the union shape of /v1/search results. Pages and
// databases share most top-level fields; properties differ in shape, so we
// lift the title out of either side.
type rawSearchHit struct {
	Object         string                  `json:"object"`
	ID             string                  `json:"id"`
	URL            string                  `json:"url"`
	CreatedTime    string                  `json:"created_time"`
	LastEditedTime string                  `json:"last_edited_time"`
	Archived       bool                    `json:"archived"`
	Parent         struct {
		Type string `json:"type"`
	} `json:"parent"`
	// For pages: properties is a map of name -> {type, title?, ...}
	Properties map[string]pageProperty `json:"properties,omitempty"`
	// For databases: title is a top-level rich_text array.
	Title []richText `json:"title,omitempty"`
}

func runSearch(client *Client) mcp.ToolHandlerFor[SearchInput, SearchOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
		updatedSince, err := parseUpdatedSince(in.UpdatedSince)
		if err != nil {
			return nil, SearchOutput{}, fmt.Errorf("notion: %w", err)
		}

		objectType := strings.TrimSpace(in.ObjectType)
		if objectType == "" {
			objectType = "page"
		}
		if objectType != "page" && objectType != "database" {
			return nil, SearchOutput{}, fmt.Errorf("notion: object_type must be 'page' or 'database'")
		}

		body := map[string]any{
			"page_size": clampLimit(in.Limit),
			"filter": map[string]any{
				"property": "object",
				"value":    objectType,
			},
			"sort": map[string]any{
				"direction": "descending",
				"timestamp": "last_edited_time",
			},
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
			Results:    make([]searchResult, 0, len(resp.Results)),
			NextCursor: resp.NextCursor,
			HasMore:    resp.HasMore,
		}
		for _, hit := range resp.Results {
			// Notion's sort already orders by last_edited_time desc; once
			// we see a hit older than updated_since we can stop. But the
			// list is small (page_size <= 100), so a per-item filter is
			// fine and cheaper than reasoning about ordering edge cases.
			if updatedSince != "" && hit.LastEditedTime != "" && hit.LastEditedTime < updatedSince {
				continue
			}
			title := ""
			switch hit.Object {
			case "database":
				title = richTextPlain(hit.Title)
			default: // "page"
				title = extractTitle(hit.Properties)
			}
			out.Results = append(out.Results, searchResult{
				ID:             hit.ID,
				Object:         hit.Object,
				URL:            hit.URL,
				Title:          title,
				ParentType:     hit.Parent.Type,
				CreatedTime:    hit.CreatedTime,
				LastEditedTime: hit.LastEditedTime,
				Archived:       hit.Archived,
			})
		}
		return nil, out, nil
	}
}

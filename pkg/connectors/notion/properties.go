package notion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func addPropertyTools(server *mcp.Server, name string, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name: name + "_get_page_property",
		Description: "Read the full value of a single page property. Required for relations and " +
			"rollups with more than 25 entries — the page response truncates those at 25 and sets " +
			"has_more=true. Cursor-paginated. property_id is the property's stable id (the 'id' " +
			"field in the page's properties map), not the human-readable name.",
	}, getPageProperty(client))
}

// ---------- notion_get_page_property ----------

type GetPagePropertyInput struct {
	PageID     string `json:"page_id"     jsonschema:"page UUID or notion.so URL"`
	PropertyID string `json:"property_id" jsonschema:"the property's stable id (not its name)"`
	Cursor     string `json:"cursor,omitempty"`
	Limit      int    `json:"limit,omitempty" jsonschema:"1-100 (default 25)"`
}

// GetPagePropertyOutput is intentionally shallow — Notion's property-item
// responses are heterogeneous (single property objects vs. paginated lists
// of property_item objects), so we surface the raw payload and let the
// agent decode by type.
type GetPagePropertyOutput struct {
	Object     string          `json:"object"`
	Type       string          `json:"type,omitempty"`
	Results    json.RawMessage `json:"results,omitempty"`
	Property   json.RawMessage `json:"property_item,omitempty"`
	NextCursor string          `json:"next_cursor,omitempty"`
	HasMore    bool            `json:"has_more,omitempty"`
}

func getPageProperty(client *Client) mcp.ToolHandlerFor[GetPagePropertyInput, GetPagePropertyOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in GetPagePropertyInput) (*mcp.CallToolResult, GetPagePropertyOutput, error) {
		pageID := normalizeID(in.PageID)
		if pageID == "" {
			return nil, GetPagePropertyOutput{}, fmt.Errorf("notion: page_id is required")
		}
		propID := url.PathEscape(in.PropertyID)
		if propID == "" {
			return nil, GetPagePropertyOutput{}, fmt.Errorf("notion: property_id is required")
		}
		path := fmt.Sprintf("/pages/%s/properties/%s?page_size=%d", pageID, propID, clampLimit(in.Limit))
		if in.Cursor != "" {
			path += "&start_cursor=" + url.QueryEscape(in.Cursor)
		}
		var raw struct {
			Object     string          `json:"object"`
			Type       string          `json:"type,omitempty"`
			Results    json.RawMessage `json:"results,omitempty"`
			NextCursor string          `json:"next_cursor,omitempty"`
			HasMore    bool            `json:"has_more,omitempty"`

			// When the property is a single value (e.g. select, number) the
			// response IS the property_item object itself, not a wrapper.
			// We capture the whole body separately so the agent sees it.
			PropertyItem json.RawMessage `json:"property_item,omitempty"`
		}
		body, err := client.doRaw(ctx, http.MethodGet, path)
		if err != nil {
			return nil, GetPagePropertyOutput{}, fmt.Errorf("notion: get page property %q on %q: %w", in.PropertyID, pageID, err)
		}
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, GetPagePropertyOutput{}, fmt.Errorf("notion: decode page property: %w", err)
		}
		out := GetPagePropertyOutput{
			Object:     raw.Object,
			Type:       raw.Type,
			Results:    raw.Results,
			NextCursor: raw.NextCursor,
			HasMore:    raw.HasMore,
		}
		// For single-value responses (object=="property_item"), pass the
		// entire body through under property_item so the agent can decode
		// by type.
		if raw.Object == "property_item" {
			out.Property = body
		}
		return nil, out, nil
	}
}

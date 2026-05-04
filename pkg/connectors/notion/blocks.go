package notion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func addBlockTools(server *mcp.Server, name string, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name: name + "_get_block_children",
		Description: "List the direct children of a Notion block (or page — pages are blocks). " +
			"Returns one level only with raw type-specific payloads; recurse on has_children for " +
			"deeper subtrees, or use notion_fetch on a page id to get a server-flattened markdown " +
			"render in one call. Cursor-paginated, max 100 per page. " +
			"Note: synced_block clones reference their source via synced_from.block_id; this tool " +
			"does not follow that reference, so a recursive walk should track visited block ids to " +
			"avoid cycles.",
	}, getBlockChildren(client))
}

// ---------- notion_get_block_children ----------

type GetBlockChildrenInput struct {
	BlockID string `json:"block_id" jsonschema:"block UUID, page UUID, or notion.so URL"`
	Cursor  string `json:"cursor,omitempty"`
	Limit   int    `json:"limit,omitempty" jsonschema:"1-100 (default 100)"`
}

type Block struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	HasChildren bool            `json:"has_children"`
	Archived    bool            `json:"archived,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

type GetBlockChildrenOutput struct {
	Results    []Block `json:"results"`
	NextCursor string  `json:"next_cursor,omitempty"`
	HasMore    bool    `json:"has_more"`
}

// rawBlock decodes one /v1/blocks/{id}/children entry. The type-specific
// payload (e.g. "paragraph", "code") is captured via UnmarshalJSON into
// Payloads, then re-extracted on demand.
type rawBlock struct {
	Object      string                     `json:"object"`
	ID          string                     `json:"id"`
	Type        string                     `json:"type"`
	HasChildren bool                       `json:"has_children"`
	Archived    bool                       `json:"archived"`
	Payloads    map[string]json.RawMessage `json:"-"`
}

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

// blockFromRaw converts a rawBlock to the wire-facing Block struct, picking
// out the type-specific payload (the JSON value under the key matching
// b.Type). Container blocks like column_list have no payload here; their
// content lives one level deeper via another /v1/blocks/{id}/children call.
func blockFromRaw(b rawBlock) Block {
	out := Block{
		ID:          b.ID,
		Type:        b.Type,
		HasChildren: b.HasChildren,
		Archived:    b.Archived,
	}
	if payload, ok := b.Payloads[b.Type]; ok {
		out.Payload = payload
	}
	return out
}

func getBlockChildren(client *Client) mcp.ToolHandlerFor[GetBlockChildrenInput, GetBlockChildrenOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in GetBlockChildrenInput) (*mcp.CallToolResult, GetBlockChildrenOutput, error) {
		id := normalizeID(in.BlockID)
		if id == "" {
			return nil, GetBlockChildrenOutput{}, fmt.Errorf("notion: block_id is required")
		}
		path := fmt.Sprintf("/blocks/%s/children?page_size=%d", id, clampLimit(in.Limit))
		if in.Cursor != "" {
			path += "&start_cursor=" + url.QueryEscape(in.Cursor)
		}
		var resp struct {
			Results    []rawBlock `json:"results"`
			NextCursor string     `json:"next_cursor"`
			HasMore    bool       `json:"has_more"`
		}
		if err := client.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
			return nil, GetBlockChildrenOutput{}, fmt.Errorf("notion: get block children %q: %w", id, err)
		}
		out := GetBlockChildrenOutput{
			Results:    make([]Block, 0, len(resp.Results)),
			NextCursor: resp.NextCursor,
			HasMore:    resp.HasMore,
		}
		for _, b := range resp.Results {
			out.Results = append(out.Results, blockFromRaw(b))
		}
		return nil, out, nil
	}
}

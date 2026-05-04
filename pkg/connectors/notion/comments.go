package notion

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func addCommentTools(server *mcp.Server, name string, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name: name + "_get_comments",
		Description: "List un-resolved comments anchored at a Notion page or block. " +
			"Notion's REST API does not surface resolved comments — agents looking for an audit " +
			"of all discussion history will see only what is currently un-resolved. " +
			"Pages-are-blocks: pass a page id to get page-level comments, a block id to get " +
			"inline (block-anchored) comments. Cursor-paginated.",
	}, getComments(client))
}

// ---------- notion_get_comments ----------

type GetCommentsInput struct {
	ID     string `json:"id"     jsonschema:"page or block UUID, or notion.so URL"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty" jsonschema:"1-100 (default 25)"`
}

type Comment struct {
	ID              string `json:"id"`
	DiscussionID    string `json:"discussion_id"`
	ParentID        string `json:"parent_id,omitempty"`
	ParentType      string `json:"parent_type,omitempty"`
	ParentCommentID string `json:"parent_comment_id,omitempty"`
	RichTextPlain   string `json:"rich_text_plain"`
	CreatedBy       string `json:"created_by,omitempty"`
	CreatedTime     string `json:"created_time,omitempty"`
	LastEditedTime  string `json:"last_edited_time,omitempty"`
}

type GetCommentsOutput struct {
	Comments    []Comment           `json:"comments"`
	Discussions map[string][]string `json:"discussions"`
	NextCursor  string              `json:"next_cursor,omitempty"`
	HasMore     bool                `json:"has_more"`
}

type rawComment struct {
	Object       string `json:"object"`
	ID           string `json:"id"`
	DiscussionID string `json:"discussion_id"`
	Parent       struct {
		Type    string `json:"type"`
		PageID  string `json:"page_id,omitempty"`
		BlockID string `json:"block_id,omitempty"`
	} `json:"parent"`
	ParentCommentID string `json:"parent_comment_id,omitempty"`
	RichText        []richText `json:"rich_text"`
	CreatedBy       struct {
		ID string `json:"id"`
	} `json:"created_by"`
	CreatedTime    string `json:"created_time"`
	LastEditedTime string `json:"last_edited_time"`
}

func getComments(client *Client) mcp.ToolHandlerFor[GetCommentsInput, GetCommentsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in GetCommentsInput) (*mcp.CallToolResult, GetCommentsOutput, error) {
		id := normalizeID(in.ID)
		if id == "" {
			return nil, GetCommentsOutput{}, fmt.Errorf("notion: id is required")
		}
		path := fmt.Sprintf("/comments?block_id=%s&page_size=%d", id, clampLimit(in.Limit))
		if in.Cursor != "" {
			path += "&start_cursor=" + url.QueryEscape(in.Cursor)
		}
		var resp struct {
			Results    []rawComment `json:"results"`
			NextCursor string       `json:"next_cursor"`
			HasMore    bool         `json:"has_more"`
		}
		if err := client.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
			return nil, GetCommentsOutput{}, fmt.Errorf("notion: get comments on %q: %w", id, err)
		}
		out := GetCommentsOutput{
			Comments:    make([]Comment, 0, len(resp.Results)),
			Discussions: map[string][]string{},
			NextCursor:  resp.NextCursor,
			HasMore:     resp.HasMore,
		}
		for _, c := range resp.Results {
			parentID := c.Parent.PageID
			if parentID == "" {
				parentID = c.Parent.BlockID
			}
			cmt := Comment{
				ID:              c.ID,
				DiscussionID:    c.DiscussionID,
				ParentID:        parentID,
				ParentType:      c.Parent.Type,
				ParentCommentID: c.ParentCommentID,
				RichTextPlain:   richTextPlain(c.RichText),
				CreatedBy:       c.CreatedBy.ID,
				CreatedTime:     c.CreatedTime,
				LastEditedTime:  c.LastEditedTime,
			}
			out.Comments = append(out.Comments, cmt)
			out.Discussions[c.DiscussionID] = append(out.Discussions[c.DiscussionID], c.ID)
		}
		return nil, out, nil
	}
}

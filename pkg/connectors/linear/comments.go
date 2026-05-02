package linear

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func addCommentTools(server *mcp.Server, name string, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        name + "_list_comments",
		Description: "List comments on a Linear issue. issue_id accepts UUID or human identifier (e.g. 'ENG-123'). Cursor-paginated. Bodies are truncated.",
	}, listComments(client))
}

// ---------- linear_list_comments ----------

type ListCommentsInput struct {
	IssueID string `json:"issue_id" jsonschema:"issue UUID or identifier (e.g. 'ENG-123')"`
	Limit   int    `json:"limit,omitempty" jsonschema:"1-100 (default 30)"`
	Cursor  string `json:"cursor,omitempty" jsonschema:"opaque cursor"`
}

type ListCommentsOutput struct {
	Comments   []IssueComment `json:"comments"`
	NextCursor string         `json:"nextCursor,omitempty"`
	HasMore    bool           `json:"hasMore"`
}

const listCommentsQuery = `
query ListComments($id: String!, $first: Int!, $after: String) {
	issue(id: $id) {
		id
		comments(first: $first, after: $after) {
			nodes {
				id
				body
				createdAt
				updatedAt
				user { id name displayName email }
			}
			pageInfo { endCursor hasNextPage }
		}
	}
}`

func listComments(client *Client) mcp.ToolHandlerFor[ListCommentsInput, ListCommentsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListCommentsInput) (*mcp.CallToolResult, ListCommentsOutput, error) {
		id := strings.TrimSpace(in.IssueID)
		if id == "" {
			return nil, ListCommentsOutput{}, fmt.Errorf("linear: issue_id is required")
		}

		vars := map[string]any{
			"id":    id,
			"first": clampLimit(in.Limit),
		}
		if in.Cursor != "" {
			vars["after"] = in.Cursor
		}

		var resp struct {
			Issue *struct {
				ID       string `json:"id"`
				Comments struct {
					Nodes []struct {
						ID        string   `json:"id"`
						Body      string   `json:"body"`
						CreatedAt string   `json:"createdAt"`
						UpdatedAt string   `json:"updatedAt"`
						User      *userRef `json:"user"`
					} `json:"nodes"`
					PageInfo PageInfo `json:"pageInfo"`
				} `json:"comments"`
			} `json:"issue"`
		}
		if err := client.do(ctx, listCommentsQuery, vars, &resp); err != nil {
			return nil, ListCommentsOutput{}, fmt.Errorf("linear: list comments for %q: %w", id, err)
		}
		if resp.Issue == nil {
			return nil, ListCommentsOutput{}, fmt.Errorf("linear: issue %q not found", id)
		}

		comments := make([]IssueComment, 0, len(resp.Issue.Comments.Nodes))
		for _, c := range resp.Issue.Comments.Nodes {
			body, trunc := truncateString(c.Body, maxCommentBodyChars)
			comments = append(comments, IssueComment{
				ID:            c.ID,
				Body:          body,
				BodyTruncated: trunc,
				CreatedAt:     c.CreatedAt,
				UpdatedAt:     c.UpdatedAt,
				User:          c.User,
			})
		}
		return nil, ListCommentsOutput{
			Comments:   comments,
			NextCursor: resp.Issue.Comments.PageInfo.EndCursor,
			HasMore:    resp.Issue.Comments.PageInfo.HasNextPage,
		}, nil
	}
}

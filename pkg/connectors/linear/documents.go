package linear

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxDocumentContentChars = 32000

func addDocumentTools(server *mcp.Server, name string, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        name + "_list_documents",
		Description: "List Linear Docs (workspace wiki). Filter by project_id, creator_id, or updated_since. Returns summaries without content.",
	}, listDocuments(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        name + "_get_document",
		Description: "Get a single Linear Doc by UUID, including its full content (truncated).",
	}, getDocument(client))
}

// ---------- linear_list_documents ----------

type documentSummary struct {
	ID        string      `json:"id"`
	Title     string      `json:"title"`
	Icon      string      `json:"icon,omitempty"`
	URL       string      `json:"url,omitempty"`
	SlugID    string      `json:"slugId,omitempty"`
	Creator   *userRef    `json:"creator,omitempty"`
	Project   *projectRef `json:"project,omitempty"`
	CreatedAt string      `json:"createdAt,omitempty"`
	UpdatedAt string      `json:"updatedAt,omitempty"`
}

type ListDocumentsInput struct {
	ProjectID    string `json:"project_id,omitempty" jsonschema:"project UUID"`
	CreatorID    string `json:"creator_id,omitempty" jsonschema:"user UUID of document creator"`
	UpdatedSince string `json:"updated_since,omitempty" jsonschema:"RFC3339 lower bound on updatedAt"`
	Limit        int    `json:"limit,omitempty" jsonschema:"1-100 (default 30)"`
	Cursor       string `json:"cursor,omitempty" jsonschema:"opaque cursor"`
}

type ListDocumentsOutput struct {
	Documents  []documentSummary `json:"documents"`
	NextCursor string            `json:"nextCursor,omitempty"`
	HasMore    bool              `json:"hasMore"`
}

const listDocumentsQuery = `
query ListDocuments($first: Int!, $after: String, $filter: DocumentFilter) {
	documents(first: $first, after: $after, filter: $filter) {
		nodes {
			id
			title
			icon
			url
			slugId
			creator { id name displayName email }
			project { id name }
			createdAt
			updatedAt
		}
		pageInfo { endCursor hasNextPage }
	}
}`

func listDocuments(client *Client) mcp.ToolHandlerFor[ListDocumentsInput, ListDocumentsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListDocumentsInput) (*mcp.CallToolResult, ListDocumentsOutput, error) {
		updatedSince, err := parseUpdatedSince(in.UpdatedSince)
		if err != nil {
			return nil, ListDocumentsOutput{}, fmt.Errorf("linear: %w", err)
		}

		filter := map[string]any{}
		if in.ProjectID != "" {
			filter["project"] = map[string]any{"id": eqFilter(in.ProjectID)}
		}
		if in.CreatorID != "" {
			filter["creator"] = map[string]any{"id": eqFilter(in.CreatorID)}
		}
		if updatedSince != "" {
			filter["updatedAt"] = map[string]any{"gte": updatedSince}
		}

		extra := map[string]any{}
		if len(filter) > 0 {
			extra["filter"] = filter
		}

		var resp struct {
			Documents struct {
				Nodes    []documentSummary `json:"nodes"`
				PageInfo PageInfo          `json:"pageInfo"`
			} `json:"documents"`
		}
		if err := client.do(ctx, listDocumentsQuery, pageVars(in.Limit, in.Cursor, extra), &resp); err != nil {
			return nil, ListDocumentsOutput{}, fmt.Errorf("linear: list documents: %w", err)
		}
		out := ListDocumentsOutput{
			Documents:  resp.Documents.Nodes,
			NextCursor: resp.Documents.PageInfo.EndCursor,
			HasMore:    resp.Documents.PageInfo.HasNextPage,
		}
		if out.Documents == nil {
			out.Documents = []documentSummary{}
		}
		return nil, out, nil
	}
}

// ---------- linear_get_document ----------

type GetDocumentInput struct {
	ID string `json:"id" jsonschema:"document UUID"`
}

type GetDocumentOutput struct {
	ID               string      `json:"id"`
	Title            string      `json:"title"`
	Icon             string      `json:"icon,omitempty"`
	URL              string      `json:"url,omitempty"`
	SlugID           string      `json:"slugId,omitempty"`
	Content          string      `json:"content,omitempty"`
	ContentTruncated bool        `json:"contentTruncated,omitempty"`
	Creator          *userRef    `json:"creator,omitempty"`
	Updater          *userRef    `json:"updater,omitempty"`
	Project          *projectRef `json:"project,omitempty"`
	CreatedAt        string      `json:"createdAt,omitempty"`
	UpdatedAt        string      `json:"updatedAt,omitempty"`
}

const getDocumentQuery = `
query GetDocument($id: String!) {
	document(id: $id) {
		id
		title
		icon
		url
		slugId
		content
		creator { id name displayName email }
		updatedBy { id name displayName email }
		project { id name }
		createdAt
		updatedAt
	}
}`

func getDocument(client *Client) mcp.ToolHandlerFor[GetDocumentInput, GetDocumentOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in GetDocumentInput) (*mcp.CallToolResult, GetDocumentOutput, error) {
		id := strings.TrimSpace(in.ID)
		if id == "" {
			return nil, GetDocumentOutput{}, fmt.Errorf("linear: id is required")
		}

		var resp struct {
			Document *struct {
				ID        string      `json:"id"`
				Title     string      `json:"title"`
				Icon      string      `json:"icon"`
				URL       string      `json:"url"`
				SlugID    string      `json:"slugId"`
				Content   string      `json:"content"`
				Creator   *userRef    `json:"creator"`
				UpdatedBy *userRef    `json:"updatedBy"`
				Project   *projectRef `json:"project"`
				CreatedAt string      `json:"createdAt"`
				UpdatedAt string      `json:"updatedAt"`
			} `json:"document"`
		}
		if err := client.do(ctx, getDocumentQuery, map[string]any{"id": id}, &resp); err != nil {
			return nil, GetDocumentOutput{}, fmt.Errorf("linear: get document %q: %w", id, err)
		}
		if resp.Document == nil {
			return nil, GetDocumentOutput{}, fmt.Errorf("linear: document %q not found", id)
		}

		content, trunc := truncateString(resp.Document.Content, maxDocumentContentChars)
		return nil, GetDocumentOutput{
			ID:               resp.Document.ID,
			Title:            resp.Document.Title,
			Icon:             resp.Document.Icon,
			URL:              resp.Document.URL,
			SlugID:           resp.Document.SlugID,
			Content:          content,
			ContentTruncated: trunc,
			Creator:          resp.Document.Creator,
			Updater:          resp.Document.UpdatedBy,
			Project:          resp.Document.Project,
			CreatedAt:        resp.Document.CreatedAt,
			UpdatedAt:        resp.Document.UpdatedAt,
		}, nil
	}
}

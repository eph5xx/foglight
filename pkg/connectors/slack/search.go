package slack

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	slacksdk "github.com/slack-go/slack"
)

const (
	defaultSearchCount = 20
	maxSearchCount     = 100
)

func addSearchTools(server *mcp.Server, name string, client *slacksdk.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        name + "_search_messages",
		Description: "Search messages workspace-wide using Slack search operators (in:#channel, from:@user, before:YYYY-MM-DD, after:, on:, has::eyes:, etc.). Returns message permalinks plus surrounding context. Page-based pagination (1-indexed). Note: bot tokens cannot use this; xoxc/xoxp only.",
	}, searchMessages(client))
}

type SearchMessagesInput struct {
	Query         string `json:"query" jsonschema:"Slack search query; supports operators like in:#chan from:@user before:2026-01-01"`
	Sort          string `json:"sort,omitempty" jsonschema:"score|timestamp (default score)"`
	SortDirection string `json:"sort_direction,omitempty" jsonschema:"asc|desc (default desc)"`
	Count         int    `json:"count,omitempty" jsonschema:"results per page, 1-100 (default 20)"`
	Page          int    `json:"page,omitempty" jsonschema:"1-indexed page number (default 1)"`
}

// SearchMessageHit is a single match from search.messages. Note: we do
// not surface IsIM — Slack's search.messages CtxChannel payload omits
// it, so direct messages are indistinguishable from regular channels in
// search results. Channel IDs starting with 'D' are DMs if the caller
// needs to disambiguate.
type SearchMessageHit struct {
	TS            string `json:"ts"`
	Time          string `json:"time,omitempty"`
	User          string `json:"user,omitempty"`
	Username      string `json:"username,omitempty"`
	ChannelID     string `json:"channelId,omitempty"`
	Channel       string `json:"channel,omitempty"`
	IsPrivate     bool   `json:"isPrivate,omitempty"`
	IsMpIM        bool   `json:"isMpIM,omitempty"`
	Text          string `json:"text,omitempty"`
	TextTruncated bool   `json:"textTruncated,omitempty"`
	Permalink     string `json:"permalink,omitempty"`
}

type SearchMessagesOutput struct {
	Hits      []SearchMessageHit `json:"hits"`
	Total     int                `json:"total"`
	Page      int                `json:"page"`
	PageCount int                `json:"pageCount"`
	HasMore   bool               `json:"hasMore"`
}

func searchMessages(client *slacksdk.Client) mcp.ToolHandlerFor[SearchMessagesInput, SearchMessagesOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in SearchMessagesInput) (*mcp.CallToolResult, SearchMessagesOutput, error) {
		query := strings.TrimSpace(in.Query)
		if query == "" {
			return nil, SearchMessagesOutput{}, fmt.Errorf("slack: query is required")
		}

		params := slacksdk.NewSearchParameters()
		if in.Sort != "" {
			params.Sort = in.Sort
		}
		if in.SortDirection != "" {
			params.SortDirection = in.SortDirection
		}
		params.Count = clampLimit(in.Count, defaultSearchCount, maxSearchCount)
		if in.Page > 0 {
			params.Page = in.Page
		} else {
			params.Page = 1
		}

		resp, err := client.SearchMessagesContext(ctx, query, params)
		if err != nil {
			return nil, SearchMessagesOutput{}, fmt.Errorf("slack: search messages: %w", err)
		}

		out := SearchMessagesOutput{
			Hits:      make([]SearchMessageHit, 0, len(resp.Matches)),
			Total:     resp.Total,
			Page:      resp.Pagination.Page,
			PageCount: resp.Pagination.PageCount,
			HasMore:   resp.Pagination.Page < resp.Pagination.PageCount,
		}
		for i := range resp.Matches {
			m := &resp.Matches[i]
			text, trunc := truncateString(m.Text, maxMessageChars)
			out.Hits = append(out.Hits, SearchMessageHit{
				TS:            m.Timestamp,
				Time:          slackTSToRFC3339(m.Timestamp),
				User:          m.User,
				Username:      m.Username,
				ChannelID:     m.Channel.ID,
				Channel:       m.Channel.Name,
				IsPrivate:     m.Channel.IsPrivate,
				IsMpIM:        m.Channel.IsMPIM,
				Text:          text,
				TextTruncated: trunc,
				Permalink:     m.Permalink,
			})
		}
		return nil, out, nil
	}
}

package slack

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	slacksdk "github.com/slack-go/slack"
)

func addChannelTools(server *mcp.Server, name string, client *slacksdk.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        name + "_list_channels",
		Description: "List channels in the workspace, optionally filtered by type (public_channel, private_channel, mpim, im). Cursor-paginated. Resolve channel IDs here before calling slack_read_channel.",
	}, listChannels(client))
}

type ListChannelsInput struct {
	Types           string `json:"types,omitempty" jsonschema:"comma-separated: public_channel,private_channel,mpim,im (default: public_channel,private_channel)"`
	ExcludeArchived bool   `json:"exclude_archived,omitempty" jsonschema:"default false"`
	Limit           int    `json:"limit,omitempty" jsonschema:"1-100 (default 30)"`
	Cursor          string `json:"cursor,omitempty" jsonschema:"opaque cursor from a prior call's nextCursor"`
}

type ChannelSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	IsPrivate   bool   `json:"isPrivate,omitempty"`
	IsIM        bool   `json:"isIM,omitempty"`
	IsMpIM      bool   `json:"isMpIM,omitempty"`
	IsArchived  bool   `json:"isArchived,omitempty"`
	NumMembers  int    `json:"numMembers,omitempty"`
	Topic       string `json:"topic,omitempty"`
	Purpose     string `json:"purpose,omitempty"`
	UserID      string `json:"userId,omitempty"` // for IMs: the other user
}

type ListChannelsOutput struct {
	Channels   []ChannelSummary `json:"channels"`
	NextCursor string           `json:"nextCursor,omitempty"`
	HasMore    bool             `json:"hasMore"`
}

func listChannels(client *slacksdk.Client) mcp.ToolHandlerFor[ListChannelsInput, ListChannelsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListChannelsInput) (*mcp.CallToolResult, ListChannelsOutput, error) {
		types := parseTypes(in.Types)
		params := &slacksdk.GetConversationsParameters{
			Cursor:          in.Cursor,
			ExcludeArchived: in.ExcludeArchived,
			Limit:           clampLimit(in.Limit, defaultLimit, maxLimit),
			Types:           types,
		}

		channels, nextCursor, err := client.GetConversationsContext(ctx, params)
		if err != nil {
			return nil, ListChannelsOutput{}, fmt.Errorf("slack: list channels: %w", err)
		}

		out := ListChannelsOutput{
			Channels:   make([]ChannelSummary, 0, len(channels)),
			NextCursor: nextCursor,
			HasMore:    nextCursor != "",
		}
		for _, c := range channels {
			out.Channels = append(out.Channels, channelToSummary(&c))
		}
		return nil, out, nil
	}
}

func parseTypes(raw string) []string {
	if raw == "" {
		return []string{"public_channel", "private_channel"}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return []string{"public_channel", "private_channel"}
	}
	return out
}

func channelToSummary(c *slacksdk.Channel) ChannelSummary {
	return ChannelSummary{
		ID:         c.ID,
		Name:       c.Name,
		IsPrivate:  c.IsPrivate,
		IsIM:       c.IsIM,
		IsMpIM:     c.IsMpIM,
		IsArchived: c.IsArchived,
		NumMembers: c.NumMembers,
		Topic:      c.Topic.Value,
		Purpose:    c.Purpose.Value,
		UserID:     c.User,
	}
}

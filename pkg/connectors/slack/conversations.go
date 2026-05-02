package slack

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	slacksdk "github.com/slack-go/slack"
)

const (
	defaultUnreadConversations  = 30
	maxUnreadConversations      = 100
	defaultUnreadMessagesPerConv = 20
	maxUnreadMessagesPerConv    = 50
)

func addConversationTools(server *mcp.Server, name string, client *slacksdk.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        name + "_read_channel",
		Description: "Read recent messages from a channel or DM by channel_id. Supports oldest/latest Unix-second timestamps and a limit. Cursor-paginated.",
	}, readChannel(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        name + "_read_thread",
		Description: "Read a full thread by channel_id + thread_ts. Threads are usually higher-signal than channel surface — go here for decision context.",
	}, readThread(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        name + "_unreads",
		Description: "Aggregate unread messages across the user's DMs and channels. Best-effort: iterates user's conversations (default 30, max 100) and pulls messages newer than each last_read marker. Slow on large workspaces. mentions_only=true filters to plain @-you mentions only — does not match @-here, @-channel, or usergroup mentions.",
	}, unreads(client))
}

// ---------- shared types ----------

type MessageSummary struct {
	TS             string `json:"ts"`
	Time           string `json:"time,omitempty"` // RFC3339
	User           string `json:"user,omitempty"`
	Username       string `json:"username,omitempty"`
	BotID          string `json:"botId,omitempty"`
	SubType        string `json:"subtype,omitempty"`
	Text           string `json:"text,omitempty"`
	TextTruncated  bool   `json:"textTruncated,omitempty"`
	ThreadTS       string `json:"threadTS,omitempty"`
	ReplyCount     int    `json:"replyCount,omitempty"`
	LatestReply    string `json:"latestReply,omitempty"`
	IsThreadParent bool   `json:"isThreadParent,omitempty"`
}

func msgToSummary(m *slacksdk.Msg) MessageSummary {
	text, trunc := truncateString(m.Text, maxMessageChars)
	return MessageSummary{
		TS:             m.Timestamp,
		Time:           slackTSToRFC3339(m.Timestamp),
		User:           m.User,
		Username:       m.Username,
		BotID:          m.BotID,
		SubType:        m.SubType,
		Text:           text,
		TextTruncated:  trunc,
		ThreadTS:       m.ThreadTimestamp,
		ReplyCount:     m.ReplyCount,
		LatestReply:    m.LatestReply,
		IsThreadParent: m.ThreadTimestamp != "" && m.ThreadTimestamp == m.Timestamp,
	}
}

// ---------- slack_read_channel ----------

type ReadChannelInput struct {
	ChannelID string `json:"channel_id" jsonschema:"channel ID like C0123ABCD or D0123ABCD; resolve via slack_list_channels"`
	Oldest    string `json:"oldest,omitempty" jsonschema:"Unix-second start (e.g. '1704067200'); messages strictly newer unless inclusive=true"`
	Latest    string `json:"latest,omitempty" jsonschema:"Unix-second end; messages strictly older unless inclusive=true"`
	Inclusive bool   `json:"inclusive,omitempty" jsonschema:"include exact-match boundary messages"`
	Limit     int    `json:"limit,omitempty" jsonschema:"1-100 (default 30)"`
	Cursor    string `json:"cursor,omitempty" jsonschema:"opaque cursor from a prior call's nextCursor"`
}

type ReadChannelOutput struct {
	Messages   []MessageSummary `json:"messages"`
	NextCursor string           `json:"nextCursor,omitempty"`
	HasMore    bool             `json:"hasMore"`
}

func readChannel(client *slacksdk.Client) mcp.ToolHandlerFor[ReadChannelInput, ReadChannelOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ReadChannelInput) (*mcp.CallToolResult, ReadChannelOutput, error) {
		if strings.TrimSpace(in.ChannelID) == "" {
			return nil, ReadChannelOutput{}, fmt.Errorf("slack: channel_id is required")
		}

		params := &slacksdk.GetConversationHistoryParameters{
			ChannelID: in.ChannelID,
			Cursor:    in.Cursor,
			Inclusive: in.Inclusive,
			Latest:    in.Latest,
			Oldest:    in.Oldest,
			Limit:     clampLimit(in.Limit, defaultLimit, maxLimit),
		}
		resp, err := client.GetConversationHistoryContext(ctx, params)
		if err != nil {
			return nil, ReadChannelOutput{}, fmt.Errorf("slack: read channel %q: %w", in.ChannelID, err)
		}

		out := ReadChannelOutput{
			Messages:   make([]MessageSummary, 0, len(resp.Messages)),
			NextCursor: resp.ResponseMetaData.NextCursor,
			HasMore:    resp.HasMore,
		}
		for i := range resp.Messages {
			out.Messages = append(out.Messages, msgToSummary(&resp.Messages[i].Msg))
		}
		return nil, out, nil
	}
}

// ---------- slack_read_thread ----------

type ReadThreadInput struct {
	ChannelID string `json:"channel_id" jsonschema:"channel ID where the thread lives"`
	ThreadTS  string `json:"thread_ts" jsonschema:"parent message ts (the 'ts' field of the thread root)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"1-100 (default 30)"`
	Cursor    string `json:"cursor,omitempty" jsonschema:"opaque cursor"`
}

type ReadThreadOutput struct {
	Messages   []MessageSummary `json:"messages"`
	NextCursor string           `json:"nextCursor,omitempty"`
	HasMore    bool             `json:"hasMore"`
}

func readThread(client *slacksdk.Client) mcp.ToolHandlerFor[ReadThreadInput, ReadThreadOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ReadThreadInput) (*mcp.CallToolResult, ReadThreadOutput, error) {
		if strings.TrimSpace(in.ChannelID) == "" {
			return nil, ReadThreadOutput{}, fmt.Errorf("slack: channel_id is required")
		}
		if strings.TrimSpace(in.ThreadTS) == "" {
			return nil, ReadThreadOutput{}, fmt.Errorf("slack: thread_ts is required")
		}

		params := &slacksdk.GetConversationRepliesParameters{
			ChannelID: in.ChannelID,
			Timestamp: in.ThreadTS,
			Cursor:    in.Cursor,
			Limit:     clampLimit(in.Limit, defaultLimit, maxLimit),
		}
		msgs, hasMore, nextCursor, err := client.GetConversationRepliesContext(ctx, params)
		if err != nil {
			return nil, ReadThreadOutput{}, fmt.Errorf("slack: read thread %s/%s: %w", in.ChannelID, in.ThreadTS, err)
		}

		out := ReadThreadOutput{
			Messages:   make([]MessageSummary, 0, len(msgs)),
			NextCursor: nextCursor,
			HasMore:    hasMore,
		}
		for i := range msgs {
			out.Messages = append(out.Messages, msgToSummary(&msgs[i].Msg))
		}
		return nil, out, nil
	}
}

// ---------- slack_unreads ----------

type UnreadsInput struct {
	MentionsOnly       bool `json:"mentions_only,omitempty" jsonschema:"only return messages with a plain @-mention of the calling user; does not match @-here, @-channel, or usergroup mentions"`
	MaxConversations   int  `json:"max_conversations,omitempty" jsonschema:"how many conversations to scan (default 30, max 100)"`
	MaxMessagesPerConv int  `json:"max_messages_per_conv,omitempty" jsonschema:"per-conversation message cap (default 20, max 50)"`
}

type UnreadConversation struct {
	ChannelID   string           `json:"channelId"`
	ChannelName string           `json:"channelName,omitempty"`
	IsIM        bool             `json:"isIM,omitempty"`
	IsMpIM      bool             `json:"isMpIM,omitempty"`
	IsPrivate   bool             `json:"isPrivate,omitempty"`
	UnreadCount int              `json:"unreadCount,omitempty"`
	LastReadTS  string           `json:"lastReadTS,omitempty"`
	Messages    []MessageSummary `json:"messages"`
	HasMore     bool             `json:"hasMore,omitempty"`
}

type UnreadsOutput struct {
	UserID               string               `json:"userId"`
	Conversations        []UnreadConversation `json:"conversations"`
	ConversationsScanned int                  `json:"conversationsScanned"`
	// ConversationsSkipped counts conversations the loop walked past
	// without producing output (failed introspection, no last_read, no
	// unread count, history fetch errored, no messages matched filter).
	// A nonzero count with empty Conversations and a non-empty FirstError
	// is the signal that something is wrong, not that you're caught up.
	ConversationsSkipped int    `json:"conversationsSkipped,omitempty"`
	FirstError           string `json:"firstError,omitempty"`
	Truncated            bool   `json:"truncated,omitempty"` // user has more conversations than we scanned
}

func unreads(client *slacksdk.Client) mcp.ToolHandlerFor[UnreadsInput, UnreadsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in UnreadsInput) (*mcp.CallToolResult, UnreadsOutput, error) {
		auth, err := client.AuthTestContext(ctx)
		if err != nil {
			return nil, UnreadsOutput{}, fmt.Errorf("slack: auth test: %w", err)
		}
		mentionToken := "<@" + auth.UserID + ">"

		maxConvs := clampLimit(in.MaxConversations, defaultUnreadConversations, maxUnreadConversations)
		maxPerConv := clampLimit(in.MaxMessagesPerConv, defaultUnreadMessagesPerConv, maxUnreadMessagesPerConv)

		// Pull the user's conversations. Prioritize DMs and group DMs by
		// listing them first, then channels, since that matches what the
		// Slack client itself surfaces as "what did I miss".
		convs, truncated, err := collectUserConversations(ctx, client, auth.UserID, maxConvs)
		if err != nil {
			return nil, UnreadsOutput{}, fmt.Errorf("slack: list user conversations: %w", err)
		}

		out := UnreadsOutput{
			UserID:               auth.UserID,
			Conversations:        make([]UnreadConversation, 0),
			ConversationsScanned: len(convs),
			Truncated:            truncated,
		}

		recordSkip := func(err error) {
			out.ConversationsSkipped++
			if err != nil && out.FirstError == "" {
				out.FirstError = err.Error()
			}
		}

		for i := range convs {
			c := &convs[i]
			info, err := client.GetConversationInfoContext(ctx, &slacksdk.GetConversationInfoInput{
				ChannelID: c.ID,
			})
			if err != nil {
				recordSkip(fmt.Errorf("conversations.info %s: %w", c.ID, err))
				continue
			}
			lastRead := info.LastRead
			if lastRead == "" {
				recordSkip(nil)
				continue
			}

			// Quick check: if there's no unread count surfaced on the
			// channel object, skip the history call entirely. Slack only
			// populates UnreadCountDisplay when the user has at least one
			// unread.
			if info.UnreadCountDisplay == 0 {
				recordSkip(nil)
				continue
			}

			hist, err := client.GetConversationHistoryContext(ctx, &slacksdk.GetConversationHistoryParameters{
				ChannelID: c.ID,
				Oldest:    lastRead,
				Limit:     maxPerConv,
			})
			if err != nil {
				recordSkip(fmt.Errorf("conversations.history %s: %w", c.ID, err))
				continue
			}

			msgs := make([]MessageSummary, 0, len(hist.Messages))
			for j := range hist.Messages {
				m := &hist.Messages[j].Msg
				if in.MentionsOnly && !strings.Contains(m.Text, mentionToken) {
					continue
				}
				msgs = append(msgs, msgToSummary(m))
			}
			if len(msgs) == 0 {
				recordSkip(nil)
				continue
			}

			out.Conversations = append(out.Conversations, UnreadConversation{
				ChannelID:   c.ID,
				ChannelName: c.Name,
				IsIM:        c.IsIM,
				IsMpIM:      c.IsMpIM,
				IsPrivate:   c.IsPrivate,
				UnreadCount: info.UnreadCountDisplay,
				LastReadTS:  lastRead,
				Messages:    msgs,
				HasMore:     hist.HasMore,
			})
		}

		return nil, out, nil
	}
}

// collectUserConversations pulls the user's conversations in priority
// order: DMs/MPIMs first, then private channels, then public channels.
// Stops once we've collected `cap` total. Returns (convs, truncated)
// where truncated is true if Slack had more pages we didn't read.
func collectUserConversations(ctx context.Context, client *slacksdk.Client, userID string, cap int) ([]slacksdk.Channel, bool, error) {
	priority := [][]string{
		{"im", "mpim"},
		{"private_channel"},
		{"public_channel"},
	}

	out := make([]slacksdk.Channel, 0, cap)
	truncated := false

	for _, types := range priority {
		if len(out) >= cap {
			truncated = true
			break
		}
		cursor := ""
		for {
			remaining := cap - len(out)
			if remaining <= 0 {
				break
			}
			limit := remaining
			if limit > 200 {
				limit = 200
			}
			channels, nextCursor, err := client.GetConversationsForUserContext(ctx, &slacksdk.GetConversationsForUserParameters{
				UserID:          userID,
				Cursor:          cursor,
				Types:           types,
				Limit:           limit,
				ExcludeArchived: true,
			})
			if err != nil {
				return nil, false, err
			}
			for i := range channels {
				if len(out) >= cap {
					if nextCursor != "" {
						truncated = true
					}
					break
				}
				out = append(out, channels[i])
			}
			if nextCursor == "" || len(out) >= cap {
				if nextCursor != "" {
					truncated = true
				}
				break
			}
			cursor = nextCursor
		}
	}
	return out, truncated, nil
}

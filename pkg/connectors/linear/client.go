package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var nullJSON = []byte("null")

const (
	defaultEndpoint  = "https://api.linear.app/graphql"
	defaultUserAgent = "foglight/v0.1.0"
	defaultTimeout   = 30 * time.Second
	maxErrSnippet    = 512
)

// Client is a thin wrapper around Linear's GraphQL API.
//
// Auth uses a personal API key in the Authorization header *as-is* —
// not "Bearer <key>". Linear is the rare service that wants the raw key.
type Client struct {
	httpClient *http.Client
	endpoint   string
	apiKey     string
	userAgent  string
}

func newClient(apiKey string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
		endpoint:   defaultEndpoint,
		apiKey:     apiKey,
		userAgent:  defaultUserAgent,
	}
}

type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type graphQLResponse struct {
	Data   json.RawMessage  `json:"data"`
	Errors []GraphQLError   `json:"errors,omitempty"`
}

// GraphQLError is one entry in a GraphQL `errors` array. Linear puts the
// best human-facing string in extensions.userPresentableMessage.
type GraphQLError struct {
	Message    string         `json:"message"`
	Path       []any          `json:"path,omitempty"`
	Extensions map[string]any `json:"extensions,omitempty"`
}

func (e *GraphQLError) Error() string {
	if e == nil {
		return ""
	}
	if msg, ok := e.Extensions["userPresentableMessage"].(string); ok && msg != "" {
		return msg
	}
	return e.Message
}

// graphQLErrors wraps multiple GraphQL errors into one error.
type graphQLErrors []GraphQLError

func (es graphQLErrors) Error() string {
	parts := make([]string, 0, len(es))
	for i := range es {
		parts = append(parts, (&es[i]).Error())
	}
	return "linear: graphql: " + strings.Join(parts, "; ")
}

// do executes a GraphQL query and decodes data into out.
func (c *Client) do(ctx context.Context, query string, variables map[string]any, out any) error {
	body, err := json.Marshal(graphQLRequest{Query: query, Variables: variables})
	if err != nil {
		return fmt.Errorf("linear: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("linear: build request: %w", err)
	}
	req.Header.Set("Authorization", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("linear: http: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("linear: read response: %w", err)
	}

	if resp.StatusCode/100 != 2 {
		snippet, _ := truncateString(strings.TrimSpace(string(respBody)), maxErrSnippet)
		return fmt.Errorf("linear: http %d: %s", resp.StatusCode, snippet)
	}

	var env graphQLResponse
	if err := json.Unmarshal(respBody, &env); err != nil {
		snippet, _ := truncateString(strings.TrimSpace(string(respBody)), maxErrSnippet)
		return fmt.Errorf("linear: decode response: %w (body: %s)", err, snippet)
	}
	if len(env.Errors) > 0 {
		return graphQLErrors(env.Errors)
	}
	if out == nil {
		return nil
	}
	// `data: null` (4 bytes) and missing data (0 bytes) both mean the
	// query returned nothing. Without this guard, `null` decodes into a
	// struct silently, so a list query would return an empty list with
	// no error — masking real failures. Each get_* handler also checks
	// for nil sub-objects (e.g. resp.Issue == nil), but list handlers
	// rely on this layer to fail loudly.
	if len(env.Data) == 0 || bytes.Equal(env.Data, nullJSON) {
		return fmt.Errorf("linear: empty data in response")
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("linear: decode data: %w", err)
	}
	return nil
}

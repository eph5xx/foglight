package notion

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

const (
	defaultBaseURL   = "https://api.notion.com/v1"
	defaultUserAgent = "foglight/v0.1.0"
	defaultTimeout   = 30 * time.Second
	maxErrSnippet    = 512

	// notionVersion pins the API version this client targets. Notion uses
	// date-string versioning; 2026-03-11 is the latest published version
	// per https://developers.notion.com/reference/versioning.
	notionVersion = "2026-03-11"
)

// Client is a thin wrapper around Notion's REST API. Auth is bearer with
// either an internal-integration secret or an OAuth access token.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	userAgent  string
}

func newClient(apiKey string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
		baseURL:    defaultBaseURL,
		apiKey:     apiKey,
		userAgent:  defaultUserAgent,
	}
}

// notionError is the canonical error envelope Notion returns on non-2xx
// responses. We surface code+message for clearer agent-facing failures.
type notionError struct {
	Object  string `json:"object"`
	Status  int    `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// do executes a request and decodes the response body into out (if non-nil).
// body, when non-nil, is JSON-marshaled into the request body.
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("notion: marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("notion: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Notion-Version", notionVersion)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("notion: http: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("notion: read response: %w", err)
	}

	if resp.StatusCode/100 != 2 {
		// Try to surface Notion's structured error first; fall back to a
		// raw-body snippet if the body isn't the expected shape.
		var ne notionError
		if json.Unmarshal(respBody, &ne) == nil && ne.Object == "error" && ne.Message != "" {
			return fmt.Errorf("notion: http %d: %s: %s", resp.StatusCode, ne.Code, ne.Message)
		}
		snippet, _ := truncateString(strings.TrimSpace(string(respBody)), maxErrSnippet)
		return fmt.Errorf("notion: http %d: %s", resp.StatusCode, snippet)
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		snippet, _ := truncateString(strings.TrimSpace(string(respBody)), maxErrSnippet)
		return fmt.Errorf("notion: decode response: %w (body: %s)", err, snippet)
	}
	return nil
}

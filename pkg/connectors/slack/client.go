package slack

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"

	slacksdk "github.com/slack-go/slack"
)

const (
	defaultAPIURL    = "https://slack.com/api/"
	defaultTimeout   = 30 * time.Second
	slackCookieDomain = ".slack.com"
)

// newClient builds a slack-go client wired for browser-session auth
// (xoxc token + xoxd cookie). The token is sent as a Bearer header by
// slack-go; the cookie has to ride along on every request, which is why
// we pre-populate a cookie jar and inject it via OptionHTTPClient.
//
// The xoxd cookie value Slack sets is already URL-encoded (contains
// %2B / %2F for the base64 + and / chars in the underlying token). We
// pass it through unchanged so the wire bytes match what the browser
// actually sends. If a caller pastes the decoded form, Slack will reject
// the request with invalid_auth — that's the right failure mode.
func newClient(xoxc, xoxd string) *slacksdk.Client {
	jar, _ := cookiejar.New(nil)
	slackURL, _ := url.Parse("https://slack.com")
	jar.SetCookies(slackURL, []*http.Cookie{{
		Name:   "d",
		Value:  xoxd,
		Domain: slackCookieDomain,
		Path:   "/",
		Secure: true,
	}})

	httpClient := &http.Client{
		Timeout: defaultTimeout,
		Jar:     jar,
	}

	return slacksdk.New(xoxc,
		slacksdk.OptionHTTPClient(httpClient),
		slacksdk.OptionAPIURL(defaultAPIURL),
	)
}

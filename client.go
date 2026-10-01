// Package maukirim is the official Go SDK for the MauKirim WhatsApp gateway.
//
// MauKirim (https://maukirim.com) sends WhatsApp OTP codes and templated
// notifications, and rents managed WhatsApp numbers you drive over REST.
// Every call is authenticated with an API key created in the dashboard at
// https://app.maukirim.com:
//
//	client := maukirim.NewClient(os.Getenv("MAU_KIRIM_API_KEY"))
//	res, err := client.OTP.Send(ctx, maukirim.SendOTPParams{
//		Phone:          "+6281234567890",
//		Purpose:        maukirim.PurposeLogin,
//		IdempotencyKey: "otp-" + userID,
//	})
//
// All methods take a context.Context as their first argument and return the
// API's own error codes through *Error, which reports whether a failure is
// safe to retry.
package maukirim

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the production API base URL.
	DefaultBaseURL = "https://app.maukirim.com/api/v1"

	defaultUserAgent  = "maukirim-go/1.0.0"
	defaultMaxRetries = 2
	// maxResponseBytes bounds how much of a response body is buffered.
	maxResponseBytes = 8 << 20
)

// retryBackoff is the delay before each successive retry. It is a package
// level variable so tests (and embedders) can shorten it.
var retryBackoff = []time.Duration{100 * time.Millisecond, 300 * time.Millisecond}

// Client talks to the MauKirim API. Create one with NewClient; the zero value
// is not useful beyond DefaultBaseURL resolution.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	maxRetries int
	userAgent  string
	sleepFn    func(context.Context, time.Duration) error

	// OTP sends and verifies one-time passwords.
	OTP *OTPService
	// Notifications sends templated notifications from a shared number.
	Notifications *NotificationsService
	// Devices operates rented WhatsApp numbers.
	Devices *DevicesService
	// Webhooks manages per-device event destinations and delivery logs.
	Webhooks *WebhooksService
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the API base URL. A trailing slash is ignored.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	}
}

// WithHTTPClient overrides the HTTP client used for every request.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithMaxRetries sets how many times a retryable failure is retried.
// Negative values are treated as zero.
func WithMaxRetries(n int) Option {
	return func(c *Client) {
		if n < 0 {
			n = 0
		}
		c.maxRetries = n
	}
}

// WithUserAgent overrides the User-Agent header.
func WithUserAgent(userAgent string) Option {
	return func(c *Client) {
		if userAgent != "" {
			c.userAgent = userAgent
		}
	}
}

// NewClient returns a Client authenticated with apiKey (an mk_live_… key).
func NewClient(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:     apiKey,
		baseURL:    DefaultBaseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		maxRetries: defaultMaxRetries,
		userAgent:  defaultUserAgent,
		sleepFn:    defaultSleep,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	c.OTP = &OTPService{client: c}
	c.Notifications = &NotificationsService{client: c}
	c.Devices = &DevicesService{client: c}
	c.Webhooks = &WebhooksService{client: c}
	return c
}

// resolveBaseURL returns the configured base URL, falling back to
// DefaultBaseURL when it is unset and trimming any trailing slash.
func (c *Client) resolveBaseURL() string {
	if c.baseURL == "" {
		return DefaultBaseURL
	}
	return strings.TrimRight(c.baseURL, "/")
}

func (c *Client) sleeper() func(context.Context, time.Duration) error {
	if c.sleepFn == nil {
		return defaultSleep
	}
	return c.sleepFn
}

func (c *Client) http() *http.Client {
	if c.httpClient == nil {
		return http.DefaultClient
	}
	return c.httpClient
}

// apiRequest is one API call. Body and contentType are empty for requests with
// no payload; idemKey is set only for the routes that accept or require an
// Idempotency-Key header.
type apiRequest struct {
	method      string
	path        string
	query       url.Values
	body        []byte
	contentType string
	idemKey     string
}

func (c *Client) buildURL(path string, query url.Values) string {
	u := c.resolveBaseURL() + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

// do performs req, retrying retryable failures up to maxRetries times with the
// same Idempotency-Key, and decodes a successful JSON response into out.
func (c *Client) do(ctx context.Context, req apiRequest, out any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	endpoint := c.buildURL(req.path, req.query)

	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			if err := c.sleeper()(ctx, backoffFor(attempt-1)); err != nil {
				return err
			}
		}

		var body io.Reader
		if req.body != nil {
			body = bytes.NewReader(req.body)
		}
		httpReq, err := http.NewRequestWithContext(ctx, req.method, endpoint, body)
		if err != nil {
			return &Error{Code: "invalid_request", Message: err.Error(), cause: err}
		}
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		httpReq.Header.Set("Accept", "application/json")
		httpReq.Header.Set("User-Agent", c.userAgent)
		if req.contentType != "" {
			httpReq.Header.Set("Content-Type", req.contentType)
		}
		if req.idemKey != "" {
			httpReq.Header.Set("Idempotency-Key", req.idemKey)
		}

		resp, err := c.http().Do(httpReq)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			failure := transportError(err)
			if !failure.Retryable() || attempt >= c.maxRetries {
				return failure
			}
			continue
		}

		data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		resp.Body.Close()
		if readErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			failure := &Error{Code: "network_error", Message: readErr.Error(), cause: readErr}
			if !failure.Retryable() || attempt >= c.maxRetries {
				return failure
			}
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if out == nil || len(data) == 0 {
				return nil
			}
			if err := json.Unmarshal(data, out); err != nil {
				return &Error{
					StatusCode: resp.StatusCode,
					Code:       "invalid_response",
					Message:    fmt.Sprintf("failed to decode response: %v", err),
					cause:      err,
				}
			}
			return nil
		}

		failure := decodeError(resp.StatusCode, data)
		if !failure.Retryable() || attempt >= c.maxRetries {
			return failure
		}
	}
}

// backoffFor returns the delay before retry number i (0-based).
func backoffFor(i int) time.Duration {
	if len(retryBackoff) == 0 {
		return 0
	}
	if i < 0 {
		i = 0
	}
	if i >= len(retryBackoff) {
		i = len(retryBackoff) - 1
	}
	return retryBackoff[i]
}

// defaultSleep waits for d or until ctx is done, whichever comes first.
func defaultSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

package maukirim

import (
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// testClient returns a Client wired to a local httptest server with a no-op
// sleeper so retry tests never actually wait.
func testClient(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := NewClient("mk_live_test", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	c.sleepFn = func(context.Context, time.Duration) error { return nil }
	return c, srv
}

// requestRecorder captures what a handler observed for later assertions.
type requestRecorder struct {
	mu       sync.Mutex
	method   string
	path     string
	rawQuery string
	query    url.Values
	header   http.Header
	body     []byte
	calls    int
	form     *multipart.Form
}

func newRecorder() *requestRecorder { return &requestRecorder{} }

func (rr *requestRecorder) handler(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rr.mu.Lock()
		rr.calls++
		rr.method = r.Method
		rr.path = r.URL.Path
		rr.rawQuery = r.URL.RawQuery
		rr.query = r.URL.Query()
		rr.header = r.Header.Clone()
		rr.body = b
		rr.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if status != 0 {
			w.WriteHeader(status)
		}
		io.WriteString(w, body)
	}
}

func (rr *requestRecorder) snapshot() requestRecorder {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	return requestRecorder{
		method: rr.method, path: rr.path, rawQuery: rr.rawQuery,
		query: rr.query, header: rr.header, body: rr.body, calls: rr.calls, form: rr.form,
	}
}

func TestNewClientDefaults(t *testing.T) {
	c := NewClient("mk_live_abc")
	if got, want := c.baseURL, DefaultBaseURL; got != want {
		t.Fatalf("baseURL = %q, want %q", got, want)
	}
	if DefaultBaseURL != "https://app.maukirim.com/api/v1" {
		t.Fatalf("DefaultBaseURL = %q", DefaultBaseURL)
	}
	if c.maxRetries != 2 {
		t.Fatalf("maxRetries = %d, want 2", c.maxRetries)
	}
	if c.apiKey != "mk_live_abc" {
		t.Fatalf("apiKey = %q", c.apiKey)
	}
	if c.userAgent == "" {
		t.Fatal("userAgent must have a default")
	}
	if c.httpClient == nil {
		t.Fatal("httpClient must have a default")
	}
	if c.OTP == nil || c.Notifications == nil || c.Devices == nil || c.Webhooks == nil {
		t.Fatal("namespaces must be initialised")
	}
}

func TestOptionsOverrideDefaults(t *testing.T) {
	hc := &http.Client{Timeout: time.Second}
	c := NewClient("k",
		WithBaseURL("https://example.test/api/v9/"),
		WithHTTPClient(hc),
		WithMaxRetries(9),
		WithUserAgent("custom-agent/2"),
	)
	if c.baseURL != "https://example.test/api/v9" {
		t.Fatalf("baseURL = %q (trailing slash must be trimmed)", c.baseURL)
	}
	if c.httpClient != hc {
		t.Fatal("WithHTTPClient not applied")
	}
	if c.maxRetries != 9 {
		t.Fatalf("maxRetries = %d", c.maxRetries)
	}
	if c.userAgent != "custom-agent/2" {
		t.Fatalf("userAgent = %q", c.userAgent)
	}
	if got := NewClient("k", WithMaxRetries(-3)).maxRetries; got != 0 {
		t.Fatalf("negative maxRetries = %d, want 0", got)
	}
}

func TestDoSetsAuthAndUserAgent(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true}`))
	c.userAgent = defaultUserAgent
	var out map[string]any
	if err := c.do(context.Background(), apiRequest{method: http.MethodGet, path: "/devices"}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := rec.snapshot()
	if got.method != http.MethodGet || got.path != "/devices" {
		t.Fatalf("got %s %s", got.method, got.path)
	}
	if auth := got.header.Get("Authorization"); auth != "Bearer mk_live_test" {
		t.Fatalf("Authorization = %q", auth)
	}
	if got.header.Get("User-Agent") == "" {
		t.Fatal("User-Agent missing")
	}
	if ct := got.header.Get("Content-Type"); ct != "" {
		t.Fatalf("Content-Type = %q on a bodyless GET, want empty", ct)
	}
}

func TestDoDecodesErrorEnvelopeIntoError(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusForbidden, `{"ok":false,"code":"api_key_scope_denied"}`))
	err := c.do(context.Background(), apiRequest{method: http.MethodGet, path: "/devices"}, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("errors.As(*Error) failed for %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusForbidden {
		t.Fatalf("StatusCode = %d", apiErr.StatusCode)
	}
	if apiErr.Code != "api_key_scope_denied" {
		t.Fatalf("Code = %q", apiErr.Code)
	}
	if apiErr.Retryable() {
		t.Fatal("403 must not be retryable")
	}
	if !strings.Contains(apiErr.Error(), "api_key_scope_denied") {
		t.Fatalf("Error() = %q", apiErr.Error())
	}
	if got := rec.snapshot(); got.calls != 1 {
		t.Fatalf("calls = %d, want 1 (4xx must not retry)", got.calls)
	}
}

func TestErrorRetryableMatrix(t *testing.T) {
	cases := []struct {
		status int
		code   string
		want   bool
	}{
		{500, "server_error", true},
		{502, "", true},
		{503, "worker_offline", true},
		{400, "invalid_input", false},
		{401, "api_key_invalid", false},
		{401, "api_key_revoked", false},
		{402, "insufficient_credits", false},
		{403, "forbidden", false},
		{404, "rental_not_found", false},
		{409, "device_not_connected", false},
		{410, "media_unavailable", false},
		{413, "batch_too_large", false},
		{429, "rate_limited", false},
	}
	for _, tc := range cases {
		e := &Error{StatusCode: tc.status, Code: tc.code}
		if got := e.Retryable(); got != tc.want {
			t.Errorf("Error{%d,%s}.Retryable() = %v, want %v", tc.status, tc.code, got, tc.want)
		}
		if tc.want && e.Error() == "" {
			t.Errorf("Error{%d}.Error() empty", tc.status)
		}
	}
	var nilErr *Error
	if nilErr.Retryable() {
		t.Fatal("nil *Error must not be retryable")
	}
}

type failingTransport struct{ err error }

func (f failingTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, f.err }

func TestTransportErrorsAreRetryableAndWrapCause(t *testing.T) {
	underlying := errors.New("dial tcp 127.0.0.1:443: connect: connection refused")
	c := NewClient("k", WithHTTPClient(&http.Client{Transport: failingTransport{err: underlying}}))
	c.sleepFn = func(context.Context, time.Duration) error { return nil }
	err := c.do(context.Background(), apiRequest{method: http.MethodGet, path: "/devices"}, nil)
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if apiErr.StatusCode != 0 {
		t.Fatalf("StatusCode = %d, want 0 for a transport error", apiErr.StatusCode)
	}
	if !apiErr.Retryable() {
		t.Fatal("transport error must be retryable")
	}
	if !errors.Is(err, underlying) {
		t.Fatal("transport error must wrap the underlying cause")
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestTimeoutTransportErrorIsClassifiedAsTimeout(t *testing.T) {
	c := NewClient("k", WithHTTPClient(&http.Client{Transport: failingTransport{err: timeoutError{}}}))
	c.sleepFn = func(context.Context, time.Duration) error { return nil }
	err := c.do(context.Background(), apiRequest{method: http.MethodGet, path: "/devices"}, nil)
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if apiErr.Code != "timeout" {
		t.Fatalf("Code = %q, want timeout", apiErr.Code)
	}
	if !apiErr.Retryable() {
		t.Fatal("timeout must be retryable")
	}
}

func TestRetryOn503ReusesIdempotencyKeyAndSucceeds(t *testing.T) {
	var mu sync.Mutex
	var keys []string
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		n := len(keys)
		mu.Unlock()
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, `{"ok":false,"code":"worker_offline"}`)
			return
		}
		io.WriteString(w, `{"ok":true,"batchId":"b1"}`)
	})
	var out struct {
		BatchID string `json:"batchId"`
	}
	err := c.do(context.Background(), apiRequest{
		method: http.MethodPost, path: "/messages/send",
		body:    []byte(`{"phone":"+6281234567890"}`),
		idemKey: "idem-key-1", contentType: "application/json",
	}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.BatchID != "b1" {
		t.Fatalf("BatchID = %q", out.BatchID)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(keys) != 3 {
		t.Fatalf("attempts = %d, want 3 (1 + 2 retries)", len(keys))
	}
	for i, k := range keys {
		if k != "idem-key-1" {
			t.Fatalf("attempt %d used Idempotency-Key %q, want idem-key-1", i, k)
		}
	}
}

func TestRetriesGiveUpAfterMaxRetries(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusServiceUnavailable, `{"ok":false,"code":"worker_offline"}`))
	err := c.do(context.Background(), apiRequest{method: http.MethodGet, path: "/devices"}, nil)
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Code != "worker_offline" {
		t.Fatalf("err = %v", err)
	}
	if got := rec.snapshot().calls; got != 3 {
		t.Fatalf("calls = %d, want 3", got)
	}
}

func TestWithMaxRetriesControlsAttemptCount(t *testing.T) {
	for _, tc := range []struct {
		maxRetries int
		wantCalls  int
	}{
		{0, 1},
		{1, 2},
		{5, 6},
	} {
		rec := newRecorder()
		srv := httptest.NewServer(rec.handler(http.StatusServiceUnavailable, `{"ok":false,"code":"worker_offline"}`))
		c := NewClient("k", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()), WithMaxRetries(tc.maxRetries))
		c.sleepFn = func(context.Context, time.Duration) error { return nil }
		err := c.do(context.Background(), apiRequest{method: http.MethodGet, path: "/devices"}, nil)
		if err == nil {
			t.Fatalf("maxRetries=%d: expected error", tc.maxRetries)
		}
		if got := rec.snapshot().calls; got != tc.wantCalls {
			t.Fatalf("maxRetries=%d: calls = %d, want %d", tc.maxRetries, got, tc.wantCalls)
		}
		srv.Close()
	}
}

func TestRetriesDoNotHappenForClientErrors(t *testing.T) {
	for _, status := range []int{400, 401, 402, 403, 404, 409, 410, 413, 429} {
		rec := newRecorder()
		srv := httptest.NewServer(rec.handler(status, `{"ok":false,"code":"invalid_input"}`))
		c := NewClient("k", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
		c.sleepFn = func(context.Context, time.Duration) error { return nil }
		if err := c.do(context.Background(), apiRequest{method: http.MethodGet, path: "/devices"}, nil); err == nil {
			t.Fatalf("status %d: expected error", status)
		}
		if got := rec.snapshot().calls; got != 1 {
			t.Fatalf("status %d: calls = %d, want 1", status, got)
		}
		srv.Close()
	}
}

func TestRetryBackoffScheduleIsHonoured(t *testing.T) {
	var mu sync.Mutex
	var slept []time.Duration
	rec := newRecorder()
	srv := httptest.NewServer(rec.handler(http.StatusServiceUnavailable, `{"ok":false,"code":"worker_offline"}`))
	c := NewClient("k", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	c.sleepFn = func(_ context.Context, d time.Duration) error {
		mu.Lock()
		slept = append(slept, d)
		mu.Unlock()
		return nil
	}
	_ = c.do(context.Background(), apiRequest{method: http.MethodGet, path: "/devices"}, nil)
	mu.Lock()
	defer mu.Unlock()
	if len(slept) != 2 {
		t.Fatalf("slept %d times, want 2: %v", len(slept), slept)
	}
	if slept[0] != 100*time.Millisecond || slept[1] != 300*time.Millisecond {
		t.Fatalf("backoff = %v, want [100ms 300ms]", slept)
	}
	srv.Close()
}

func TestContextCancellationStopsRetries(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusServiceUnavailable, `{"ok":false,"code":"worker_offline"}`))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var n int
	c.sleepFn = func(context.Context, time.Duration) error {
		n++
		if n == 1 {
			cancel()
		}
		return nil
	}
	err := c.do(ctx, apiRequest{method: http.MethodGet, path: "/devices"}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got := rec.snapshot().calls; got > 2 {
		t.Fatalf("calls = %d; must stop once ctx is cancelled", got)
	}
}

func TestDefaultSleepHonoursContext(t *testing.T) {
	if err := defaultSleep(context.Background(), 0); err != nil {
		t.Fatalf("zero duration must not sleep: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := defaultSleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestDoJSONDecodeFailureIsReported(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,"devices":`))
	var out struct {
		Devices []string `json:"devices"`
	}
	err := c.do(context.Background(), apiRequest{method: http.MethodGet, path: "/devices"}, &out)
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("err = %v, want a decode error", err)
	}
}

func TestBaseURLZeroValueFallsBackToDefault(t *testing.T) {
	c := NewClient("k")
	if c.resolveBaseURL() != DefaultBaseURL {
		t.Fatalf("resolveBaseURL() = %q", c.resolveBaseURL())
	}
	c.baseURL = "https://x.test"
	if c.resolveBaseURL() != "https://x.test" {
		t.Fatalf("resolveBaseURL() = %q", c.resolveBaseURL())
	}
}

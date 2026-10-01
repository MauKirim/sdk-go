package maukirim

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
)

// maxDeliveryLimit is the largest page size the API accepts.
const maxDeliveryLimit = 100

// WebhooksService manages per-device webhook destinations and delivery logs.
type WebhooksService struct {
	client *Client
}

// WebhookSettings is where a device's events are delivered. The signing
// secret is never returned inside it: it comes back once, in
// [WebhookInfo.Secret], on creation or rotation.
type WebhookSettings struct {
	ID       string   `json:"id"`
	DeviceID string   `json:"deviceId"`
	URL      string   `json:"url"`
	Events   []string `json:"events"`
	Active   bool     `json:"active"`
}

// WebhookInfo is the result of Get, Set and Rotate. Secret is set only when
// the API returns one (creation and rotation); an edit leaves it empty.
type WebhookInfo struct {
	Webhook         *WebhookSettings `json:"webhook"`
	AvailableEvents []string         `json:"availableEvents,omitempty"`
	GatewayWired    bool             `json:"gatewayWired,omitempty"`
	Secret          string           `json:"secret,omitempty"`
}

// SetWebhookParams are the inputs of [WebhooksService.Set].
type SetWebhookParams struct {
	// URL must be https, on a public host, without credentials, query,
	// fragment, IP literal or localhost.
	URL string
	// Events selects the delivered event names. Empty means every event.
	Events []string
	// Active defaults to true server-side; set it explicitly (including
	// false) with a pointer to override.
	Active *bool
}

// Get returns the device's webhook destination, the events it may subscribe to
// and whether the gateway is wired.
func (s *WebhooksService) Get(ctx context.Context, deviceID string) (*WebhookInfo, error) {
	var info WebhookInfo
	err := s.client.do(ctx, apiRequest{method: "GET", path: devicePath(deviceID, "/webhook")}, &info)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// Set creates or edits the device's webhook destination. Creating returns the
// signing secret once; editing keeps the existing secret and returns none.
func (s *WebhooksService) Set(ctx context.Context, deviceID string, params SetWebhookParams) (*WebhookInfo, error) {
	body, err := json.Marshal(struct {
		URL    string   `json:"url"`
		Events []string `json:"events,omitempty"`
		Active *bool    `json:"active,omitempty"`
	}{URL: params.URL, Events: params.Events, Active: params.Active})
	if err != nil {
		return nil, err
	}
	var info WebhookInfo
	err = s.client.do(ctx, apiRequest{
		method:      "POST",
		path:        devicePath(deviceID, "/webhook"),
		body:        body,
		contentType: "application/json",
	}, &info)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// Rotate issues a new signing secret. The old one stops verifying immediately.
func (s *WebhooksService) Rotate(ctx context.Context, deviceID string) (*WebhookInfo, error) {
	var info WebhookInfo
	err := s.client.do(ctx, apiRequest{method: "PUT", path: devicePath(deviceID, "/webhook")}, &info)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// RemoveWebhookResult acknowledges the removal of a destination and its
// delivery log.
type RemoveWebhookResult struct {
	Removed bool `json:"removed"`
}

// Remove deletes the device's webhook destination and its delivery log.
func (s *WebhooksService) Remove(ctx context.Context, deviceID string) (*RemoveWebhookResult, error) {
	var result RemoveWebhookResult
	err := s.client.do(ctx, apiRequest{method: "DELETE", path: devicePath(deviceID, "/webhook")}, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// DeliveryQuery filters [WebhooksService.Deliveries]. Zero values keep the
// API defaults (limit 25, offset 0); Limit is clamped to 100 and negative
// values fall back to the defaults.
type DeliveryQuery struct {
	// DeviceID restricts the page to one device; empty means all devices.
	DeviceID string
	// Limit is the page size, 1–100. Zero or negative means the default 25.
	Limit int
	// Offset skips that many newest-first records. Negative means 0.
	Offset int
}

// Delivery is one webhook delivery attempt record.
type Delivery struct {
	ID             string          `json:"id"`
	DeviceID       string          `json:"deviceId"`
	Event          string          `json:"event"`
	State          string          `json:"state"`
	Attempts       int             `json:"attempts"`
	ResponseStatus *int            `json:"responseStatus"`
	ErrorCode      *string         `json:"errorCode"`
	Payload        json.RawMessage `json:"payload"`
	CreatedAt      string          `json:"createdAt"`
	CompletedAt    *string         `json:"completedAt"`
}

// DeliveryPage is one page of webhook deliveries, newest first.
type DeliveryPage struct {
	Deliveries []Delivery `json:"deliveries"`
	HasMore    bool       `json:"hasMore"`
}

// Deliveries returns the device's (or account's) webhook delivery log.
func (s *WebhooksService) Deliveries(ctx context.Context, query DeliveryQuery) (*DeliveryPage, error) {
	values := url.Values{}
	if strings.TrimSpace(query.DeviceID) != "" {
		values.Set("deviceId", query.DeviceID)
	}
	if query.Limit > 0 {
		limit := query.Limit
		if limit > maxDeliveryLimit {
			limit = maxDeliveryLimit
		}
		values.Set("limit", strconv.Itoa(limit))
	}
	if query.Offset > 0 {
		values.Set("offset", strconv.Itoa(query.Offset))
	}
	var page DeliveryPage
	err := s.client.do(ctx, apiRequest{
		method: "GET",
		path:   "/webhook-deliveries",
		query:  values,
	}, &page)
	if err != nil {
		return nil, err
	}
	return &page, nil
}

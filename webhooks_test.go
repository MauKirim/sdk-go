package maukirim

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func TestWebhooksGetRequestShape(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,"webhook":{"id":"wh_1","deviceId":"dev_1","url":"https://example.com/hook","events":["message","message.ack"],"active":true},"availableEvents":["message","message.ack","call.offer"],"gatewayWired":true}`))

	info, err := c.Webhooks.Get(context.Background(), "dev_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	req := rec.snapshot()
	if req.method != http.MethodGet || req.path != "/devices/dev_1/webhook" {
		t.Fatalf("got %s %s", req.method, req.path)
	}
	if info.Webhook == nil {
		t.Fatal("Webhook = nil")
	}
	if info.Webhook.ID != "wh_1" || info.Webhook.DeviceID != "dev_1" ||
		info.Webhook.URL != "https://example.com/hook" || !info.Webhook.Active {
		t.Fatalf("webhook = %+v", info.Webhook)
	}
	if len(info.Webhook.Events) != 2 || info.Webhook.Events[0] != "message" {
		t.Fatalf("events = %v", info.Webhook.Events)
	}
	if len(info.AvailableEvents) != 3 || info.AvailableEvents[2] != "call.offer" {
		t.Fatalf("availableEvents = %v", info.AvailableEvents)
	}
	if !info.GatewayWired {
		t.Fatal("GatewayWired = false, want true")
	}
	if info.Secret != "" {
		t.Fatalf("Secret = %q, want empty (the API never returns it here)", info.Secret)
	}
}

func TestWebhooksGetWithoutDestination(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,"webhook":null,"availableEvents":["message"],"gatewayWired":false}`))
	info, err := c.Webhooks.Get(context.Background(), "dev_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Webhook != nil {
		t.Fatalf("Webhook = %+v, want nil", info.Webhook)
	}
	if info.GatewayWired || len(info.AvailableEvents) != 1 {
		t.Fatalf("info = %+v", info)
	}
}

func TestWebhooksSetCreatesAndReturnsSecret(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,"webhook":{"id":"wh_2","deviceId":"dev_1","url":"https://example.com/hook","events":["message"],"active":true},"secret":"deadbeef"}`))

	active := true
	info, err := c.Webhooks.Set(context.Background(), "dev_1", SetWebhookParams{
		URL:    "https://example.com/hook",
		Events: []string{"message"},
		Active: &active,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	req := rec.snapshot()
	if req.method != http.MethodPost || req.path != "/devices/dev_1/webhook" {
		t.Fatalf("got %s %s", req.method, req.path)
	}
	if ct := req.header.Get("Content-Type"); ct == "" {
		t.Fatal("Content-Type missing")
	}
	var body map[string]any
	if err := json.Unmarshal(req.body, &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 3 || body["url"] != "https://example.com/hook" || body["active"] != true {
		t.Fatalf("body = %s", req.body)
	}
	if info.Secret != "deadbeef" {
		t.Fatalf("Secret = %q", info.Secret)
	}
	if info.Webhook == nil || info.Webhook.ID != "wh_2" {
		t.Fatalf("webhook = %+v", info.Webhook)
	}
}

func TestWebhooksSetOmitsUnsetFields(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,"webhook":{"id":"wh_3","deviceId":"dev_1","url":"https://example.com/hook","events":[],"active":true}}`))
	if _, err := c.Webhooks.Set(context.Background(), "dev_1", SetWebhookParams{URL: "https://example.com/hook"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.snapshot().body, &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 || body["url"] != "https://example.com/hook" {
		t.Fatalf("body = %s, want only the url", rec.snapshot().body)
	}
}

func TestWebhooksSetSendsExplicitFalseActive(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,"webhook":{"id":"wh_4","deviceId":"dev_1","url":"https://example.com/hook","events":[],"active":false}}`))
	active := false
	if _, err := c.Webhooks.Set(context.Background(), "dev_1", SetWebhookParams{URL: "https://example.com/hook", Active: &active}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.snapshot().body, &body); err != nil {
		t.Fatal(err)
	}
	if v, ok := body["active"]; !ok || v != false {
		t.Fatalf("body = %s, want active:false present", rec.snapshot().body)
	}
}

func TestWebhooksSetEditHasNoSecret(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,"webhook":{"id":"wh_5","deviceId":"dev_1","url":"https://example.com/hook","events":[],"active":true}}`))
	info, err := c.Webhooks.Set(context.Background(), "dev_1", SetWebhookParams{URL: "https://example.com/hook"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Secret != "" {
		t.Fatalf("Secret = %q, want empty on edit", info.Secret)
	}
}

func TestWebhooksSetDecodesValidationError(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusBadRequest, `{"ok":false,"code":"invalid_input"}`))
	_, err := c.Webhooks.Set(context.Background(), "dev_1", SetWebhookParams{URL: "http://localhost/hook"})
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 400 || apiErr.Code != "invalid_input" {
		t.Fatalf("err = %v", err)
	}
}

func TestWebhooksRotateRequestShape(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,"webhook":{"id":"wh_6","deviceId":"dev_1","url":"https://example.com/hook","events":[],"active":true},"secret":"cafebabe"}`))
	info, err := c.Webhooks.Rotate(context.Background(), "dev_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	req := rec.snapshot()
	if req.method != http.MethodPut || req.path != "/devices/dev_1/webhook" {
		t.Fatalf("got %s %s", req.method, req.path)
	}
	if len(req.body) != 0 {
		t.Fatalf("body = %s, want empty", req.body)
	}
	if info.Secret != "cafebabe" {
		t.Fatalf("Secret = %q", info.Secret)
	}
}

func TestWebhooksRotateDecodesNotConfigured(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusNotFound, `{"ok":false,"code":"webhook_not_configured"}`))
	_, err := c.Webhooks.Rotate(context.Background(), "dev_1")
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 404 || apiErr.Code != "webhook_not_configured" {
		t.Fatalf("err = %v", err)
	}
}

func TestWebhooksRemoveRequestShape(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,"removed":true}`))
	got, err := c.Webhooks.Remove(context.Background(), "dev_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	req := rec.snapshot()
	if req.method != http.MethodDelete || req.path != "/devices/dev_1/webhook" {
		t.Fatalf("got %s %s", req.method, req.path)
	}
	if !got.Removed {
		t.Fatal("Removed = false, want true")
	}
}

func TestWebhooksRemoveDecodesNotConfigured(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusNotFound, `{"ok":false,"code":"webhook_not_configured"}`))
	_, err := c.Webhooks.Remove(context.Background(), "dev_1")
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Code != "webhook_not_configured" {
		t.Fatalf("err = %v", err)
	}
}

func TestWebhooksDeliveriesQueryEncoding(t *testing.T) {
	cases := []struct {
		name  string
		query DeliveryQuery
		want  string
	}{
		{"all defaults", DeliveryQuery{}, ""},
		{"device only", DeliveryQuery{DeviceID: "dev_1"}, "deviceId=dev_1"},
		{"explicit values", DeliveryQuery{DeviceID: "dev_1", Limit: 50, Offset: 10}, "deviceId=dev_1&limit=50&offset=10"},
		{"limit clamped high", DeliveryQuery{Limit: 1000}, "limit=100"},
		{"negative limit means default", DeliveryQuery{Limit: -4}, ""},
		{"offset clamped to zero", DeliveryQuery{Offset: -3}, ""},
		{"limit zero means default", DeliveryQuery{Limit: 0}, ""},
		{"limit one", DeliveryQuery{Limit: 1}, "limit=1"},
		{"offset without device", DeliveryQuery{Offset: 25}, "offset=25"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := newRecorder()
			c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,"deliveries":[],"hasMore":false}`))
			if _, err := c.Webhooks.Deliveries(context.Background(), tc.query); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := rec.snapshot()
			if got.method != http.MethodGet || got.path != "/webhook-deliveries" {
				t.Fatalf("got %s %s", got.method, got.path)
			}
			if got.rawQuery != tc.want {
				t.Fatalf("query = %q, want %q", got.rawQuery, tc.want)
			}
		})
	}
}

func TestWebhooksDeliveriesDecode(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,"hasMore":true,"deliveries":[
{"id":"dl_1","deviceId":"dev_1","event":"message","state":"delivered","attempts":1,"responseStatus":200,"errorCode":null,"payload":{"from":"+6281234567890"},"createdAt":"2026-10-02T00:00:00.000Z","completedAt":"2026-10-02T00:00:01.000Z"},
{"id":"dl_2","deviceId":"dev_1","event":"message.ack","state":"exhausted","attempts":5,"responseStatus":null,"errorCode":"endpoint_unreachable","payload":null,"createdAt":"2026-10-02T00:00:00.000Z","completedAt":null}
]}`))

	page, err := c.Webhooks.Deliveries(context.Background(), DeliveryQuery{DeviceID: "dev_1", Limit: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !page.HasMore || len(page.Deliveries) != 2 {
		t.Fatalf("page = %+v", page)
	}
	first := page.Deliveries[0]
	if first.ID != "dl_1" || first.DeviceID != "dev_1" || first.Event != "message" || first.State != "delivered" ||
		first.Attempts != 1 || first.CreatedAt != "2026-10-02T00:00:00.000Z" {
		t.Fatalf("first delivery = %+v", first)
	}
	if first.ResponseStatus == nil || *first.ResponseStatus != 200 {
		t.Fatalf("ResponseStatus = %v", first.ResponseStatus)
	}
	if first.ErrorCode != nil || first.CompletedAt == nil {
		t.Fatalf("first nullable fields = %+v", first)
	}
	var payload map[string]any
	if err := json.Unmarshal(first.Payload, &payload); err != nil || payload["from"] != "+6281234567890" {
		t.Fatalf("Payload = %s (%v)", first.Payload, err)
	}

	second := page.Deliveries[1]
	if second.ResponseStatus != nil || second.CompletedAt != nil {
		t.Fatalf("second nullable fields = %+v", second)
	}
	if second.ErrorCode == nil || *second.ErrorCode != "endpoint_unreachable" {
		t.Fatalf("ErrorCode = %v", second.ErrorCode)
	}
	if string(second.Payload) != "null" {
		t.Fatalf("Payload = %s, want the raw null literal", second.Payload)
	}
}

func TestWebhooksDeliveriesDecodesScopeError(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusForbidden, `{"ok":false,"code":"api_key_scope_denied"}`))
	_, err := c.Webhooks.Deliveries(context.Background(), DeliveryQuery{})
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 403 || apiErr.Code != "api_key_scope_denied" {
		t.Fatalf("err = %v", err)
	}
}

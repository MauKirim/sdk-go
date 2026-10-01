package maukirim

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strings"
	"testing"
)

func TestNotificationsSendRequestShape(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,"batchId":"b_9"}`))

	variables := map[string]any{
		"name":   "Ayu",
		"amount": "Rp 25.000",
		"items": []map[string]string{
			{"sku": "A-1", "qty": "2"},
			{"sku": "B-7", "qty": "1"},
		},
	}
	got, err := c.Notifications.Send(context.Background(), SendNotificationParams{
		Phone:          "+6281234567890",
		TemplateID:     "tpl_order_paid",
		Variables:      variables,
		IdempotencyKey: "order-1001",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.BatchID != "b_9" {
		t.Fatalf("BatchID = %q", got.BatchID)
	}

	req := rec.snapshot()
	if req.method != http.MethodPost || req.path != "/messages/send" {
		t.Fatalf("got %s %s", req.method, req.path)
	}
	if req.header.Get("Idempotency-Key") != "order-1001" {
		t.Fatalf("Idempotency-Key = %q", req.header.Get("Idempotency-Key"))
	}
	if ct := req.header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q", ct)
	}

	var body struct {
		Phone              string         `json:"phone"`
		TemplateID         string         `json:"templateId"`
		Variables          map[string]any `json:"variables"`
		AttachmentUploadID *string        `json:"attachmentUploadId"`
	}
	if err := json.Unmarshal(req.body, &body); err != nil {
		t.Fatalf("body = %s", req.body)
	}
	if body.Phone != "+6281234567890" || body.TemplateID != "tpl_order_paid" {
		t.Fatalf("body = %s", req.body)
	}
	if body.AttachmentUploadID != nil {
		t.Fatalf("attachmentUploadId must be omitted when empty: %s", req.body)
	}
	wantVariables, err := json.Marshal(variables)
	if err != nil {
		t.Fatal(err)
	}
	gotVariables, err := json.Marshal(body.Variables)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotVariables) != string(wantVariables) {
		t.Fatalf("variables round-trip mismatch:\n got %s\nwant %s", gotVariables, wantVariables)
	}
	var keys map[string]any
	if err := json.Unmarshal(req.body, &keys); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 3 {
		t.Fatalf("body has %d top-level fields, want 3: %s", len(keys), req.body)
	}
}

func TestNotificationsSendIncludesAttachmentUploadID(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,"batchId":"b_1"}`))
	_, err := c.Notifications.Send(context.Background(), SendNotificationParams{
		Phone:              "+6281234567890",
		TemplateID:         "tpl_invoice",
		Variables:          map[string]any{"name": "Ayu"},
		AttachmentUploadID: "media_123",
		IdempotencyKey:     "inv-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.snapshot().body, &body); err != nil {
		t.Fatal(err)
	}
	if body["attachmentUploadId"] != "media_123" {
		t.Fatalf("body = %s", rec.snapshot().body)
	}
}

func TestNotificationsSendRequiresIdempotencyKey(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true}`))
	_, err := c.Notifications.Send(context.Background(), SendNotificationParams{
		Phone: "+6281234567890", TemplateID: "tpl_x",
	})
	if !errors.Is(err, ErrMissingIdempotencyKey) {
		t.Fatalf("err = %v, want ErrMissingIdempotencyKey", err)
	}
	if got := rec.snapshot().calls; got != 0 {
		t.Fatalf("calls = %d, want 0", got)
	}
}

func TestNotificationsSendDecodesAPIError(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusPaymentRequired, `{"ok":false,"code":"insufficient_credits"}`))
	_, err := c.Notifications.Send(context.Background(), SendNotificationParams{
		Phone: "+6281234567890", TemplateID: "tpl_x", IdempotencyKey: "k",
	})
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Code != "insufficient_credits" || apiErr.StatusCode != 402 {
		t.Fatalf("err = %v", err)
	}
	if apiErr.Retryable() {
		t.Fatal("402 must not be retryable")
	}
}

func TestNotificationsUploadMediaMultipart(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK,
		`{"ok":true,"media":{"id":"media_7","contentType":"image/jpeg","byteSize":11,"fileName":null,"kind":"image","expiresAt":"2026-10-09T00:00:00.000Z"}}`))

	got, err := c.Notifications.UploadMedia(context.Background(), "receipt.jpg", "image/jpeg", strings.NewReader("hello world"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "media_7" || got.ContentType != "image/jpeg" || got.ByteSize != 11 || got.Kind != "image" {
		t.Fatalf("decoded media = %+v", got)
	}
	if got.FileName != nil {
		t.Fatalf("FileName = %q, want nil for a null fileName", *got.FileName)
	}
	if got.ExpiresAt != "2026-10-09T00:00:00.000Z" {
		t.Fatalf("ExpiresAt = %q", got.ExpiresAt)
	}

	if err := rec.parseMultipart(1 << 20); err != nil {
		t.Fatalf("request is not multipart: %v", err)
	}
	if mt, _, err := mime.ParseMediaType(rec.snapshot().header.Get("Content-Type")); err != nil || mt != "multipart/form-data" {
		t.Fatalf("Content-Type = %q", rec.snapshot().header.Get("Content-Type"))
	}
	if name := rec.fileName("file"); name != "receipt.jpg" {
		t.Fatalf("file part name = %q", name)
	}
	if ct := rec.fileContentType("file"); ct != "image/jpeg" {
		t.Fatalf("file part Content-Type = %q", ct)
	}
	if body := rec.fileBody("file"); body != "hello world" {
		t.Fatalf("file part body = %q", body)
	}
}

func TestNotificationsUploadMediaDecodesAPIError(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusGone, `{"ok":false,"code":"media_unavailable"}`))
	_, err := c.Notifications.UploadMedia(context.Background(), "x.pdf", "application/pdf", strings.NewReader("x"))
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusGone || apiErr.Code != "media_unavailable" {
		t.Fatalf("err = %v", err)
	}
}

func TestNotificationsUploadMediaReturnsFileNameWhenPresent(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK,
		`{"ok":true,"media":{"id":"m","contentType":"application/pdf","byteSize":1,"fileName":"doc.pdf","kind":"file","expiresAt":"t"}}`))
	got, err := c.Notifications.UploadMedia(context.Background(), "doc.pdf", "application/pdf", strings.NewReader("x"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.FileName == nil || *got.FileName != "doc.pdf" {
		t.Fatalf("FileName = %v", got.FileName)
	}
}

func TestNotificationsSendAlwaysIncludesVariablesObject(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,"batchId":"b_1"}`))
	if _, err := c.Notifications.Send(context.Background(), SendNotificationParams{
		Phone: "+6281234567890", TemplateID: "tpl_no_placeholders", IdempotencyKey: "k",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.snapshot().body, &body); err != nil {
		t.Fatal(err)
	}
	raw, ok := body["variables"]
	if !ok {
		t.Fatalf("variables must always be present, got body %s", rec.snapshot().body)
	}
	if m, ok := raw.(map[string]any); !ok || len(m) != 0 {
		t.Fatalf("variables = %#v, want an empty object (never null)", raw)
	}
}

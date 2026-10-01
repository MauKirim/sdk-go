package maukirim

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

const devicesListJSON = `{"ok":true,"devices":[
{"id":"dev_1","label":"Sales","number":"+628111111111","plan":"business","status":"active","term":"monthly","currency":"IDR","priceMinor":25000000,"autoRenew":true,"currentPeriodEnd":"2026-11-01T00:00:00.000Z","pushName":"Sales Bot","hasAvatar":true,"connection":{"state":"open","isConnected":true,"isLoggedIn":true,"missingSince":null}},
{"id":"dev_2","label":"Old","number":"+62*******111","plan":"starter","status":"cancelled","term":"monthly","currency":"IDR","priceMinor":10000000,"autoRenew":false,"currentPeriodEnd":"2026-09-01T00:00:00.000Z","pushName":null,"hasAvatar":false,"connection":{"state":null,"isConnected":null,"isLoggedIn":null,"missingSince":null}}
]}`

func TestDevicesListRequestShape(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, devicesListJSON))

	devices, err := c.Devices.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	req := rec.snapshot()
	if req.method != http.MethodGet || req.path != "/devices" {
		t.Fatalf("got %s %s", req.method, req.path)
	}
	if req.header.Get("Authorization") != "Bearer mk_live_test" {
		t.Fatalf("Authorization = %q", req.header.Get("Authorization"))
	}
	if len(devices) != 2 {
		t.Fatalf("len(devices) = %d", len(devices))
	}

	first := devices[0]
	if first.ID != "dev_1" || first.Label != "Sales" || first.Number != "+628111111111" ||
		first.Plan != "business" || first.Status != "active" || first.Term != "monthly" ||
		first.Currency != "IDR" || first.PriceMinor != 25000000 || !first.AutoRenew ||
		first.CurrentPeriodEnd != "2026-11-01T00:00:00.000Z" || !first.HasAvatar {
		t.Fatalf("first device = %+v", first)
	}
	if first.PushName == nil || *first.PushName != "Sales Bot" {
		t.Fatalf("PushName = %v", first.PushName)
	}
	if first.Connection == nil || first.Connection.State == nil || *first.Connection.State != "open" ||
		first.Connection.IsConnected == nil || !*first.Connection.IsConnected ||
		first.Connection.IsLoggedIn == nil || !*first.Connection.IsLoggedIn {
		t.Fatalf("Connection = %+v", first.Connection)
	}
	if first.Connection.MissingSince != nil {
		t.Fatalf("MissingSince = %v, want nil", first.Connection.MissingSince)
	}

	second := devices[1]
	if second.PushName != nil || second.HasAvatar {
		t.Fatalf("masked device leaked pushName/avatar: %+v", second)
	}
	if second.Connection == nil || second.Connection.State != nil || second.Connection.IsConnected != nil {
		t.Fatalf("masked device connection = %+v", second.Connection)
	}
}

func TestDevicesGetRequestShape(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,
"device":{"id":"dev_1","label":"Sales","number":"+628111111111","plan":"business","status":"active","term":"monthly","currency":"IDR","priceMinor":25000000,"autoRenew":true,"currentPeriodEnd":"2026-11-01T00:00:00.000Z","pushName":"Sales Bot","hasAvatar":true,"connection":{"state":"open","isConnected":true,"isLoggedIn":true,"missingSince":null}},
"rental":{"id":"ren_1","status":"active"},
"webhook":null}`))

	detail, err := c.Devices.Get(context.Background(), "dev_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	req := rec.snapshot()
	if req.method != http.MethodGet || req.path != "/devices/dev_1" {
		t.Fatalf("got %s %s", req.method, req.path)
	}
	if detail.Device.ID != "dev_1" || detail.Device.Number != "+628111111111" {
		t.Fatalf("device = %+v", detail.Device)
	}
	if detail.Rental["id"] != "ren_1" || detail.Rental["status"] != "active" {
		t.Fatalf("rental = %+v", detail.Rental)
	}
	if detail.Webhook != nil {
		t.Fatalf("Webhook = %+v, want nil", detail.Webhook)
	}
}

func TestDevicesGetDecodesNotFound(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusNotFound, `{"ok":false,"code":"rental_not_found"}`))
	_, err := c.Devices.Get(context.Background(), "dev_x")
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 404 || apiErr.Code != "rental_not_found" {
		t.Fatalf("err = %v", err)
	}
}

func TestDevicesSetProfileRequestShape(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,"pushName":"Support"}`))
	if err := c.Devices.SetProfile(context.Background(), "dev_1", "Support"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	req := rec.snapshot()
	if req.method != http.MethodPatch || req.path != "/devices/dev_1/profile" {
		t.Fatalf("got %s %s", req.method, req.path)
	}
	if ct := req.header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(req.body, &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 || body["pushName"] != "Support" {
		t.Fatalf("body = %s", req.body)
	}
}

func TestDevicesUploadAvatarMultipart(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,"hasAvatar":true}`))
	if err := c.Devices.UploadAvatar(context.Background(), "dev_1", "me.png", "image/png", strings.NewReader("PNGDATA")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	req := rec.snapshot()
	if req.method != http.MethodPatch || req.path != "/devices/dev_1/profile" {
		t.Fatalf("got %s %s", req.method, req.path)
	}
	if err := rec.parseMultipart(1 << 20); err != nil {
		t.Fatalf("not multipart: %v", err)
	}
	if name := rec.fileName("avatar"); name != "me.png" {
		t.Fatalf("avatar part name = %q", name)
	}
	if ct := rec.fileContentType("avatar"); ct != "image/png" {
		t.Fatalf("avatar Content-Type = %q", ct)
	}
	if body := rec.fileBody("avatar"); body != "PNGDATA" {
		t.Fatalf("avatar body = %q", body)
	}
}

func TestDevicesSetPresenceRequestShape(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,"type":"available"}`))
	if err := c.Devices.SetPresence(context.Background(), "dev_1", PresenceAvailable); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	req := rec.snapshot()
	if req.method != http.MethodPost || req.path != "/devices/dev_1/presence" {
		t.Fatalf("got %s %s", req.method, req.path)
	}
	var body map[string]any
	if err := json.Unmarshal(req.body, &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 || body["type"] != "available" {
		t.Fatalf("body = %s", req.body)
	}
	if PresenceAvailable != "available" || PresenceUnavailable != "unavailable" {
		t.Fatal("presence constants must match the API values")
	}
}

func TestDevicesSetChatPresenceRequestShape(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,"action":"start"}`))
	if err := c.Devices.SetChatPresence(context.Background(), "dev_1", "+6281234567890", ChatPresenceStart); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	req := rec.snapshot()
	if req.method != http.MethodPost || req.path != "/devices/dev_1/chat-presence" {
		t.Fatalf("got %s %s", req.method, req.path)
	}
	if key := req.header.Get("Idempotency-Key"); key != "" {
		t.Fatalf("Idempotency-Key = %q, want none", key)
	}
	var body map[string]any
	if err := json.Unmarshal(req.body, &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 2 || body["phone"] != "+6281234567890" || body["action"] != "start" {
		t.Fatalf("body = %s", req.body)
	}
	if ChatPresenceStart != "start" || ChatPresenceStop != "stop" {
		t.Fatal("chat presence constants must match the API values")
	}
}

func TestDevicesSendMessageRequestShape(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,"batchId":"b_5","created":true,"progress":{"batchId":"b_5","total":1,"counts":{"queued":1,"sending":0,"accepted":0,"failed":0,"uncertain":0,"cancelled":0},"terminal":false,"items":[{"id":"it_1","recipient":"+6281234567890","kind":"text","sendState":"queued","deliveryState":"pending","attempts":0,"messageId":null,"errorCode":null,"completedAt":null}]}}`))

	got, err := c.Devices.SendMessage(context.Background(), "dev_1", SendMessageParams{
		Phone: "+6281234567890", Message: "Halo!", IdempotencyKey: "msg-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	req := rec.snapshot()
	if req.method != http.MethodPost || req.path != "/devices/dev_1/messages" {
		t.Fatalf("got %s %s", req.method, req.path)
	}
	if req.header.Get("Idempotency-Key") != "msg-1" {
		t.Fatalf("Idempotency-Key = %q", req.header.Get("Idempotency-Key"))
	}
	var body map[string]any
	if err := json.Unmarshal(req.body, &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 2 || body["phone"] != "+6281234567890" || body["message"] != "Halo!" {
		t.Fatalf("body = %s", req.body)
	}

	if got.BatchID != "b_5" || !got.Created {
		t.Fatalf("result = %+v", got)
	}
	if got.Progress == nil || got.Progress.Total != 1 || got.Progress.Counts.Queued != 1 || got.Progress.Terminal {
		t.Fatalf("progress = %+v", got.Progress)
	}
	if len(got.Progress.Items) != 1 {
		t.Fatalf("items = %+v", got.Progress.Items)
	}
	item := got.Progress.Items[0]
	if item.ID != "it_1" || item.Recipient != "+6281234567890" || item.Kind != "text" ||
		item.SendState != "queued" || item.DeliveryState != "pending" || item.Attempts != 0 {
		t.Fatalf("item = %+v", item)
	}
	if item.MessageID != nil || item.ErrorCode != nil || item.CompletedAt != nil {
		t.Fatalf("nullable item fields = %+v", item)
	}
}

func TestDevicesSendMessageRequiresIdempotencyKey(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true}`))
	_, err := c.Devices.SendMessage(context.Background(), "dev_1", SendMessageParams{Phone: "+6281234567890", Message: "hi"})
	if !errors.Is(err, ErrMissingIdempotencyKey) {
		t.Fatalf("err = %v, want ErrMissingIdempotencyKey", err)
	}
	if got := rec.snapshot().calls; got != 0 {
		t.Fatalf("calls = %d, want 0", got)
	}
}

func TestDevicesSendMessageDecodesAPIError(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusConflict, `{"ok":false,"code":"device_not_connected"}`))
	_, err := c.Devices.SendMessage(context.Background(), "dev_1", SendMessageParams{
		Phone: "+6281234567890", Message: "hi", IdempotencyKey: "k",
	})
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 409 || apiErr.Code != "device_not_connected" || apiErr.Retryable() {
		t.Fatalf("err = %v", err)
	}
}

func TestDevicesSendMediaMultipart(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,"batchId":"b_6","created":true,"media":{"id":"media_1"}}`))
	got, err := c.Devices.SendMedia(context.Background(), "dev_1", "foto.jpg", "image/jpeg", "+6281234567890", "Lihat ini", strings.NewReader("JPEG"), "media-key-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	req := rec.snapshot()
	if req.method != http.MethodPost || req.path != "/devices/dev_1/messages" {
		t.Fatalf("got %s %s", req.method, req.path)
	}
	if req.header.Get("Idempotency-Key") != "media-key-1" {
		t.Fatalf("Idempotency-Key = %q", req.header.Get("Idempotency-Key"))
	}
	if err := rec.parseMultipart(1 << 20); err != nil {
		t.Fatalf("not multipart: %v", err)
	}
	if name := rec.fileName("file"); name != "foto.jpg" {
		t.Fatalf("file part name = %q", name)
	}
	if ct := rec.fileContentType("file"); ct != "image/jpeg" {
		t.Fatalf("file Content-Type = %q", ct)
	}
	if body := rec.fileBody("file"); body != "JPEG" {
		t.Fatalf("file body = %q", body)
	}
	if phone := rec.formValue("phone"); phone != "+6281234567890" {
		t.Fatalf("phone field = %q", phone)
	}
	if caption := rec.formValue("caption"); caption != "Lihat ini" {
		t.Fatalf("caption field = %q", caption)
	}
	if got.BatchID != "b_6" || got.Media == nil || got.Media.ID != "media_1" {
		t.Fatalf("result = %+v", got)
	}
}

func TestDevicesSendMediaOmitsEmptyCaption(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,"batchId":"b_7","created":true}`))
	if _, err := c.Devices.SendMedia(context.Background(), "dev_1", "f.pdf", "application/pdf", "+6281234567890", "", strings.NewReader("PDF"), "k2"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := rec.parseMultipart(1 << 20); err != nil {
		t.Fatalf("not multipart: %v", err)
	}
	if _, ok := rec.snapshot().form.Value["caption"]; ok {
		t.Fatalf("caption must be omitted when empty: %+v", rec.snapshot().form.Value)
	}
	if phone := rec.formValue("phone"); phone != "+6281234567890" {
		t.Fatalf("phone field = %q", phone)
	}
}

func TestDevicesSendMediaRequiresIdempotencyKey(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true}`))
	_, err := c.Devices.SendMedia(context.Background(), "dev_1", "f.pdf", "application/pdf", "+6281234567890", "", strings.NewReader("PDF"), "")
	if !errors.Is(err, ErrMissingIdempotencyKey) {
		t.Fatalf("err = %v, want ErrMissingIdempotencyKey", err)
	}
	if got := rec.snapshot().calls; got != 0 {
		t.Fatalf("calls = %d, want 0", got)
	}
}

func TestDevicesPathsAreEscaped(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,"hasAvatar":true}`))
	if err := c.Devices.UploadAvatar(context.Background(), "dev/1", "a.png", "image/png", strings.NewReader("x")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := rec.snapshot().path; got != "/devices/dev/1/profile" && got != "/devices/dev%2F1/profile" {
		t.Fatalf("path = %q", got)
	}
}

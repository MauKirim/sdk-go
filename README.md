# MauKirim Go SDK

[![Go Reference](https://pkg.go.dev/badge/github.com/MauKirim/sdk-go.svg)](https://pkg.go.dev/github.com/MauKirim/sdk-go)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.23-00ADD8.svg)](go.mod)
[![Tests](https://img.shields.io/badge/tests-passing-brightgreen.svg)](#testing)

The official Go client for **MauKirim** ([maukirim.com](https://maukirim.com)), a managed WhatsApp
gateway. It covers the whole public API — OTP delivery and verification, templated notifications
from a shared number, and rented numbers you operate (profile, presence, text, media) — plus the
webhook signing scheme, so you can verify every event MauKirim delivers to your endpoint. Requests
are authenticated with an API key (`mk_live_…`) created in your dashboard at
[app.maukirim.com](https://app.maukirim.com), and every method returns the API's own error codes.

## Install

```bash
go get github.com/MauKirim/sdk-go
```

Requires Go 1.23 or newer. There are no third-party dependencies.

## Quick start

```go
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"time"

	maukirim "github.com/MauKirim/sdk-go"
)

func main() {
	apiKey := os.Getenv("MAUKIRIM_API_KEY") // mk_live_…

	client := maukirim.NewClient(apiKey,
		maukirim.WithMaxRetries(3),
		maukirim.WithUserAgent("my-app/1.2.0"),
		// maukirim.WithBaseURL("https://app.maukirim.com/api/v1"),
		// maukirim.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sent, err := client.OTP.Send(ctx, maukirim.SendOTPParams{
		Phone:          "+6281234567890",
		Purpose:        maukirim.PurposeLogin,
		IdempotencyKey: "otp-login-user-42", // required: reusing it replays, never double-charges
	})
	if err != nil {
		log.Fatalf("send otp: %v", err)
	}
	log.Printf("challenge %s expires at %s", sent.ChallengeID, sent.ExpiresAt)

	checked, err := client.OTP.Verify(ctx, maukirim.VerifyOTPParams{
		ChallengeID: sent.ChallengeID,
		Code:        "123456",
	})
	if err != nil {
		log.Fatalf("verify otp: %v", err)
	}
	if !checked.Verified {
		log.Fatal("the code was wrong, expired or already used")
	}
}
```

A wrong, expired, used or undelivered code is **not** an error: `/otp/verify` answers `200` with
`Verified == false`. Only malformed input, an unknown challenge, scope problems or a server fault
return an error.

## Send a templated notification

Notifications go out from a shared number using a template approved for your account. `Variables`
maps each top-level placeholder to a string, or to a list section as `[]map[string]string`.

```go
result, err := client.Notifications.Send(ctx, maukirim.SendNotificationParams{
	Phone:      "+6281234567890",
	TemplateID: "tpl_order_paid",
	Variables: map[string]any{
		"name":   "Ayu",
		"amount": "Rp 25.000",
		"items": []map[string]string{
			{"sku": "A-1", "qty": "2"},
			{"sku": "B-7", "qty": "1"},
		},
	},
	IdempotencyKey: "order-1001",
})
if err != nil {
	log.Fatalf("send notification: %v", err)
}
log.Println("accepted batch", result.BatchID)
```

Attach a file by uploading it first and passing the returned media id — for per-send templates only:

```go
file, err := os.Open("invoice.pdf")
if err != nil {
	log.Fatal(err)
}
defer file.Close()

media, err := client.Notifications.UploadMedia(ctx, "invoice.pdf", "application/pdf", file)
if err != nil {
	log.Fatalf("upload: %v", err)
}

_, err = client.Notifications.Send(ctx, maukirim.SendNotificationParams{
	Phone:              "+6281234567890",
	TemplateID:         "tpl_invoice",
	Variables:          map[string]any{"name": "Ayu"},
	AttachmentUploadID: media.ID, // uploads expire after 7 days
	IdempotencyKey:     "invoice-1001",
})
```

## Operate a rented number

Every rented number is a `Device`. A send is queued as a batch and its delivery state arrives by
webhook; there is no batch-status endpoint.

```go
devices, err := client.Devices.List(ctx)
if err != nil {
	log.Fatalf("list devices: %v", err)
}
for _, d := range devices {
	log.Printf("%s %s %s (%s)", d.ID, d.Number, d.Status, d.Label)
}

device, err := client.Devices.Get(ctx, "dev_1")
if err != nil {
	log.Fatalf("get device: %v", err)
}
log.Printf("%s is %v", device.Device.ID, device.Device.Connection)

if err := client.Devices.SetPresence(ctx, "dev_1", maukirim.PresenceAvailable); err != nil {
	log.Fatalf("set presence: %v", err)
}

// Typing indicator for one recipient.
if err := client.Devices.SetChatPresence(ctx, "dev_1", "+6281234567890", maukirim.ChatPresenceStart); err != nil {
	log.Fatalf("chat presence: %v", err)
}

result, err := client.Devices.SendMessage(ctx, "dev_1", maukirim.SendMessageParams{
	Phone:          "+6281234567890",
	Message:        "Halo! Pesanan Anda sudah dikirim.",
	IdempotencyKey: "order-1001-text",
})
if err != nil {
	log.Fatalf("send message: %v", err)
}
log.Printf("batch %s created=%v queued=%d", result.BatchID, result.Created,
	result.Progress.Counts.Queued)

// Image or document, with an optional caption.
photo, err := os.Open("receipt.jpg")
if err != nil {
	log.Fatal(err)
}
defer photo.Close()

media, err := client.Devices.SendMedia(ctx, "dev_1", "receipt.jpg", "image/jpeg",
	"+6281234567890", "Struk pembayaran", photo, "order-1001-media")
if err != nil {
	log.Fatalf("send media: %v", err)
}
log.Println("media batch", media.BatchID)
```

Profile changes ride the same routes: `SetProfile(ctx, id, pushName)` for the WhatsApp-visible
name and `UploadAvatar(ctx, id, fileName, contentType, r)` for the photo.

## Webhooks

Point one device's events at your endpoint with `Webhooks.Set`. The signing secret is returned
**once**, when the destination is created (editing keeps it, `Rotate` replaces it):

```go
created, err := client.Webhooks.Set(ctx, "dev_1", maukirim.SetWebhookParams{
	URL:    "https://example.com/maukirim/hook",
	Events: []string{"message", "message.ack"},
	// Active defaults to true; pass a *bool to disable the destination.
})
if err != nil {
	log.Fatalf("set webhook: %v", err)
}
log.Printf("store this secret now: %s", created.Secret)

page, err := client.Webhooks.Deliveries(ctx, maukirim.DeliveryQuery{
	DeviceID: "dev_1",
	Limit:    50,
})
if err != nil {
	log.Fatalf("deliveries: %v", err)
}
for _, d := range page.Deliveries {
	log.Printf("%s %s -> %s (attempts=%d)", d.ID, d.Event, d.State, d.Attempts)
}
```

Verify every delivery against the **raw** request body — re-serializing the JSON changes the bytes
and breaks the signature:

```go
func webhookHandler(secret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}

		signature := r.Header.Get("x-maukirim-signature") // sha256=<hex>
		if !maukirim.VerifyWebhookSignature(body, signature, secret) {
			http.Error(w, "bad signature", http.StatusUnauthorized)
			return
		}

		// Retries reuse x-maukirim-delivery, so deduplicate on it: delivery
		// is at-least-once.
		deliveryID := r.Header.Get("x-maukirim-delivery")
		event := r.Header.Get("x-maukirim-event")
		log.Printf("event %s (%s): %s", event, deliveryID, body)

		w.WriteHeader(http.StatusOK) // any 2xx acknowledges; 3xx does not
	}
}
```

`VerifyWebhookSignature` compares in constant time, accepts uppercase hex, requires the `sha256=`
prefix, and rejects a missing or malformed header as well as an empty secret.

## Error handling

Every failure is an `*maukirim.Error` carrying the API's `Code`, the `StatusCode` (0 for transport
failures) and a `Message`. `errors.Is` also matches `maukirim.ErrMissingIdempotencyKey`, which is
returned without a request when a route that requires an idempotency key is given an empty one.

```go
result, err := client.OTP.Send(ctx, maukirim.SendOTPParams{
	Phone: "+6281234567890", Purpose: maukirim.PurposeLogin, IdempotencyKey: key,
})
if err != nil {
	if errors.Is(err, maukirim.ErrMissingIdempotencyKey) {
		return fmt.Errorf("bug: no idempotency key for otp send: %w", err)
	}

	var apiErr *maukirim.Error
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case "rate_limited":
			// 5 OTP sends per hour per (account, phone): back off, do not hammer.
		case "insufficient_credits":
			// 402: top up the account balance, then resend with the same key.
		case "device_not_connected":
			// 409: no sending number is ready right now.
		case "batch_idempotency_conflict":
			// 409: this key was used with a different request — never retry as-is.
		}
		if apiErr.Retryable() {
			// true for 500/502/503 and for transport errors and timeouts;
			// false for every 4xx.
		}
		return fmt.Errorf("otp send failed: %w", err)
	}
	return err // context cancellation, malformed response, …
}
```

Retries happen for you: on a retryable failure the client repeats the request up to `MaxRetries`
times (default 2, configurable with `WithMaxRetries`) with the **same** `Idempotency-Key`, waiting
100 ms then 300 ms, and stops immediately if the `context.Context` is cancelled.

## API surface

| Namespace | Methods |
|---|---|
| `client.OTP` | `Send`, `Verify` |
| `client.Notifications` | `Send`, `UploadMedia` |
| `client.Devices` | `List`, `Get`, `SetProfile`, `UploadAvatar`, `SetPresence`, `SetChatPresence`, `SendMessage`, `SendMedia` |
| `client.Webhooks` | `Get`, `Set`, `Rotate`, `Remove`, `Deliveries` |
| package | `VerifyWebhookSignature`, `NewClient`, `WithBaseURL`, `WithHTTPClient`, `WithMaxRetries`, `WithUserAgent`, `ErrMissingIdempotencyKey` |

`Idempotency-Key` is required on `/otp/send`, `/messages/send` and `/devices/{id}/messages`.
Responses are decoded into typed structs whose JSON names match the API exactly; nullable fields
are pointers, so `nil` means "the API said null".

## Testing

```bash
go test ./... -race -count=1
```

Every test runs against `net/http/httptest`; the suite never touches the network.

## Publishing

The module is already importable as soon as the repository is tagged — no registry step is
needed, because Go modules resolve straight from the Git tag:

```bash
git tag v1.0.0
git push origin v1.0.0
```

That makes `v1.0.0` resolvable in `go.mod` files, and pkg.go.dev renders the documentation for it.
To have the version listed and indexed on the site, request it once at
<https://pkg.go.dev/github.com/MauKirim/sdk-go> (the "Request" link appears until the module is
indexed for that version); afterwards new tags appear automatically.

Versioning rules:

- Tags follow [semantic versioning](https://semver.org): `v1.2.3`. Breaking changes require a
  major bump.
- A `v2` (or later) release must live at a matching module path — `github.com/MauKirim/sdk-go/v2`
  — with the `go.mod` module directive updated, per Go's
  [major version subdirectory rule](https://go.dev/blog/v2-go-modules).
- Pre-releases (`v1.1.0-rc.1`) are ignored by default and can be adopted with
  `go get github.com/MauKirim/sdk-go@v1.1.0-rc.1`.

## License

MIT — see [LICENSE](LICENSE).

package maukirim

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

var webhookBody = []byte(`{"event":"message","delivery_id":"dl_1","device_id":"dev_1","created_at":"2026-10-02T00:00:00.000Z","payload":{"from":"+6281234567890","text":"Halo"}}`)

func TestVerifyWebhookSignatureAcceptsValidSignature(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if !VerifyWebhookSignature(webhookBody, sign(secret, webhookBody), secret) {
		t.Fatal("valid signature rejected")
	}
}

func TestVerifyWebhookSignatureAcceptsRawBodyExactly(t *testing.T) {
	const secret = "s3cret"
	// A body with insignificant JSON whitespace still hashes byte-for-byte.
	body := []byte("{\n  \"event\": \"message\",\n  \"delivery_id\": \"dl_9\"\n}\n")
	if !VerifyWebhookSignature(body, sign(secret, body), secret) {
		t.Fatal("raw body must be the signed message")
	}
	// Re-serializing the JSON must not verify.
	compact := []byte(`{"event":"message","delivery_id":"dl_9"}`)
	if VerifyWebhookSignature(compact, sign(secret, body), secret) {
		t.Fatal("a different byte sequence must not verify")
	}
}

func TestVerifyWebhookSignatureAcceptsUppercaseHex(t *testing.T) {
	const secret = "s3cret"
	if !VerifyWebhookSignature(webhookBody, strings.ToUpper(sign(secret, webhookBody)), secret) {
		t.Fatal("uppercase hex must be accepted")
	}
	header := strings.ToUpper(sign(secret, webhookBody))
	if !VerifyWebhookSignature(webhookBody, "  "+header+"\n", secret) {
		t.Fatal("surrounding whitespace in the header must be tolerated")
	}
}

func TestVerifyWebhookSignatureRejectsWrongSecret(t *testing.T) {
	if VerifyWebhookSignature(webhookBody, sign("wrong-secret", webhookBody), "right-secret") {
		t.Fatal("signature from another secret must be rejected")
	}
}

func TestVerifyWebhookSignatureRejectsTamperedBody(t *testing.T) {
	const secret = "s3cret"
	signature := sign(secret, webhookBody)
	tampered := []byte(strings.Replace(string(webhookBody), "+6281234567890", "+6289999999999", 1))
	if VerifyWebhookSignature(tampered, signature, secret) {
		t.Fatal("tampered body must be rejected")
	}
}

func TestVerifyWebhookSignatureRejectsMissingAndMalformedHeaders(t *testing.T) {
	const secret = "s3cret"
	cases := []struct {
		name   string
		header string
	}{
		{"missing", ""},
		{"whitespace only", "   "},
		{"no prefix", "deadbeef"},
		{"wrong prefix", "sha1=" + hex.EncodeToString([]byte{1, 2, 3})},
		{"empty hex", "sha256="},
		{"not hex", "sha256=zzzzzz"},
		{"odd length hex", "sha256=abc"},
		{"wrong length hex", "sha256=" + strings.Repeat("ab", 16)},
		{"base64 instead of hex", "sha256=" + hex.EncodeToString([]byte("not-hex-not-hex-not-hex-not-hex!"))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if VerifyWebhookSignature(webhookBody, tc.header, secret) {
				t.Fatalf("header %q must not verify", tc.header)
			}
		})
	}
}

func TestVerifyWebhookSignatureRejectsEmptySecret(t *testing.T) {
	// A signature computed over an empty key must never be accepted, otherwise
	// an unconfigured webhook would be forgeable by anyone.
	if VerifyWebhookSignature(webhookBody, sign("", webhookBody), "") {
		t.Fatal("empty secret must not verify")
	}
}

func TestVerifyWebhookSignatureNotificationCallback(t *testing.T) {
	const secret = "notif-secret"
	body := []byte(`{"event":"notification.delivered","delivery_id":"d_1","batch_id":"b_1","item_id":"i_1","status":"delivered","created_at":"2026-10-02T00:00:00.000Z"}`)
	if !VerifyWebhookSignature(body, sign(secret, body), secret) {
		t.Fatal("notification callback signature rejected")
	}
	if VerifyWebhookSignature(body, sign(secret, append(body, ' ')), secret) {
		t.Fatal("signature over a different body must be rejected")
	}
}

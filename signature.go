package maukirim

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// signaturePrefix is the algorithm tag MauKirim puts in front of the hex digest.
const signaturePrefix = "sha256="

// VerifyWebhookSignature reports whether signatureHeader authenticates rawBody
// with secret.
//
// MauKirim signs every webhook and notification callback with
// HMAC-SHA256 over the raw request body, hex encoded and prefixed with
// "sha256=", delivered in the x-maukirim-signature header:
//
//	body, _ := io.ReadAll(r.Body)
//	if !maukirim.VerifyWebhookSignature(body, r.Header.Get("x-maukirim-signature"), secret) {
//		http.Error(w, "bad signature", http.StatusUnauthorized)
//		return
//	}
//
// Pass the bytes exactly as received; re-serializing the JSON changes them.
// Comparison is constant time, and a missing, malformed or empty-secret
// signature is rejected.
func VerifyWebhookSignature(rawBody []byte, signatureHeader, secret string) bool {
	if secret == "" {
		return false
	}
	header := strings.TrimSpace(signatureHeader)
	if len(header) <= len(signaturePrefix) || !strings.EqualFold(header[:len(signaturePrefix)], signaturePrefix) {
		return false
	}
	provided, err := hex.DecodeString(header[len(signaturePrefix):])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(rawBody)
	return hmac.Equal(provided, mac.Sum(nil))
}

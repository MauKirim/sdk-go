package maukirim

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestOTPSendRequestShape(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,"challengeId":"ch_1","batchId":"b_1","expiresAt":"2026-10-02T10:05:00.000Z"}`))

	got, err := c.OTP.Send(context.Background(), SendOTPParams{
		Phone:          "+6281234567890",
		Purpose:        PurposeLogin,
		IdempotencyKey: "otp-42",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ChallengeID != "ch_1" || got.BatchID != "b_1" || got.ExpiresAt != "2026-10-02T10:05:00.000Z" {
		t.Fatalf("decoded result = %+v", got)
	}

	req := rec.snapshot()
	if req.method != http.MethodPost || req.path != "/otp/send" {
		t.Fatalf("got %s %s", req.method, req.path)
	}
	if req.header.Get("Authorization") != "Bearer mk_live_test" {
		t.Fatalf("Authorization = %q", req.header.Get("Authorization"))
	}
	if req.header.Get("Idempotency-Key") != "otp-42" {
		t.Fatalf("Idempotency-Key = %q", req.header.Get("Idempotency-Key"))
	}
	if ct := req.header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q", ct)
	}

	var body map[string]any
	if err := json.Unmarshal(req.body, &body); err != nil {
		t.Fatalf("request body is not JSON: %v (%s)", err, req.body)
	}
	if len(body) != 2 {
		t.Fatalf("body has %d fields, want exactly 2: %s", len(body), req.body)
	}
	if body["phone"] != "+6281234567890" || body["purpose"] != "login" {
		t.Fatalf("body = %s", req.body)
	}
}

func TestOTPSendRequiresIdempotencyKey(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true}`))

	_, err := c.OTP.Send(context.Background(), SendOTPParams{Phone: "+6281234567890", Purpose: PurposeSignup})
	if !errors.Is(err, ErrMissingIdempotencyKey) {
		t.Fatalf("err = %v, want ErrMissingIdempotencyKey", err)
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("errors.As(*Error) failed for %T", err)
	}
	if apiErr.Retryable() {
		t.Fatal("a client-side validation error must not be retryable")
	}
	if got := rec.snapshot().calls; got != 0 {
		t.Fatalf("calls = %d, want 0 (no request for a missing key)", got)
	}
}

func TestOTPSendDecodesAPIError(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusTooManyRequests, `{"ok":false,"code":"rate_limited"}`))

	_, err := c.OTP.Send(context.Background(), SendOTPParams{
		Phone: "+6281234567890", Purpose: PurposeLogin, IdempotencyKey: "k",
	})
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v", err)
	}
	if apiErr.StatusCode != http.StatusTooManyRequests || apiErr.Code != "rate_limited" || apiErr.Retryable() {
		t.Fatalf("apiErr = %+v", apiErr)
	}
}

func TestOTPVerifyRequestShapeAndUnverifiedIsSuccess(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,"verified":false}`))

	got, err := c.OTP.Verify(context.Background(), VerifyOTPParams{ChallengeID: "ch_1", Code: "000000"})
	if err != nil {
		t.Fatalf("a wrong code must not be an error: %v", err)
	}
	if got.Verified {
		t.Fatal("Verified = true, want false")
	}

	req := rec.snapshot()
	if req.method != http.MethodPost || req.path != "/otp/verify" {
		t.Fatalf("got %s %s", req.method, req.path)
	}
	if key := req.header.Get("Idempotency-Key"); key != "" {
		t.Fatalf("Idempotency-Key = %q, want none on /otp/verify", key)
	}
	var body map[string]any
	if err := json.Unmarshal(req.body, &body); err != nil {
		t.Fatalf("body = %s", req.body)
	}
	if len(body) != 2 || body["challengeId"] != "ch_1" || body["code"] != "000000" {
		t.Fatalf("body = %s", req.body)
	}
}

func TestOTPVerifyVerified(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusOK, `{"ok":true,"verified":true}`))
	got, err := c.OTP.Verify(context.Background(), VerifyOTPParams{ChallengeID: "ch_1", Code: "123456"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Verified {
		t.Fatal("Verified = false, want true")
	}
}

func TestOTPVerifyUnknownChallengeIsAPIError(t *testing.T) {
	rec := newRecorder()
	c, _ := testClient(t, rec.handler(http.StatusNotFound, `{"ok":false,"code":"not_found"}`))
	_, err := c.OTP.Verify(context.Background(), VerifyOTPParams{ChallengeID: "gone", Code: "123456"})
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound || apiErr.Code != "not_found" {
		t.Fatalf("err = %v", err)
	}
}

func TestOTPPurposeConstants(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{PurposeLogin, "login"},
		{PurposeSignup, "signup"},
		{PurposePayment, "payment"},
		{PurposePasswordReset, "passwordReset"},
		{PurposePhoneChange, "phoneChange"},
	} {
		if tc.got != tc.want {
			t.Errorf("purpose constant = %q, want %q", tc.got, tc.want)
		}
	}
}

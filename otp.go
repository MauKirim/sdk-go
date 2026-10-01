package maukirim

import (
	"context"
	"encoding/json"
)

// Purposes accepted by [OTPService.Send].
const (
	PurposeLogin         = "login"
	PurposeSignup        = "signup"
	PurposePayment       = "payment"
	PurposePasswordReset = "passwordReset"
	PurposePhoneChange   = "phoneChange"
)

// OTPService sends and verifies WhatsApp one-time passwords.
type OTPService struct {
	client *Client
}

// SendOTPParams are the inputs of [OTPService.Send].
type SendOTPParams struct {
	// Phone is the recipient in E.164 format, e.g. "+6281234567890".
	Phone string
	// Purpose is one of the Purpose* constants.
	Purpose string
	// IdempotencyKey is required (1–128 characters). Reusing a key with the
	// same phone and purpose replays the original challenge; reusing it with a
	// different request is rejected with batch_idempotency_conflict.
	IdempotencyKey string
}

// SendOTPResult is the accepted OTP challenge. The code itself is sent to the
// recipient and is never returned by the API.
type SendOTPResult struct {
	ChallengeID string `json:"challengeId"`
	BatchID     string `json:"batchId"`
	ExpiresAt   string `json:"expiresAt"`
}

type sendOTPRequest struct {
	Phone   string `json:"phone"`
	Purpose string `json:"purpose"`
}

// Send delivers a six-digit OTP to Phone. Requires an IdempotencyKey.
func (s *OTPService) Send(ctx context.Context, params SendOTPParams) (*SendOTPResult, error) {
	if err := requireIdempotencyKey(params.IdempotencyKey); err != nil {
		return nil, err
	}
	body, err := json.Marshal(sendOTPRequest{Phone: params.Phone, Purpose: params.Purpose})
	if err != nil {
		return nil, err
	}
	var result SendOTPResult
	err = s.client.do(ctx, apiRequest{
		method:      "POST",
		path:        "/otp/send",
		body:        body,
		contentType: "application/json",
		idemKey:     params.IdempotencyKey,
	}, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// VerifyOTPParams are the inputs of [OTPService.Verify].
type VerifyOTPParams struct {
	ChallengeID string
	// Code is the six-digit code the recipient received.
	Code string
}

// VerifyOTPResult reports whether the code was accepted. A wrong, expired,
// used, undelivered or out-of-attempts code arrives as a 200 with
// Verified == false, not as an error.
type VerifyOTPResult struct {
	Verified bool `json:"verified"`
}

type verifyOTPRequest struct {
	ChallengeID string `json:"challengeId"`
	Code        string `json:"code"`
}

// Verify checks a code against a challenge from [OTPService.Send].
func (s *OTPService) Verify(ctx context.Context, params VerifyOTPParams) (*VerifyOTPResult, error) {
	body, err := json.Marshal(verifyOTPRequest{ChallengeID: params.ChallengeID, Code: params.Code})
	if err != nil {
		return nil, err
	}
	var result VerifyOTPResult
	err = s.client.do(ctx, apiRequest{
		method:      "POST",
		path:        "/otp/verify",
		body:        body,
		contentType: "application/json",
	}, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

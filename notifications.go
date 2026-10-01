package maukirim

import (
	"context"
	"encoding/json"
	"io"
)

// NotificationsService sends templated notifications from a shared number.
type NotificationsService struct {
	client *Client
}

// SendNotificationParams are the inputs of [NotificationsService.Send].
type SendNotificationParams struct {
	// Phone is the recipient in E.164 format.
	Phone string
	// TemplateID is an approved template's id.
	TemplateID string
	// Variables maps the template's top-level placeholder names to their values:
	// either a string (scalar placeholder) or []map[string]string (a list
	// section, 0–10 objects with exactly that section's keys).
	Variables map[string]any
	// AttachmentUploadID is the media id from [NotificationsService.UploadMedia].
	// Only per-send templates accept it.
	AttachmentUploadID string
	// IdempotencyKey is required (1–128 characters).
	IdempotencyKey string
}

// SendNotificationResult acknowledges an accepted notification.
type SendNotificationResult struct {
	BatchID string `json:"batchId"`
}

type sendNotificationRequest struct {
	Phone              string         `json:"phone"`
	TemplateID         string         `json:"templateId"`
	Variables          map[string]any `json:"variables"`
	AttachmentUploadID string         `json:"attachmentUploadId,omitempty"`
}

// Send queues one templated notification. Requires an IdempotencyKey.
func (s *NotificationsService) Send(ctx context.Context, params SendNotificationParams) (*SendNotificationResult, error) {
	if err := requireIdempotencyKey(params.IdempotencyKey); err != nil {
		return nil, err
	}
	// variables is required by the API: templates without placeholders must
	// still receive an empty object, never null and never a missing key.
	variables := params.Variables
	if variables == nil {
		variables = map[string]any{}
	}
	body, err := json.Marshal(sendNotificationRequest{
		Phone:              params.Phone,
		TemplateID:         params.TemplateID,
		Variables:          variables,
		AttachmentUploadID: params.AttachmentUploadID,
	})
	if err != nil {
		return nil, err
	}
	var result SendNotificationResult
	err = s.client.do(ctx, apiRequest{
		method:      "POST",
		path:        "/messages/send",
		body:        body,
		contentType: "application/json",
		idemKey:     params.IdempotencyKey,
	}, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// UploadedMedia describes a stored media object. Uploads expire after 7 days.
type UploadedMedia struct {
	ID          string  `json:"id"`
	ContentType string  `json:"contentType"`
	ByteSize    int64   `json:"byteSize"`
	FileName    *string `json:"fileName"`
	Kind        string  `json:"kind"`
	ExpiresAt   string  `json:"expiresAt"`
}

// UploadMedia stores a file for a later [NotificationsService.Send].
// It accepts JPEG/PNG up to 5 MB and PDF/Office/CSV/TXT/ZIP up to 16 MB.
func (s *NotificationsService) UploadMedia(ctx context.Context, fileName string, contentType string, r io.Reader) (*UploadedMedia, error) {
	body, header, err := buildMultipart(nil, uploadFile{
		field:       "file",
		fileName:    fileName,
		contentType: contentType,
		reader:      r,
	})
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Media *UploadedMedia `json:"media"`
	}
	err = s.client.do(ctx, apiRequest{
		method:      "POST",
		path:        "/messages/media",
		body:        body,
		contentType: header,
	}, &envelope)
	if err != nil {
		return nil, err
	}
	if envelope.Media == nil {
		return nil, &Error{StatusCode: 200, Code: "invalid_response", Message: "response did not contain media"}
	}
	return envelope.Media, nil
}

package maukirim

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
)

// Presence values accepted by [DevicesService.SetPresence].
const (
	PresenceAvailable   = "available"
	PresenceUnavailable = "unavailable"
)

// Chat presence actions accepted by [DevicesService.SetChatPresence].
const (
	ChatPresenceStart = "start"
	ChatPresenceStop  = "stop"
)

// DevicesService operates rented WhatsApp numbers.
type DevicesService struct {
	client *Client
}

// Device is one rented number as returned by [DevicesService.List].
// The full number, push name and avatar appear only while the rental is
// active or past due; otherwise Number is masked and the rest are cleared.
type Device struct {
	ID               string            `json:"id"`
	Label            string            `json:"label"`
	Number           string            `json:"number"`
	Plan             string            `json:"plan"`
	Status           string            `json:"status"`
	Term             string            `json:"term"`
	Currency         string            `json:"currency"`
	PriceMinor       int64             `json:"priceMinor"`
	AutoRenew        bool              `json:"autoRenew"`
	CurrentPeriodEnd string            `json:"currentPeriodEnd"`
	PushName         *string           `json:"pushName"`
	HasAvatar        bool              `json:"hasAvatar"`
	Connection       *DeviceConnection `json:"connection"`
}

// DeviceConnection is the gateway connection state of a [Device]. Every field
// is null while the rental is not active.
type DeviceConnection struct {
	State        *string `json:"state"`
	IsConnected  *bool   `json:"isConnected"`
	IsLoggedIn   *bool   `json:"isLoggedIn"`
	MissingSince *string `json:"missingSince"`
}

// DeviceDetail is the richer view returned by [DevicesService.Get].
type DeviceDetail struct {
	Device  Device           `json:"device"`
	Rental  map[string]any   `json:"rental"`
	Webhook *WebhookSettings `json:"webhook"`
}

// List returns every number the account holds, newest first (no pagination).
func (s *DevicesService) List(ctx context.Context) ([]Device, error) {
	var envelope struct {
		Devices []Device `json:"devices"`
	}
	err := s.client.do(ctx, apiRequest{method: "GET", path: "/devices"}, &envelope)
	if err != nil {
		return nil, err
	}
	return envelope.Devices, nil
}

// Get returns one number with its rental and webhook settings. It is reachable
// only for active or past due rentals; anything else is rental_not_found.
func (s *DevicesService) Get(ctx context.Context, id string) (*DeviceDetail, error) {
	var detail DeviceDetail
	err := s.client.do(ctx, apiRequest{method: "GET", path: devicePath(id, "")}, &detail)
	if err != nil {
		return nil, err
	}
	return &detail, nil
}

// SetProfile sets the WhatsApp-visible display name (1–64 characters).
func (s *DevicesService) SetProfile(ctx context.Context, id string, pushName string) error {
	body, err := json.Marshal(struct {
		PushName string `json:"pushName"`
	}{PushName: pushName})
	if err != nil {
		return err
	}
	return s.client.do(ctx, apiRequest{
		method:      "PATCH",
		path:        devicePath(id, "/profile"),
		body:        body,
		contentType: "application/json",
	}, nil)
}

// UploadAvatar replaces the profile photo (JPEG or PNG, at most 5 MiB).
// There is no remove-photo operation.
func (s *DevicesService) UploadAvatar(ctx context.Context, id, fileName, contentType string, r io.Reader) error {
	body, header, err := buildMultipart(nil, uploadFile{
		field:       "avatar",
		fileName:    fileName,
		contentType: contentType,
		reader:      r,
	})
	if err != nil {
		return err
	}
	return s.client.do(ctx, apiRequest{
		method:      "PATCH",
		path:        devicePath(id, "/profile"),
		body:        body,
		contentType: header,
	}, nil)
}

// SetPresence shows the number as available or unavailable.
func (s *DevicesService) SetPresence(ctx context.Context, id, presence string) error {
	body, err := json.Marshal(struct {
		Type string `json:"type"`
	}{Type: presence})
	if err != nil {
		return err
	}
	return s.client.do(ctx, apiRequest{
		method:      "POST",
		path:        devicePath(id, "/presence"),
		body:        body,
		contentType: "application/json",
	}, nil)
}

// SetChatPresence starts or stops the typing indicator for one recipient.
func (s *DevicesService) SetChatPresence(ctx context.Context, id, phone, action string) error {
	body, err := json.Marshal(struct {
		Phone  string `json:"phone"`
		Action string `json:"action"`
	}{Phone: phone, Action: action})
	if err != nil {
		return err
	}
	return s.client.do(ctx, apiRequest{
		method:      "POST",
		path:        devicePath(id, "/chat-presence"),
		body:        body,
		contentType: "application/json",
	}, nil)
}

// SendMessageParams are the inputs of [DevicesService.SendMessage].
type SendMessageParams struct {
	// Phone is the recipient in E.164 format.
	Phone string
	// Message is the text to send, at most 4096 characters.
	Message string
	// IdempotencyKey is required (1–128 characters).
	IdempotencyKey string
}

// MessageMedia is the media a send created.
type MessageMedia struct {
	ID string `json:"id"`
}

// SendMessageResult acknowledges a send. Created is false when the request
// replayed an earlier one with the same Idempotency-Key. Delivery state
// arrives by webhook; there is no batch-status endpoint.
type SendMessageResult struct {
	BatchID  string        `json:"batchId"`
	Created  bool          `json:"created"`
	Media    *MessageMedia `json:"media"`
	Progress *Progress     `json:"progress"`
}

// Progress is the initial progress of a send batch.
type Progress struct {
	BatchID  string         `json:"batchId"`
	Total    int            `json:"total"`
	Counts   ProgressCounts `json:"counts"`
	Terminal bool           `json:"terminal"`
	Items    []ProgressItem `json:"items"`
}

// ProgressCounts counts batch items by send state.
type ProgressCounts struct {
	Queued    int `json:"queued"`
	Sending   int `json:"sending"`
	Accepted  int `json:"accepted"`
	Failed    int `json:"failed"`
	Uncertain int `json:"uncertain"`
	Cancelled int `json:"cancelled"`
}

// ProgressItem is one recipient of a send batch.
type ProgressItem struct {
	ID            string  `json:"id"`
	Recipient     string  `json:"recipient"`
	Kind          string  `json:"kind"`
	SendState     string  `json:"sendState"`
	DeliveryState string  `json:"deliveryState"`
	Attempts      int     `json:"attempts"`
	MessageID     *string `json:"messageId"`
	ErrorCode     *string `json:"errorCode"`
	CompletedAt   *string `json:"completedAt"`
}

// SendMessage queues a text message from a rented number. Requires an
// IdempotencyKey.
func (s *DevicesService) SendMessage(ctx context.Context, id string, params SendMessageParams) (*SendMessageResult, error) {
	if err := requireIdempotencyKey(params.IdempotencyKey); err != nil {
		return nil, err
	}
	body, err := json.Marshal(struct {
		Phone   string `json:"phone"`
		Message string `json:"message"`
	}{Phone: params.Phone, Message: params.Message})
	if err != nil {
		return nil, err
	}
	var result SendMessageResult
	err = s.client.do(ctx, apiRequest{
		method:      "POST",
		path:        devicePath(id, "/messages"),
		body:        body,
		contentType: "application/json",
		idemKey:     params.IdempotencyKey,
	}, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// SendMedia queues an image (≤5 MiB) or document (≤16 MiB) with an optional
// caption (≤1024 characters). Requires an IdempotencyKey.
func (s *DevicesService) SendMedia(ctx context.Context, id, fileName, contentType, phone, caption string, r io.Reader, idempotencyKey string) (*SendMessageResult, error) {
	if err := requireIdempotencyKey(idempotencyKey); err != nil {
		return nil, err
	}
	fields := []multipartField{{name: "phone", value: phone}}
	if caption != "" {
		fields = append(fields, multipartField{name: "caption", value: caption})
	}
	body, header, err := buildMultipart(fields, uploadFile{
		field:       "file",
		fileName:    fileName,
		contentType: contentType,
		reader:      r,
	})
	if err != nil {
		return nil, err
	}
	var result SendMessageResult
	err = s.client.do(ctx, apiRequest{
		method:      "POST",
		path:        devicePath(id, "/messages"),
		body:        body,
		contentType: header,
		idemKey:     idempotencyKey,
	}, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// devicePath builds /devices/{id}{suffix} with the id escaped.
func devicePath(id, suffix string) string {
	return "/devices/" + url.PathEscape(id) + suffix
}

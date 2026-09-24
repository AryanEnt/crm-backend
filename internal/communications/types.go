package communications

import "time"

// Account is a WhatsApp or Twilio number/account usable by multiple SEs.
// Credentials are never serialized in API responses.
type Account struct {
	ID                 string         `json:"id"`
	Provider           string         `json:"provider"`
	Name               string         `json:"name"`
	DisplayIdentifier  string         `json:"displayIdentifier"`
	ExternalAccountID  string         `json:"externalAccountId"`
	IsActive           bool           `json:"isActive"`
	OwnerUserID        *string        `json:"ownerUserId,omitempty"`
	TeamID             *string        `json:"teamId,omitempty"`
	AllowRecordings    bool           `json:"allowRecordings"`
	Metadata           map[string]any `json:"metadata,omitempty"`
	CreatedAt          string         `json:"createdAt"`
	UpdatedAt          string         `json:"updatedAt"`
	// Credentials kept off JSON tags intentionally for public DTO; loaded separately.
	Credentials map[string]string `json:"-"`
}

type WhatsAppMessage struct {
	ID                string         `json:"id"`
	AccountID         string         `json:"accountId"`
	Direction         string         `json:"direction"`
	Status            string         `json:"status"`
	ProviderMessageID string         `json:"providerMessageId"`
	ConversationKey   string         `json:"conversationKey"`
	FromNumber        string         `json:"fromNumber"`
	ToNumber          string         `json:"toNumber"`
	Body              string         `json:"body"`
	MediaURL          string         `json:"mediaUrl,omitempty"`
	MediaMime         string         `json:"mediaMime,omitempty"`
	MediaFilename     string         `json:"mediaFilename,omitempty"`
	ErrorCode         string         `json:"errorCode,omitempty"`
	ErrorMessage      string         `json:"errorMessage,omitempty"`
	LeadID            *string        `json:"leadId,omitempty"`
	CustomerID        *string        `json:"customerId,omitempty"`
	DealID            *string        `json:"dealId,omitempty"`
	ActivityID        *string        `json:"activityId,omitempty"`
	TimelineEventID   *string        `json:"timelineEventId,omitempty"`
	ActorUserID       *string        `json:"actorUserId,omitempty"`
	OccurredAt        string         `json:"occurredAt"`
	CreatedAt         string         `json:"createdAt"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

type TwilioCall struct {
	ID              string  `json:"id"`
	AccountID       string  `json:"accountId"`
	Direction       string  `json:"direction"`
	Status          string  `json:"status"`
	ProviderCallSID string  `json:"providerCallSid"`
	FromNumber      string  `json:"fromNumber"`
	ToNumber        string  `json:"toNumber"`
	DurationSeconds *int    `json:"durationSeconds,omitempty"`
	RecordingSID    string  `json:"recordingSid,omitempty"`
	RecordingURL    string  `json:"recordingUrl,omitempty"`
	ErrorCode       string  `json:"errorCode,omitempty"`
	ErrorMessage    string  `json:"errorMessage,omitempty"`
	LeadID          *string `json:"leadId,omitempty"`
	CustomerID      *string `json:"customerId,omitempty"`
	DealID          *string `json:"dealId,omitempty"`
	ActivityID      *string `json:"activityId,omitempty"`
	TimelineEventID *string `json:"timelineEventId,omitempty"`
	ActorUserID     *string `json:"actorUserId,omitempty"`
	StartedAt       *string `json:"startedAt,omitempty"`
	EndedAt         *string `json:"endedAt,omitempty"`
	CreatedAt       string  `json:"createdAt"`
}

type SendWhatsAppInput struct {
	AccountID  string  `json:"accountId"`
	To         string  `json:"to"`
	Body       string  `json:"body"`
	MediaURL   string  `json:"mediaUrl"`
	LeadID     *string `json:"leadId"`
	CustomerID *string `json:"customerId"`
	DealID     *string `json:"dealId"`
}

type PlaceCallInput struct {
	AccountID  string  `json:"accountId"`
	To         string  `json:"to"`
	LeadID     *string `json:"leadId"`
	CustomerID *string `json:"customerId"`
	DealID     *string `json:"dealId"`
}

type CreateAccountInput struct {
	Provider          string            `json:"provider"`
	Name              string            `json:"name"`
	DisplayIdentifier string            `json:"displayIdentifier"`
	ExternalAccountID string            `json:"externalAccountId"`
	Credentials       map[string]string `json:"credentials"`
	OwnerUserID       *string           `json:"ownerUserId"`
	TeamID            *string           `json:"teamId"`
	AllowRecordings   bool              `json:"allowRecordings"`
	IsActive          *bool             `json:"isActive"`
}

// Provider DTOs (internal to WhatsApp / Twilio adapters)

type OutboundMessageRequest struct {
	ToPhoneNumberID string
	To              string
	Body            string
	MediaURL        string
	AccessToken     string
	GraphAPIVersion string
}

type OutboundMessageResult struct {
	ProviderMessageID string
	Raw               map[string]any
}

type InboundWhatsAppEvent struct {
	Kind              string // message | status
	PhoneNumberID     string
	ProviderMessageID string
	Status            string // for status updates: sent, delivered, read, failed
	From              string
	To                string
	Body              string
	MediaURL          string
	MediaMime         string
	MediaFilename     string
	Timestamp         time.Time
	Raw               map[string]any
	EventKey          string // stable idempotency key
}

type OutboundCallRequest struct {
	AccountSID        string
	AuthToken         string
	From              string
	To                string
	StatusCallbackURL string
	TwimlURL          string
}

type OutboundCallResult struct {
	CallSID string
	Status  string
	Raw     map[string]any
}

type CallStatusEvent struct {
	CallSID      string
	AccountSID   string
	From         string
	To           string
	CallStatus   string
	Direction    string
	Duration     *int
	RecordingSID string
	RecordingURL string
	Timestamp    time.Time
	EventKey     string
	Raw          map[string]any
}

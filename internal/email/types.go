package email

import "time"

type AccountPublic struct {
	ID               string     `json:"id"`
	Provider         string     `json:"provider"`
	EmailAddress     string     `json:"emailAddress"`
	DisplayName      string     `json:"displayName"`
	AvatarURL        string     `json:"avatarUrl"`
	ConnectionStatus string     `json:"connectionStatus"`
	SendingEnabled   bool       `json:"sendingEnabled"`
	ReceivingEnabled bool       `json:"receivingEnabled"`
	LastSyncAt       *time.Time `json:"lastSyncAt"`
	LastSyncError    string     `json:"lastSyncError,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
}

type accountRecord struct {
	AccountPublic
	UserID                 string
	EncryptedAccessToken   string
	EncryptedRefreshToken  string
	TokenExpiry            *time.Time
	GmailHistoryID         string
	WatchExpiration        *time.Time
}

type Thread struct {
	ID                  string     `json:"id"`
	AccountID           string     `json:"accountId"`
	OwnerUserID         string     `json:"ownerUserId"`
	OwnerName           string     `json:"ownerName,omitempty"`
	ProviderThreadID    string     `json:"providerThreadId"`
	LeadID              *string    `json:"leadId"`
	LeadName            *string    `json:"leadName"`
	CustomerID          *string    `json:"customerId"`
	CustomerName        *string    `json:"customerName"`
	DealID              *string    `json:"dealId"`
	Participants        []string   `json:"participants"`
	Subject             string     `json:"subject"`
	LastMessageAt       *time.Time `json:"lastMessageAt"`
	LastMessagePreview  string     `json:"lastMessagePreview"`
	MessageCount        int        `json:"messageCount"`
	UnreadCount         int        `json:"unreadCount"`
	Starred             bool       `json:"starred"`
	Archived            bool       `json:"archived"`
	MatchStatus         string     `json:"matchStatus"`
	HasAttachments      bool       `json:"hasAttachments"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
}

type Message struct {
	ID                 string       `json:"id"`
	ThreadID           string       `json:"threadId"`
	AccountID          string       `json:"accountId"`
	OwnerUserID        string       `json:"ownerUserId"`
	ProviderMessageID  string       `json:"providerMessageId"`
	ProviderThreadID   string       `json:"providerThreadId"`
	Direction          string       `json:"direction"`
	FromAddress        string       `json:"from"`
	FromName           string       `json:"fromName"`
	ToAddresses        []string     `json:"to"`
	CcAddresses        []string     `json:"cc"`
	BccAddresses       []string     `json:"bcc"`
	Subject            string       `json:"subject"`
	BodyHTML           string       `json:"bodyHtml"`
	BodyText           string       `json:"bodyText"`
	Snippet            string       `json:"snippet"`
	SentAt             *time.Time   `json:"sentAt"`
	ReceivedAt         *time.Time   `json:"receivedAt"`
	ScheduledAt        *time.Time   `json:"scheduledAt"`
	Status             string       `json:"status"`
	HasAttachments     bool         `json:"hasAttachments"`
	InReplyTo          *string      `json:"inReplyTo"`
	Attachments        []Attachment `json:"attachments,omitempty"`
	CreatedAt          time.Time    `json:"createdAt"`
}

type Attachment struct {
	ID                     string `json:"id"`
	MessageID              string `json:"messageId"`
	Filename               string `json:"filename"`
	MimeType               string `json:"mimeType"`
	SizeBytes              int64  `json:"sizeBytes"`
	ProviderAttachmentID   string `json:"providerAttachmentId"`
}

type Template struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Subject    string    `json:"subject"`
	BodyHTML   string    `json:"bodyHtml"`
	Category   string    `json:"category"`
	Variables  []string  `json:"variables"`
	IsActive   bool      `json:"isActive"`
	CreatedBy  *string   `json:"createdBy"`
	TeamID     *string   `json:"teamId"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type ComposeInput struct {
	AccountID   string   `json:"accountId"`
	To          []string `json:"to"`
	Cc          []string `json:"cc"`
	Bcc         []string `json:"bcc"`
	Subject     string   `json:"subject"`
	BodyHTML    string   `json:"bodyHtml"`
	BodyText    string   `json:"bodyText"`
	LeadID      *string  `json:"leadId"`
	CustomerID  *string  `json:"customerId"`
	DealID      *string  `json:"dealId"`
	InReplyTo   *string  `json:"inReplyTo"`
	ThreadID    *string  `json:"threadId"`
	ScheduledAt *string  `json:"scheduledAt"`
	Send        bool     `json:"send"`
}

type DraftPatch struct {
	To       []string `json:"to"`
	Cc       []string `json:"cc"`
	Bcc      []string `json:"bcc"`
	Subject  *string  `json:"subject"`
	BodyHTML *string  `json:"bodyHtml"`
	BodyText *string  `json:"bodyText"`
}

type AssociateInput struct {
	LeadID     *string `json:"leadId"`
	CustomerID *string `json:"customerId"`
	DealID     *string `json:"dealId"`
}

type TemplateInput struct {
	Name      string   `json:"name"`
	Subject   string   `json:"subject"`
	BodyHTML  string   `json:"bodyHtml"`
	Category  string   `json:"category"`
	Variables []string `json:"variables"`
	IsActive  *bool    `json:"isActive"`
	TeamID    *string  `json:"teamId"`
}

type ThreadListFilter struct {
	Folder      string
	Q           string
	LeadID      string
	CustomerID  string
	OwnerUserID string
	Direction   string
	Status      string
	HasAttach   string
	Unread      string
	From        string
	To          string
	DateFrom    string
	DateTo      string
	Limit       int
	Offset      int
}

type FolderCounts struct {
	Inbox     int `json:"inbox"`
	Sent      int `json:"sent"`
	Drafts    int `json:"drafts"`
	Starred   int `json:"starred"`
	Unmatched int `json:"unmatched"`
	Scheduled int `json:"scheduled"`
	Archived  int `json:"archived"`
}

type IntegrationHealth struct {
	Configured          bool   `json:"configured"`
	OAuthConfigured     bool   `json:"oauthConfigured"`
	GmailAPIEnabled     bool   `json:"gmailApiEnabled"`
	PubSubConfigured    bool   `json:"pubSubConfigured"`
	PushSyncHealthy     bool   `json:"pushSyncHealthy"`
	ConnectedAccounts   int    `json:"connectedAccounts"`
	NeedsAttention      int    `json:"needsAttention"`
	Message             string `json:"message,omitempty"`
}

type PreviewInput struct {
	Subject    string  `json:"subject"`
	BodyHTML   string  `json:"bodyHtml"`
	BodyText   string  `json:"bodyText"`
	LeadID     *string `json:"leadId"`
	CustomerID *string `json:"customerId"`
}

type PreviewResult struct {
	Subject         string   `json:"subject"`
	BodyHTML        string   `json:"bodyHtml"`
	BodyText        string   `json:"bodyText"`
	Unresolved      []string `json:"unresolved"`
	CanSend         bool     `json:"canSend"`
}

type ThreadDetail struct {
	Thread   *Thread    `json:"thread"`
	Messages []Message  `json:"messages"`
}

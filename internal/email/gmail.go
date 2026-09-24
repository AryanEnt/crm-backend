package email

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	googleAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL = "https://oauth2.googleapis.com/token"
	gmailAPIBase   = "https://gmail.googleapis.com/gmail/v1/users/me"
	userInfoURL    = "https://www.googleapis.com/oauth2/v2/userinfo"

	scopeGmailSend     = "https://www.googleapis.com/auth/gmail.send"
	scopeGmailReadonly = "https://www.googleapis.com/auth/gmail.readonly"
	scopeUserInfoEmail = "https://www.googleapis.com/auth/userinfo.email"
	scopeUserInfoProf  = "https://www.googleapis.com/auth/userinfo.profile"
)

func oauthScopes() string {
	return strings.Join([]string{
		scopeGmailSend,
		scopeGmailReadonly,
		scopeUserInfoEmail,
		scopeUserInfoProf,
	}, " ")
}

type googleTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
}

type googleProfile struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

type gmailProfile struct {
	EmailAddress string `json:"emailAddress"`
	HistoryID    uint64 `json:"historyId"`
}

type gmailWatchResponse struct {
	HistoryID  uint64 `json:"historyId"`
	Expiration string `json:"expiration"`
}

type gmailSendResponse struct {
	ID       string `json:"id"`
	ThreadID string `json:"threadId"`
}

type gmailHistoryResponse struct {
	History           []gmailHistory `json:"history"`
	HistoryID         uint64         `json:"historyId"`
	NextPageToken     string         `json:"nextPageToken"`
}

type gmailHistory struct {
	ID            string             `json:"id"`
	MessagesAdded []gmailHistoryItem `json:"messagesAdded"`
}

type gmailHistoryItem struct {
	Message gmailRef `json:"message"`
}

type gmailRef struct {
	ID       string   `json:"id"`
	ThreadID string   `json:"threadId"`
	LabelIDs []string `json:"labelIds"`
}

type gmailListResponse struct {
	Messages      []gmailRef `json:"messages"`
	NextPageToken string     `json:"nextPageToken"`
}

type gmailMessage struct {
	ID           string          `json:"id"`
	ThreadID     string          `json:"threadId"`
	Snippet      string          `json:"snippet"`
	InternalDate string          `json:"internalDate"`
	LabelIDs     []string        `json:"labelIds"`
	Payload      gmailPayload    `json:"payload"`
}

type gmailPayload struct {
	MimeType string         `json:"mimeType"`
	Filename string         `json:"filename"`
	Headers  []gmailHeader  `json:"headers"`
	Body     gmailBody      `json:"body"`
	Parts    []gmailPayload `json:"parts"`
}

type gmailHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type gmailBody struct {
	Size     int    `json:"size"`
	Data     string `json:"data"`
	AttachmentID string `json:"attachmentId"`
}

type GmailClient struct {
	http       *http.Client
	clientID   string
	clientSecret string
	redirect   string
}

func NewGmailClient(clientID, clientSecret, redirect string) *GmailClient {
	return &GmailClient{
		http:         &http.Client{Timeout: 30 * time.Second},
		clientID:     clientID,
		clientSecret: clientSecret,
		redirect:     redirect,
	}
}

func (c *GmailClient) AuthURL(state string) string {
	q := url.Values{}
	q.Set("client_id", c.clientID)
	q.Set("redirect_uri", c.redirect)
	q.Set("response_type", "code")
	q.Set("scope", oauthScopes())
	q.Set("access_type", "offline")
	q.Set("prompt", "consent")
	q.Set("include_granted_scopes", "true")
	q.Set("state", state)
	return googleAuthURL + "?" + q.Encode()
}

func (c *GmailClient) ExchangeCode(ctx context.Context, code string) (*googleTokens, error) {
	form := url.Values{}
	form.Set("code", code)
	form.Set("client_id", c.clientID)
	form.Set("client_secret", c.clientSecret)
	form.Set("redirect_uri", c.redirect)
	form.Set("grant_type", "authorization_code")
	return c.tokenRequest(ctx, form)
}

func (c *GmailClient) Refresh(ctx context.Context, refreshToken string) (*googleTokens, error) {
	form := url.Values{}
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", c.clientID)
	form.Set("client_secret", c.clientSecret)
	form.Set("grant_type", "refresh_token")
	return c.tokenRequest(ctx, form)
}

func (c *GmailClient) tokenRequest(ctx context.Context, form url.Values) (*googleTokens, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("google token error (%d): %s", res.StatusCode, truncate(string(body), 400))
	}
	var tok googleTokens
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

func (c *GmailClient) UserInfo(ctx context.Context, accessToken string) (*googleProfile, error) {
	var p googleProfile
	if err := c.getJSON(ctx, accessToken, userInfoURL, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *GmailClient) GmailProfile(ctx context.Context, accessToken string) (*gmailProfile, error) {
	var p gmailProfile
	if err := c.getJSON(ctx, accessToken, gmailAPIBase+"/profile", &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *GmailClient) Watch(ctx context.Context, accessToken, topic string) (*gmailWatchResponse, error) {
	payload := map[string]any{
		"topicName": topic,
		"labelIds":  []string{"INBOX"},
	}
	var out gmailWatchResponse
	if err := c.postJSON(ctx, accessToken, gmailAPIBase+"/watch", payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *GmailClient) StopWatch(ctx context.Context, accessToken string) {
	_ = c.postJSON(ctx, accessToken, gmailAPIBase+"/stop", map[string]any{}, nil)
}

func (c *GmailClient) Send(ctx context.Context, accessToken, rawRFC822, threadID string) (*gmailSendResponse, error) {
	raw := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(rawRFC822))
	body := map[string]any{"raw": raw}
	if threadID != "" {
		body["threadId"] = threadID
	}
	var out gmailSendResponse
	if err := c.postJSON(ctx, accessToken, gmailAPIBase+"/messages/send", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *GmailClient) GetMessage(ctx context.Context, accessToken, id string) (*gmailMessage, error) {
	var m gmailMessage
	u := gmailAPIBase + "/messages/" + url.PathEscape(id) + "?format=full"
	if err := c.getJSON(ctx, accessToken, u, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (c *GmailClient) History(ctx context.Context, accessToken, startHistoryID string) (*gmailHistoryResponse, error) {
	u := gmailAPIBase + "/history?startHistoryId=" + url.QueryEscape(startHistoryID) + "&historyTypes=messageAdded"
	var out gmailHistoryResponse
	if err := c.getJSON(ctx, accessToken, u, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *GmailClient) ListRecent(ctx context.Context, accessToken string, max int) ([]gmailRef, error) {
	if max <= 0 {
		max = 50
	}
	u := fmt.Sprintf("%s/messages?maxResults=%d&labelIds=INBOX", gmailAPIBase, max)
	var out gmailListResponse
	if err := c.getJSON(ctx, accessToken, u, &out); err != nil {
		return nil, err
	}
	return out.Messages, nil
}

type apiError struct {
	Status  int
	Body    string
	Revoked bool
}

func (e *apiError) Error() string {
	return fmt.Sprintf("gmail api %d: %s", e.Status, e.Body)
}

func (c *GmailClient) getJSON(ctx context.Context, token, rawURL string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return c.doJSON(req, dest)
}

func (c *GmailClient) postJSON(ctx context.Context, token, rawURL string, payload any, dest any) error {
	var rdr io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		rdr = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return c.doJSON(req, dest)
}

func (c *GmailClient) doJSON(req *http.Request, dest any) error {
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		revoked := res.StatusCode == 401 || strings.Contains(strings.ToLower(string(body)), "invalid_grant")
		return &apiError{Status: res.StatusCode, Body: truncate(string(body), 500), Revoked: revoked}
	}
	if dest == nil || len(body) == 0 {
		return nil
	}
	return json.Unmarshal(body, dest)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func decodeGmailBody(data string) string {
	if data == "" {
		return ""
	}
	raw, err := base64.URLEncoding.DecodeString(data)
	if err != nil {
		raw, err = base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(data)
		if err != nil {
			return ""
		}
	}
	return string(raw)
}

func headerValue(headers []gmailHeader, name string) string {
	for _, h := range headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

func collectBodies(p gmailPayload) (html, text string, attachments []parsedAttachment) {
	ct := strings.ToLower(p.MimeType)
	if strings.HasPrefix(ct, "text/html") && p.Body.Data != "" {
		html = decodeGmailBody(p.Body.Data)
	}
	if strings.HasPrefix(ct, "text/plain") && p.Body.Data != "" {
		text = decodeGmailBody(p.Body.Data)
	}
	if p.Filename != "" && p.Body.AttachmentID != "" {
		attachments = append(attachments, parsedAttachment{
			Filename:   p.Filename,
			MimeType:   p.MimeType,
			Size:       int64(p.Body.Size),
			ProviderID: p.Body.AttachmentID,
		})
	}
	for _, part := range p.Parts {
		h, t, a := collectBodies(part)
		if html == "" {
			html = h
		}
		if text == "" {
			text = t
		}
		attachments = append(attachments, a...)
	}
	return html, text, attachments
}

type parsedAttachment struct {
	Filename   string
	MimeType   string
	Size       int64
	ProviderID string
}

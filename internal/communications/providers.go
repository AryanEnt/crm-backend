package communications

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// WhatsAppProvider isolates Meta WhatsApp Cloud API from the rest of the CRM.
// WhatsApp traffic must never go through Twilio.
type WhatsAppProvider interface {
	Send(ctx context.Context, req OutboundMessageRequest) (*OutboundMessageResult, error)
	VerifyWebhook(mode, token, challenge, verifyToken string) (challengeOut string, ok bool)
	ValidateSignature(appSecret string, body []byte, signatureHeader string) bool
	ParseWebhook(body []byte) ([]InboundWhatsAppEvent, error)
}

// TwilioProvider isolates Twilio Voice from the rest of the CRM.
type TwilioProvider interface {
	PlaceCall(ctx context.Context, req OutboundCallRequest) (*OutboundCallResult, error)
	ValidateSignature(authToken, fullURL string, form url.Values, signatureHeader string) bool
	ParseStatusCallback(form url.Values) (*CallStatusEvent, error)
}

// MetaWhatsAppProvider talks to Meta Graph API.
type MetaWhatsAppProvider struct {
	HTTP *http.Client
}

func NewMetaWhatsAppProvider() *MetaWhatsAppProvider {
	return &MetaWhatsAppProvider{HTTP: &http.Client{Timeout: 20 * time.Second}}
}

func (p *MetaWhatsAppProvider) Send(ctx context.Context, req OutboundMessageRequest) (*OutboundMessageResult, error) {
	if strings.TrimSpace(req.AccessToken) == "" || strings.TrimSpace(req.ToPhoneNumberID) == "" {
		return nil, fmt.Errorf("meta whatsapp credentials incomplete")
	}
	ver := req.GraphAPIVersion
	if ver == "" {
		ver = "v21.0"
	}
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"to":                normalizePhone(req.To),
		"type":              "text",
		"text":              map[string]any{"body": req.Body},
	}
	if strings.TrimSpace(req.MediaURL) != "" {
		payload["type"] = "image"
		payload["image"] = map[string]any{"link": req.MediaURL}
		delete(payload, "text")
	}
	body, _ := json.Marshal(payload)
	endpoint := fmt.Sprintf("https://graph.facebook.com/%s/%s/messages", ver, req.ToPhoneNumberID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+req.AccessToken)
	httpReq.Header.Set("Content-Type", "application/json")
	res, err := p.HTTP.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	rawBytes, _ := io.ReadAll(res.Body)
	var raw map[string]any
	_ = json.Unmarshal(rawBytes, &raw)
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("meta whatsapp send failed (%d): %s", res.StatusCode, truncate(string(rawBytes), 400))
	}
	msgID := ""
	if msgs, ok := raw["messages"].([]any); ok && len(msgs) > 0 {
		if m, ok := msgs[0].(map[string]any); ok {
			msgID, _ = m["id"].(string)
		}
	}
	if msgID == "" {
		msgID = "wamid." + uuid.NewString()
	}
	return &OutboundMessageResult{ProviderMessageID: msgID, Raw: raw}, nil
}

func (p *MetaWhatsAppProvider) VerifyWebhook(mode, token, challenge, verifyToken string) (string, bool) {
	if mode == "subscribe" && token != "" && token == verifyToken {
		return challenge, true
	}
	return "", false
}

func (p *MetaWhatsAppProvider) ValidateSignature(appSecret string, body []byte, signatureHeader string) bool {
	if appSecret == "" {
		return false
	}
	sig := strings.TrimPrefix(signatureHeader, "sha256=")
	mac := hmac.New(sha256.New, []byte(appSecret))
	_, _ = mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(strings.ToLower(expected)), []byte(strings.ToLower(sig)))
}

func (p *MetaWhatsAppProvider) ParseWebhook(body []byte) ([]InboundWhatsAppEvent, error) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	var out []InboundWhatsAppEvent
	entries, _ := root["entry"].([]any)
	for _, e := range entries {
		em, _ := e.(map[string]any)
		changes, _ := em["changes"].([]any)
		for _, ch := range changes {
			cm, _ := ch.(map[string]any)
			value, _ := cm["value"].(map[string]any)
			meta, _ := value["metadata"].(map[string]any)
			phoneNumberID, _ := meta["phone_number_id"].(string)
			displayPhone, _ := meta["display_phone_number"].(string)

			if statuses, ok := value["statuses"].([]any); ok {
				for _, st := range statuses {
					sm, _ := st.(map[string]any)
					id, _ := sm["id"].(string)
					status, _ := sm["status"].(string)
					ts := parseUnix(asString(sm["timestamp"]))
					out = append(out, InboundWhatsAppEvent{
						Kind: "status", PhoneNumberID: phoneNumberID, ProviderMessageID: id,
						Status: status, To: displayPhone, Timestamp: ts,
						EventKey: "wa-status:" + id + ":" + status, Raw: sm,
					})
				}
			}
			if messages, ok := value["messages"].([]any); ok {
				for _, msg := range messages {
					mm, _ := msg.(map[string]any)
					id, _ := mm["id"].(string)
					from, _ := mm["from"].(string)
					ts := parseUnix(asString(mm["timestamp"]))
					bodyText := ""
					mediaURL, mediaMime, mediaName := "", "", ""
					if text, ok := mm["text"].(map[string]any); ok {
						bodyText, _ = text["body"].(string)
					}
					if img, ok := mm["image"].(map[string]any); ok {
						mediaURL, _ = img["id"].(string)
						mediaMime, _ = img["mime_type"].(string)
						bodyText = coalesce(bodyText, "[image attachment]")
					}
					if doc, ok := mm["document"].(map[string]any); ok {
						mediaURL, _ = doc["id"].(string)
						mediaMime, _ = doc["mime_type"].(string)
						mediaName, _ = doc["filename"].(string)
						bodyText = coalesce(bodyText, "[document attachment]")
					}
					out = append(out, InboundWhatsAppEvent{
						Kind: "message", PhoneNumberID: phoneNumberID, ProviderMessageID: id,
						From: from, To: displayPhone, Body: bodyText,
						MediaURL: mediaURL, MediaMime: mediaMime, MediaFilename: mediaName,
						Timestamp: ts, EventKey: "wa-msg:" + id, Raw: mm,
					})
				}
			}
		}
	}
	return out, nil
}

// DevWhatsAppProvider simulates Meta WhatsApp when live credentials are absent.
type DevWhatsAppProvider struct{}

func (p *DevWhatsAppProvider) Send(ctx context.Context, req OutboundMessageRequest) (*OutboundMessageResult, error) {
	_ = ctx
	id := "wamid.dev." + uuid.NewString()
	return &OutboundMessageResult{
		ProviderMessageID: id,
		Raw:               map[string]any{"simulated": true, "to": req.To},
	}, nil
}
func (p *DevWhatsAppProvider) VerifyWebhook(mode, token, challenge, verifyToken string) (string, bool) {
	return (&MetaWhatsAppProvider{}).VerifyWebhook(mode, token, challenge, verifyToken)
}
func (p *DevWhatsAppProvider) ValidateSignature(appSecret string, body []byte, signatureHeader string) bool {
	if appSecret == "" {
		return true // local/dev accounts without secret
	}
	return (&MetaWhatsAppProvider{}).ValidateSignature(appSecret, body, signatureHeader)
}
func (p *DevWhatsAppProvider) ParseWebhook(body []byte) ([]InboundWhatsAppEvent, error) {
	return (&MetaWhatsAppProvider{}).ParseWebhook(body)
}

// TwilioVoiceProvider places calls via Twilio REST.
type TwilioVoiceProvider struct {
	HTTP *http.Client
}

func NewTwilioVoiceProvider() *TwilioVoiceProvider {
	return &TwilioVoiceProvider{HTTP: &http.Client{Timeout: 20 * time.Second}}
}

func (p *TwilioVoiceProvider) PlaceCall(ctx context.Context, req OutboundCallRequest) (*OutboundCallResult, error) {
	if req.AccountSID == "" || req.AuthToken == "" {
		return nil, fmt.Errorf("twilio credentials incomplete")
	}
	form := url.Values{}
	form.Set("To", normalizePhone(req.To))
	form.Set("From", normalizePhone(req.From))
	if req.StatusCallbackURL != "" {
		form.Set("StatusCallback", req.StatusCallbackURL)
		form.Set("StatusCallbackEvent", "initiated ringing answered completed")
	}
	if req.TwimlURL != "" {
		form.Set("Url", req.TwimlURL)
	} else {
		// Minimal TwiML via Twilio TwimlBins-style inline: say greeting then hang up
		form.Set("Twiml", `<Response><Say>Connecting your CRM call.</Say><Pause length="2"/></Response>`)
	}
	endpoint := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Calls.json", req.AccountSID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	httpReq.SetBasicAuth(req.AccountSID, req.AuthToken)
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := p.HTTP.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	rawBytes, _ := io.ReadAll(res.Body)
	var raw map[string]any
	_ = json.Unmarshal(rawBytes, &raw)
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("twilio call failed (%d): %s", res.StatusCode, truncate(string(rawBytes), 400))
	}
	sid, _ := raw["sid"].(string)
	status, _ := raw["status"].(string)
	if sid == "" {
		sid = "CA" + strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	return &OutboundCallResult{CallSID: sid, Status: status, Raw: raw}, nil
}

func (p *TwilioVoiceProvider) ValidateSignature(authToken, fullURL string, form url.Values, signatureHeader string) bool {
	if authToken == "" {
		return false
	}
	var b strings.Builder
	b.WriteString(fullURL)
	keys := make([]string, 0, len(form))
	for k := range form {
		keys = append(keys, k)
	}
	// Twilio requires sorted keys
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString(form.Get(k))
	}
	mac := hmac.New(sha1.New, []byte(authToken))
	_, _ = mac.Write([]byte(b.String()))
	expected := mac.Sum(nil)
	got, err := decodeTwilioSig(signatureHeader)
	if err != nil {
		return false
	}
	return hmac.Equal(expected, got)
}

func (p *TwilioVoiceProvider) ParseStatusCallback(form url.Values) (*CallStatusEvent, error) {
	sid := form.Get("CallSid")
	if sid == "" {
		return nil, fmt.Errorf("missing CallSid")
	}
	status := form.Get("CallStatus")
	var duration *int
	if d := form.Get("CallDuration"); d != "" {
		if n, err := strconv.Atoi(d); err == nil {
			duration = &n
		}
	}
	raw := map[string]any{}
	for k, vals := range form {
		if len(vals) > 0 {
			raw[k] = vals[0]
		}
	}
	return &CallStatusEvent{
		CallSID: sid, AccountSID: form.Get("AccountSid"),
		From: form.Get("From"), To: form.Get("To"),
		CallStatus: status, Direction: form.Get("Direction"),
		Duration: duration,
		RecordingSID: form.Get("RecordingSid"), RecordingURL: form.Get("RecordingUrl"),
		Timestamp: time.Now().UTC(),
		EventKey:  "twilio-call:" + sid + ":" + status,
		Raw:       raw,
	}, nil
}

// DevTwilioProvider simulates calls without live Twilio credentials.
type DevTwilioProvider struct{}

func (p *DevTwilioProvider) PlaceCall(ctx context.Context, req OutboundCallRequest) (*OutboundCallResult, error) {
	_ = ctx
	sid := "CA" + strings.ReplaceAll(uuid.NewString(), "-", "")
	return &OutboundCallResult{
		CallSID: sid, Status: "queued",
		Raw: map[string]any{"simulated": true, "to": req.To, "from": req.From},
	}, nil
}
func (p *DevTwilioProvider) ValidateSignature(authToken, fullURL string, form url.Values, signatureHeader string) bool {
	if authToken == "" {
		return true
	}
	return (&TwilioVoiceProvider{}).ValidateSignature(authToken, fullURL, form, signatureHeader)
}
func (p *DevTwilioProvider) ParseStatusCallback(form url.Values) (*CallStatusEvent, error) {
	return (&TwilioVoiceProvider{}).ParseStatusCallback(form)
}

func normalizePhone(v string) string {
	v = strings.TrimSpace(v)
	v = strings.ReplaceAll(v, " ", "")
	v = strings.ReplaceAll(v, "-", "")
	if v != "" && !strings.HasPrefix(v, "+") && strings.HasPrefix(v, "0") == false {
		// keep as-is; Meta/Twilio expect E.164 when possible
	}
	return v
}

func parseUnix(s string) time.Time {
	if s == "" {
		return time.Now().UTC()
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Now().UTC()
	}
	return time.Unix(n, 0).UTC()
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func coalesce(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func decodeTwilioSig(sig string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(sig)
}

package communications

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/crm/backend/internal/audit"
	"github.com/crm/backend/internal/timeline"
	"github.com/crm/backend/pkg/apperrors"
)

type Service struct {
	repo     *Repository
	timeline *timeline.Service
	audit    *audit.Service
	whatsapp WhatsAppProvider
	twilio   TwilioProvider
	publicBaseURL string
}

func NewService(
	repo *Repository,
	timelineSvc *timeline.Service,
	auditSvc *audit.Service,
	wa WhatsAppProvider,
	tw TwilioProvider,
	publicBaseURL string,
) *Service {
	if wa == nil {
		wa = &DevWhatsAppProvider{}
	}
	if tw == nil {
		tw = &DevTwilioProvider{}
	}
	return &Service{
		repo: repo, timeline: timelineSvc, audit: auditSvc,
		whatsapp: wa, twilio: tw, publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
	}
}

func (s *Service) ListAccounts(ctx context.Context, provider string) ([]Account, error) {
	items, err := s.repo.ListAccounts(ctx, provider, true)
	if err != nil {
		return nil, apperrors.Internal("failed to list communication accounts", err)
	}
	out := make([]Account, 0, len(items))
	for _, a := range items {
		out = append(out, a.PublicDTO())
	}
	return out, nil
}

func (s *Service) CreateAccount(ctx context.Context, actorID string, in CreateAccountInput, ip, ua string) (*Account, error) {
	if in.Provider != ProviderMetaWhatsApp && in.Provider != ProviderTwilioVoice {
		return nil, apperrors.Validation("provider must be meta_whatsapp or twilio_voice")
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, apperrors.Validation("name is required")
	}
	a, err := s.repo.CreateAccount(ctx, in)
	if err != nil {
		return nil, apperrors.Internal("failed to create account", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "communications.account_created", "communication_account", audit.Ptr(a.ID), map[string]any{
		"provider": a.Provider, "name": a.Name,
	}, ip, ua)
	dto := a.PublicDTO()
	return &dto, nil
}

func (s *Service) ListWhatsApp(ctx context.Context, customerID, dealID, leadID string, limit, offset int) ([]WhatsAppMessage, int, error) {
	if customerID == "" && dealID == "" && leadID == "" {
		return nil, 0, apperrors.Validation("customerId, dealID, or leadId is required")
	}
	return s.repo.ListWhatsAppMessages(ctx, customerID, dealID, leadID, limit, offset)
}

func (s *Service) ListCalls(ctx context.Context, customerID, dealID, leadID string, limit, offset int) ([]TwilioCall, int, error) {
	if customerID == "" && dealID == "" && leadID == "" {
		return nil, 0, apperrors.Validation("customerId, dealID, or leadId is required")
	}
	return s.repo.ListCalls(ctx, customerID, dealID, leadID, limit, offset)
}

func (s *Service) SendWhatsApp(ctx context.Context, actorID string, in SendWhatsAppInput, ip, ua string) (*WhatsAppMessage, error) {
	if strings.TrimSpace(in.Body) == "" && strings.TrimSpace(in.MediaURL) == "" {
		return nil, apperrors.Validation("body or mediaUrl is required")
	}
	leadID, customerID, dealID, err := s.normalizeLinks(ctx, in.LeadID, in.CustomerID, in.DealID)
	if err != nil {
		return nil, err
	}
	acct, err := s.resolveWhatsAppAccount(ctx, in.AccountID)
	if err != nil {
		return nil, err
	}
	to := strings.TrimSpace(in.To)
	if to == "" {
		to, err = s.repo.LoadEntityPhone(ctx, deref(customerID), deref(leadID), deref(dealID))
		if err != nil {
			return nil, apperrors.Internal("failed to resolve phone", err)
		}
	}
	if strings.TrimSpace(to) == "" {
		return nil, apperrors.Validation("recipient phone is required")
	}

	provider := s.pickWhatsAppProvider(acct)
	result, err := provider.Send(ctx, OutboundMessageRequest{
		ToPhoneNumberID: acct.ExternalAccountID,
		To:              to,
		Body:            in.Body,
		MediaURL:        in.MediaURL,
		AccessToken:     acct.Credentials["access_token"],
		GraphAPIVersion: coalesce(acct.Credentials["graph_api_version"], "v21.0"),
	})
	if err != nil {
		return nil, apperrors.Internal("whatsapp send failed", err)
	}

	msg := &WhatsAppMessage{
		AccountID: acct.ID, Direction: DirectionOutbound, Status: "sent",
		ProviderMessageID: result.ProviderMessageID,
		ConversationKey:   conversationKey(to),
		FromNumber:        acct.DisplayIdentifier, ToNumber: normalizePhone(to),
		Body: in.Body, MediaURL: in.MediaURL,
		LeadID: leadID, CustomerID: customerID, DealID: dealID,
		ActorUserID: audit.Ptr(actorID),
		OccurredAt:  time.Now().UTC().Format(time.RFC3339),
	}
	saved, err := s.repo.InsertWhatsAppMessage(ctx, msg, result.Raw)
	if err != nil {
		return nil, apperrors.Internal("failed to store whatsapp message", err)
	}
	ev, _, err := s.timeline.RecordIdempotent(ctx, timeline.WriteInput{
		EventType: timeline.EventWhatsApp, Title: "WhatsApp message",
		Body: truncate(in.Body, 500), Source: "whatsapp",
		ExternalID: result.ProviderMessageID, ExternalProvider: ProviderMetaWhatsApp,
		ActorUserID: audit.Ptr(actorID), LeadID: leadID, CustomerID: customerID, DealID: dealID,
		Metadata: map[string]any{
			"direction": DirectionOutbound, "status": "sent", "accountId": acct.ID,
			"to": normalizePhone(to), "messageId": saved.ID,
		},
	})
	if err == nil && ev != nil {
		saved.TimelineEventID = &ev.ID
		_, _ = s.repo.UpdateWhatsAppStatus(ctx, acct.ID, result.ProviderMessageID, "sent", "", "")
		_ = s.linkWhatsAppTimeline(ctx, saved.ID, ev.ID)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "whatsapp.message_sent", "whatsapp_message", audit.Ptr(saved.ID), map[string]any{
		"to": to, "accountId": acct.ID,
	}, ip, ua)
	return saved, nil
}

func (s *Service) PlaceCall(ctx context.Context, actorID string, in PlaceCallInput, ip, ua string) (*TwilioCall, error) {
	leadID, customerID, dealID, err := s.normalizeLinks(ctx, in.LeadID, in.CustomerID, in.DealID)
	if err != nil {
		return nil, err
	}
	acct, err := s.resolveTwilioAccount(ctx, in.AccountID)
	if err != nil {
		return nil, err
	}
	to := strings.TrimSpace(in.To)
	if to == "" {
		to, err = s.repo.LoadEntityPhone(ctx, deref(customerID), deref(leadID), deref(dealID))
		if err != nil {
			return nil, apperrors.Internal("failed to resolve phone", err)
		}
	}
	if strings.TrimSpace(to) == "" {
		return nil, apperrors.Validation("destination phone is required")
	}

	callback := s.publicBaseURL + "/api/v1/webhooks/twilio/voice/status"
	provider := s.pickTwilioProvider(acct)
	result, err := provider.PlaceCall(ctx, OutboundCallRequest{
		AccountSID:        acct.Credentials["account_sid"],
		AuthToken:         acct.Credentials["auth_token"],
		From:              coalesce(acct.DisplayIdentifier, acct.Credentials["from_number"]),
		To:                to,
		StatusCallbackURL: callback,
	})
	if err != nil {
		return nil, apperrors.Internal("twilio call failed", err)
	}

	call := &TwilioCall{
		AccountID: acct.ID, Direction: DirectionOutbound, Status: coalesce(result.Status, "queued"),
		ProviderCallSID: result.CallSID,
		FromNumber:      coalesce(acct.DisplayIdentifier, acct.Credentials["from_number"]),
		ToNumber:        normalizePhone(to),
		LeadID:          leadID, CustomerID: customerID, DealID: dealID,
		ActorUserID: audit.Ptr(actorID),
	}
	saved, err := s.repo.InsertCall(ctx, call, result.Raw)
	if err != nil {
		return nil, apperrors.Internal("failed to store call", err)
	}
	ev, _, err := s.timeline.RecordIdempotent(ctx, timeline.WriteInput{
		EventType: timeline.EventCall, Title: "Call",
		Body: fmt.Sprintf("Outbound call to %s", normalizePhone(to)),
		Source: "twilio", ExternalID: result.CallSID, ExternalProvider: ProviderTwilioVoice,
		ActorUserID: audit.Ptr(actorID), LeadID: leadID, CustomerID: customerID, DealID: dealID,
		Metadata: map[string]any{
			"direction": DirectionOutbound, "status": saved.Status, "accountId": acct.ID, "callId": saved.ID,
		},
	})
	if err == nil && ev != nil {
		_ = s.linkCallTimeline(ctx, saved.ID, ev.ID)
		saved.TimelineEventID = &ev.ID
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "twilio.call_placed", "twilio_call", audit.Ptr(saved.ID), map[string]any{
		"to": to, "accountId": acct.ID,
	}, ip, ua)
	return saved, nil
}

// --- Webhooks ---

func (s *Service) VerifyMetaWhatsApp(ctx context.Context, mode, token, challenge string) (string, bool, error) {
	accounts, err := s.repo.ListAccounts(ctx, ProviderMetaWhatsApp, true)
	if err != nil {
		return "", false, err
	}
	for _, a := range accounts {
		verify := a.Credentials["verify_token"]
		if out, ok := s.pickWhatsAppProvider(&a).VerifyWebhook(mode, token, challenge, verify); ok {
			return out, true, nil
		}
	}
	return "", false, nil
}

func (s *Service) HandleMetaWhatsAppWebhook(r *http.Request, body []byte) error {
	accounts, err := s.repo.ListAccounts(r.Context(), ProviderMetaWhatsApp, true)
	if err != nil {
		return apperrors.Internal("failed to load accounts", err)
	}
	if len(accounts) == 0 {
		return apperrors.Validation("no whatsapp accounts configured")
	}
	// Validate signature against any account app_secret (multi-account)
	sig := r.Header.Get("X-Hub-Signature-256")
	valid := false
	var parser WhatsAppProvider = s.whatsapp
	for i := range accounts {
		a := &accounts[i]
		secret := a.Credentials["app_secret"]
		p := s.pickWhatsAppProvider(a)
		if p.ValidateSignature(secret, body, sig) {
			valid = true
			parser = p
			break
		}
	}
	if !valid && !allDevAccounts(accounts) {
		return apperrors.Forbidden("invalid Meta webhook signature")
	}
	events, err := parser.ParseWebhook(body)
	if err != nil {
		return apperrors.Validation("invalid webhook payload")
	}
	for _, ev := range events {
		claimed, err := s.repo.ClaimWebhook(r.Context(), ProviderMetaWhatsApp, ev.EventKey, ev.Raw)
		if err != nil {
			return apperrors.Internal("webhook idempotency failed", err)
		}
		if !claimed {
			continue // duplicate retry
		}
		if err := s.applyWhatsAppEvent(r.Context(), ev); err != nil {
			_ = s.repo.CompleteWebhook(r.Context(), ProviderMetaWhatsApp, ev.EventKey, "failed", err.Error(), 200)
			continue
		}
		_ = s.repo.CompleteWebhook(r.Context(), ProviderMetaWhatsApp, ev.EventKey, "processed", "", 200)
	}
	return nil
}

func (s *Service) HandleTwilioStatus(r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return apperrors.Validation("invalid form")
	}
	accounts, err := s.repo.ListAccounts(r.Context(), ProviderTwilioVoice, true)
	if err != nil {
		return apperrors.Internal("failed to load accounts", err)
	}
	fullURL := s.publicBaseURL + r.URL.Path
	if r.URL.RawQuery != "" {
		fullURL += "?" + r.URL.RawQuery
	}
	sig := r.Header.Get("X-Twilio-Signature")
	valid := false
	var provider TwilioProvider = s.twilio
	for i := range accounts {
		a := &accounts[i]
		p := s.pickTwilioProvider(a)
		token := a.Credentials["auth_token"]
		if p.ValidateSignature(token, fullURL, r.PostForm, sig) {
			valid = true
			provider = p
			break
		}
	}
	if !valid && !allDevTwilio(accounts) {
		return apperrors.Forbidden("invalid Twilio signature")
	}
	ev, err := provider.ParseStatusCallback(r.PostForm)
	if err != nil {
		return apperrors.Validation(err.Error())
	}
	claimed, err := s.repo.ClaimWebhook(r.Context(), ProviderTwilioVoice, ev.EventKey, ev.Raw)
	if err != nil {
		return apperrors.Internal("webhook idempotency failed", err)
	}
	if !claimed {
		return nil
	}
	if err := s.applyCallStatus(r.Context(), ev); err != nil {
		_ = s.repo.CompleteWebhook(r.Context(), ProviderTwilioVoice, ev.EventKey, "failed", err.Error(), 200)
		return nil
	}
	_ = s.repo.CompleteWebhook(r.Context(), ProviderTwilioVoice, ev.EventKey, "processed", "", 200)
	return nil
}

func (s *Service) applyWhatsAppEvent(ctx context.Context, ev InboundWhatsAppEvent) error {
	acct, err := s.repo.FindAccountByExternal(ctx, ProviderMetaWhatsApp, ev.PhoneNumberID)
	if err != nil {
		return err
	}
	if acct == nil {
		// fall back to first active WhatsApp account
		list, _ := s.repo.ListAccounts(ctx, ProviderMetaWhatsApp, true)
		if len(list) == 0 {
			return fmt.Errorf("no matching whatsapp account for phone_number_id %s", ev.PhoneNumberID)
		}
		acct = &list[0]
	}

	if ev.Kind == "status" {
		msg, err := s.repo.UpdateWhatsAppStatus(ctx, acct.ID, ev.ProviderMessageID, mapWAStatus(ev.Status), "", "")
		if err != nil {
			return err
		}
		if msg != nil {
			_, _, _ = s.timeline.RecordIdempotent(ctx, timeline.WriteInput{
				EventType: timeline.EventWhatsApp, Title: "WhatsApp " + ev.Status,
				Body: fmt.Sprintf("Message %s", ev.Status), Source: "whatsapp",
				ExternalID: ev.ProviderMessageID + ":" + ev.Status, ExternalProvider: ProviderMetaWhatsApp,
				LeadID: msg.LeadID, CustomerID: msg.CustomerID, DealID: msg.DealID,
				Metadata: map[string]any{"direction": msg.Direction, "status": ev.Status, "messageId": msg.ID},
			})
		}
		return nil
	}

	customerID, leadID, _ := s.repo.ResolvePhoneContext(ctx, ev.From)
	if customerID == nil && leadID == nil {
		// Cannot attach to CRM entity — still claim receipt but skip persistence requiring entity.
		return fmt.Errorf("no CRM contact matched phone %s", ev.From)
	}
	msg := &WhatsAppMessage{
		AccountID: acct.ID, Direction: DirectionInbound, Status: "received",
		ProviderMessageID: ev.ProviderMessageID, ConversationKey: conversationKey(ev.From),
		FromNumber: normalizePhone(ev.From), ToNumber: coalesce(ev.To, acct.DisplayIdentifier),
		Body: ev.Body, MediaURL: ev.MediaURL, MediaMime: ev.MediaMime, MediaFilename: ev.MediaFilename,
		CustomerID: customerID, LeadID: leadID,
		OccurredAt: ev.Timestamp.UTC().Format(time.RFC3339),
	}
	saved, err := s.repo.InsertWhatsAppMessage(ctx, msg, ev.Raw)
	if err != nil {
		return err
	}
	title := "WhatsApp message"
	if ev.MediaURL != "" {
		title = "WhatsApp attachment"
	}
	tev, _, err := s.timeline.RecordIdempotent(ctx, timeline.WriteInput{
		EventType: timeline.EventWhatsApp, Title: title, Body: truncate(ev.Body, 500),
		Source: "whatsapp", ExternalID: ev.ProviderMessageID, ExternalProvider: ProviderMetaWhatsApp,
		OccurredAt: &ev.Timestamp, LeadID: leadID, CustomerID: customerID,
		Metadata: map[string]any{
			"direction": DirectionInbound, "status": "received", "accountId": acct.ID,
			"from": normalizePhone(ev.From), "messageId": saved.ID,
			"hasAttachment": ev.MediaURL != "",
		},
	})
	if err == nil && tev != nil {
		_ = s.linkWhatsAppTimeline(ctx, saved.ID, tev.ID)
	}
	return nil
}

func (s *Service) applyCallStatus(ctx context.Context, ev *CallStatusEvent) error {
	call, err := s.repo.UpdateCallStatus(ctx, ev.CallSID, mapTwilioStatus(ev.CallStatus), ev.Duration, ev.RecordingSID, ev.RecordingURL)
	if err != nil {
		return err
	}
	if call == nil {
		// Inbound call not initiated from CRM — try to attach by phone
		customerID, leadID, _ := s.repo.ResolvePhoneContext(ctx, ev.From)
		if customerID == nil && leadID == nil {
			customerID, leadID, _ = s.repo.ResolvePhoneContext(ctx, ev.To)
		}
		if customerID == nil && leadID == nil {
			return fmt.Errorf("unknown call sid %s", ev.CallSID)
		}
		accounts, _ := s.repo.ListAccounts(ctx, ProviderTwilioVoice, true)
		if len(accounts) == 0 {
			return fmt.Errorf("no twilio accounts")
		}
		acct := accounts[0]
		for _, a := range accounts {
			if a.Credentials["account_sid"] == ev.AccountSID {
				acct = a
				break
			}
		}
		dir := DirectionInbound
		if strings.Contains(strings.ToLower(ev.Direction), "outbound") {
			dir = DirectionOutbound
		}
		created, err := s.repo.InsertCall(ctx, &TwilioCall{
			AccountID: acct.ID, Direction: dir, Status: mapTwilioStatus(ev.CallStatus),
			ProviderCallSID: ev.CallSID, FromNumber: ev.From, ToNumber: ev.To,
			DurationSeconds: ev.Duration, RecordingSID: ev.RecordingSID, RecordingURL: ev.RecordingURL,
			CustomerID: customerID, LeadID: leadID,
		}, ev.Raw)
		if err != nil {
			return err
		}
		call = created
	}

	recordingMeta := map[string]any{}
	if call.RecordingURL != "" {
		// Only surface recording refs when account allows
		acct, _ := s.repo.GetAccount(ctx, call.AccountID)
		if acct != nil && acct.AllowRecordings {
			recordingMeta["recordingSid"] = call.RecordingSID
			recordingMeta["recordingUrl"] = call.RecordingURL
		}
	}
	body := fmt.Sprintf("Call %s", call.Status)
	if call.DurationSeconds != nil {
		body = fmt.Sprintf("Call %s - %ds", call.Status, *call.DurationSeconds)
	}
	_, _, _ = s.timeline.RecordIdempotent(ctx, timeline.WriteInput{
		EventType: timeline.EventCall, Title: "Call update", Body: body,
		Source: "twilio", ExternalID: ev.CallSID + ":" + ev.CallStatus, ExternalProvider: ProviderTwilioVoice,
		LeadID: call.LeadID, CustomerID: call.CustomerID, DealID: call.DealID,
		Metadata: mergeMaps(map[string]any{
			"direction": call.Direction, "status": call.Status, "callId": call.ID,
			"durationSeconds": call.DurationSeconds,
		}, recordingMeta),
	})
	return nil
}

func (s *Service) normalizeLinks(ctx context.Context, leadID, customerID, dealID *string) (*string, *string, *string, error) {
	l, c, d := leadID, customerID, dealID
	if d != nil && strings.TrimSpace(*d) != "" && (c == nil || *c == "") {
		cid, err := s.repo.DealCustomerID(ctx, *d)
		if err != nil {
			return nil, nil, nil, apperrors.NotFound("deal not found")
		}
		c = &cid
	}
	if (l == nil || *l == "") && (c == nil || *c == "") && (d == nil || *d == "") {
		return nil, nil, nil, apperrors.Validation("customerId, leadId, or dealId is required")
	}
	return l, c, d, nil
}

func (s *Service) resolveWhatsAppAccount(ctx context.Context, id string) (*Account, error) {
	if id != "" {
		a, err := s.repo.GetAccount(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) || a == nil {
			return nil, apperrors.NotFound("whatsapp account not found")
		}
		if err != nil {
			return nil, apperrors.Internal("failed to load account", err)
		}
		if a.Provider != ProviderMetaWhatsApp || !a.IsActive {
			return nil, apperrors.Validation("account is not an active WhatsApp account")
		}
		return a, nil
	}
	list, err := s.repo.ListAccounts(ctx, ProviderMetaWhatsApp, true)
	if err != nil {
		return nil, apperrors.Internal("failed to list accounts", err)
	}
	if len(list) == 0 {
		return nil, apperrors.Validation("no WhatsApp accounts configured — add one under communications")
	}
	return &list[0], nil
}

func (s *Service) resolveTwilioAccount(ctx context.Context, id string) (*Account, error) {
	if id != "" {
		a, err := s.repo.GetAccount(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) || a == nil {
			return nil, apperrors.NotFound("twilio account not found")
		}
		if err != nil {
			return nil, apperrors.Internal("failed to load account", err)
		}
		if a.Provider != ProviderTwilioVoice || !a.IsActive {
			return nil, apperrors.Validation("account is not an active Twilio voice account")
		}
		return a, nil
	}
	list, err := s.repo.ListAccounts(ctx, ProviderTwilioVoice, true)
	if err != nil {
		return nil, apperrors.Internal("failed to list accounts", err)
	}
	if len(list) == 0 {
		return nil, apperrors.Validation("no Twilio accounts configured — add one under communications")
	}
	return &list[0], nil
}

func (s *Service) pickWhatsAppProvider(a *Account) WhatsAppProvider {
	if a != nil && a.Credentials["access_token"] != "" && a.ExternalAccountID != "" {
		if _, ok := s.whatsapp.(*MetaWhatsAppProvider); ok {
			return s.whatsapp
		}
		return NewMetaWhatsAppProvider()
	}
	return &DevWhatsAppProvider{}
}

func (s *Service) pickTwilioProvider(a *Account) TwilioProvider {
	if a != nil && a.Credentials["account_sid"] != "" && a.Credentials["auth_token"] != "" {
		if _, ok := s.twilio.(*TwilioVoiceProvider); ok {
			return s.twilio
		}
		return NewTwilioVoiceProvider()
	}
	return &DevTwilioProvider{}
}

func (s *Service) linkWhatsAppTimeline(ctx context.Context, messageID, eventID string) error {
	_, err := s.repo.pool.Exec(ctx, `UPDATE whatsapp_messages SET timeline_event_id=$2 WHERE id=$1`, messageID, eventID)
	return err
}

func (s *Service) linkCallTimeline(ctx context.Context, callID, eventID string) error {
	_, err := s.repo.pool.Exec(ctx, `UPDATE twilio_calls SET timeline_event_id=$2 WHERE id=$1`, callID, eventID)
	return err
}

func conversationKey(phone string) string {
	return digitsOnly(normalizePhone(phone))
}

func mapWAStatus(s string) string {
	switch strings.ToLower(s) {
	case "sent", "delivered", "read", "failed":
		return strings.ToLower(s)
	default:
		return s
	}
}

func mapTwilioStatus(s string) string {
	s = strings.ToLower(s)
	switch s {
	case "queued", "initiated", "ringing", "in-progress", "completed", "busy", "failed", "no-answer", "canceled":
		return s
	default:
		return s
	}
}

func allDevAccounts(accounts []Account) bool {
	for _, a := range accounts {
		if a.Credentials["app_secret"] != "" {
			return false
		}
	}
	return true
}

func allDevTwilio(accounts []Account) bool {
	for _, a := range accounts {
		if a.Credentials["auth_token"] != "" {
			return false
		}
	}
	return true
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func mergeMaps(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
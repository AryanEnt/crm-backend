package email

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/crm/backend/internal/audit"
	"github.com/crm/backend/internal/auth"
	"github.com/crm/backend/internal/config"
	"github.com/crm/backend/internal/permissions"
	"github.com/crm/backend/internal/timeline"
	"github.com/crm/backend/pkg/apperrors"
)

type Service struct {
	repo     *Repository
	box      *TokenBox
	gmail    *GmailClient
	cfg      *config.Config
	audit    *audit.Service
	timeline *timeline.Service
}

func NewService(repo *Repository, cfg *config.Config, auditSvc *audit.Service, tl *timeline.Service) *Service {
	encKey := cfg.TokenEncryptionKey
	if encKey == "" {
		encKey = cfg.JWTSecret
	}
	redirect := strings.TrimRight(cfg.PublicAPIURL, "/") + "/api/v1/email/google/callback"
	return &Service{
		repo:     repo,
		box:      NewTokenBox(encKey),
		gmail:    NewGmailClient(cfg.GoogleClientID, cfg.GoogleClientSecret, redirect),
		cfg:      cfg,
		audit:    auditSvc,
		timeline: tl,
	}
}

func (s *Service) Configured() bool {
	return strings.TrimSpace(s.cfg.GoogleClientID) != "" && strings.TrimSpace(s.cfg.GoogleClientSecret) != ""
}

func (s *Service) Health(ctx context.Context) IntegrationHealth {
	h := IntegrationHealth{
		OAuthConfigured:  s.Configured(),
		GmailAPIEnabled:  s.Configured(),
		PubSubConfigured: strings.TrimSpace(s.cfg.GmailPubSubTopic) != "",
	}
	h.Configured = h.OAuthConfigured
	if !h.Configured {
		h.Message = "Gmail integration is not configured"
		return h
	}
	connected, attention, err := s.repo.HealthCounts(ctx)
	if err != nil {
		slog.Error("email health counts", "error", err)
	}
	h.ConnectedAccounts = connected
	h.NeedsAttention = attention
	h.PushSyncHealthy = h.PubSubConfigured && attention == 0
	return h
}

func (s *Service) ListAccounts(ctx context.Context, userID string) ([]AccountPublic, error) {
	return s.repo.ListAccountsForUser(ctx, userID)
}

func (s *Service) StartConnect(ctx context.Context, claims auth.Claims, redirectTo string) (string, error) {
	if !s.Configured() {
		return "", apperrors.Validation("Gmail integration is not configured")
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	state := base64.RawURLEncoding.EncodeToString(buf)
	if redirectTo == "" {
		redirectTo = strings.TrimRight(s.cfg.FrontendURL, "/") + "/settings/email"
	}
	if err := s.repo.InsertOAuthState(ctx, state, claims.UserID, redirectTo, time.Now().Add(10*time.Minute)); err != nil {
		return "", err
	}
	return s.gmail.AuthURL(state), nil
}

func (s *Service) OAuthCallback(ctx context.Context, code, state, errorCode string) (string, error) {
	front := strings.TrimRight(s.cfg.FrontendURL, "/") + "/settings/email"
	if errorCode != "" {
		return front + "?gmail=denied", nil
	}
	if code == "" || state == "" {
		return front + "?gmail=invalid", apperrors.Validation("missing oauth parameters")
	}
	userID, redirect, err := s.repo.ConsumeOAuthState(ctx, state)
	if err != nil {
		return front + "?gmail=invalid", apperrors.Unauthorized("invalid or expired OAuth state")
	}
	if redirect != "" {
		front = redirect
	}
	tok, err := s.gmail.ExchangeCode(ctx, code)
	if err != nil {
		slog.Error("gmail oauth exchange failed", "error", err)
		return front + "?gmail=error", apperrors.Validation("Could not complete Gmail authorization")
	}
	prof, err := s.gmail.UserInfo(ctx, tok.AccessToken)
	if err != nil {
		slog.Error("gmail userinfo failed", "error", err)
		return front + "?gmail=error", err
	}
	gp, err := s.gmail.GmailProfile(ctx, tok.AccessToken)
	if err != nil {
		slog.Error("gmail profile failed", "error", err)
		return front + "?gmail=error", err
	}
	emailAddr := strings.ToLower(strings.TrimSpace(gp.EmailAddress))
	if emailAddr == "" {
		emailAddr = strings.ToLower(strings.TrimSpace(prof.Email))
	}
	encAccess, err := s.box.Encrypt(tok.AccessToken)
	if err != nil {
		return front + "?gmail=error", err
	}
	encRefresh, err := s.box.Encrypt(tok.RefreshToken)
	if err != nil {
		return front + "?gmail=error", err
	}
	exp := time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	hist := fmt.Sprintf("%d", gp.HistoryID)
	rec := accountRecord{
		AccountPublic: AccountPublic{
			EmailAddress:     emailAddr,
			DisplayName:      prof.Name,
			AvatarURL:        prof.Picture,
			ConnectionStatus: "connected",
		},
		UserID:                userID,
		EncryptedAccessToken:  encAccess,
		EncryptedRefreshToken: encRefresh,
		TokenExpiry:           &exp,
		GmailHistoryID:        hist,
	}
	saved, err := s.repo.UpsertAccount(ctx, rec)
	if err != nil {
		slog.Error("gmail account upsert failed", "error", err)
		return front + "?gmail=error", err
	}
	_ = s.audit.Record(ctx, &userID, "gmail.connected", "email_account", &saved.ID, map[string]any{
		"email": emailAddr,
	}, "", "")
	if err := s.ensureWatch(ctx, saved); err != nil {
		slog.Warn("gmail watch after connect failed", "error", err, "account", saved.ID)
		_ = s.repo.UpdateSync(ctx, saved.ID, "error", hist, "Watch setup failed. Sync will retry.")
	} else {
		go func() {
			bg, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			if err := s.initialSync(bg, saved.ID); err != nil {
				slog.Warn("gmail initial sync failed", "error", err, "account", saved.ID)
			}
		}()
	}
	sep := "?"
	if strings.Contains(front, "?") {
		sep = "&"
	}
	return front + sep + "gmail=connected", nil
}

func (s *Service) Disconnect(ctx context.Context, claims auth.Claims, accountID, ip, ua string) error {
	acc, err := s.requireOwnAccount(ctx, claims, accountID)
	if err != nil {
		return err
	}
	token, err := s.accessToken(ctx, acc)
	if err == nil && token != "" {
		s.gmail.StopWatch(ctx, token)
	}
	if err := s.repo.Disconnect(ctx, acc.ID); err != nil {
		return err
	}
	return s.audit.Record(ctx, &claims.UserID, "gmail.disconnected", "email_account", &acc.ID, map[string]any{
		"email": acc.EmailAddress,
	}, ip, ua)
}

func (s *Service) requireOwnAccount(ctx context.Context, claims auth.Claims, accountID string) (*accountRecord, error) {
	acc, err := s.repo.GetAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if acc == nil || acc.ConnectionStatus == "disconnected" {
		return nil, apperrors.NotFound("email account not found")
	}
	if acc.UserID != claims.UserID && claims.RoleCode != permissions.RoleSuperAdmin {
		return nil, apperrors.Forbidden("cannot manage another user's Gmail account")
	}
	return acc, nil
}

func (s *Service) accessToken(ctx context.Context, acc *accountRecord) (string, error) {
	refresh, err := s.box.Decrypt(acc.EncryptedRefreshToken)
	if err != nil {
		return "", err
	}
	access, err := s.box.Decrypt(acc.EncryptedAccessToken)
	if err != nil {
		return "", err
	}
	needsRefresh := acc.TokenExpiry == nil || time.Until(*acc.TokenExpiry) < 2*time.Minute
	if !needsRefresh && access != "" {
		return access, nil
	}
	if refresh == "" {
		_ = s.repo.UpdateSync(ctx, acc.ID, "needs_reauth", "", "Reconnect Gmail")
		return "", apperrors.Unauthorized("Gmail connection needs attention")
	}
	tok, err := s.gmail.Refresh(ctx, refresh)
	if err != nil {
		slog.Error("gmail token refresh failed", "account", acc.ID, "error", err)
		_ = s.repo.UpdateSync(ctx, acc.ID, "needs_reauth", "", "Reconnect Gmail")
		_ = s.audit.Record(ctx, &acc.UserID, "gmail.authorization_revoked", "email_account", &acc.ID, map[string]any{
			"email": acc.EmailAddress,
		}, "", "")
		return "", apperrors.Unauthorized("Gmail connection needs attention")
	}
	encA, _ := s.box.Encrypt(tok.AccessToken)
	encR := ""
	if tok.RefreshToken != "" {
		encR, _ = s.box.Encrypt(tok.RefreshToken)
	}
	exp := time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	if err := s.repo.UpdateTokens(ctx, acc.ID, encA, encR, &exp); err != nil {
		return "", err
	}
	acc.EncryptedAccessToken = encA
	acc.TokenExpiry = &exp
	return tok.AccessToken, nil
}

func (s *Service) ensureWatch(ctx context.Context, acc *accountRecord) error {
	if strings.TrimSpace(s.cfg.GmailPubSubTopic) == "" {
		return nil
	}
	token, err := s.accessToken(ctx, acc)
	if err != nil {
		return err
	}
	w, err := s.gmail.Watch(ctx, token, s.cfg.GmailPubSubTopic)
	if err != nil {
		return err
	}
	var exp *time.Time
	if w.Expiration != "" {
		if ms, e := parseInt64(w.Expiration); e == nil && ms > 0 {
			t := time.UnixMilli(ms)
			exp = &t
		}
	}
	hist := strings.TrimSpace(fmt.Sprintf("%d", w.HistoryID))
	if hist == "0" {
		hist = acc.GmailHistoryID
	}
	return s.repo.UpdateWatch(ctx, acc.ID, hist, exp)
}

func (s *Service) HandlePubSub(ctx context.Context, raw []byte) error {
	var envelope struct {
		Message struct {
			Data      string `json:"data"`
			MessageID string `json:"messageId"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil
	}
	if envelope.Message.Data == "" {
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(envelope.Message.Data)
	if err != nil {
		return nil
	}
	var n struct {
		EmailAddress string `json:"emailAddress"`
		HistoryID    uint64 `json:"historyId"`
	}
	if err := json.Unmarshal(decoded, &n); err != nil {
		return nil
	}
	acc, err := s.repo.GetAccountByEmail(ctx, n.EmailAddress)
	if err != nil || acc == nil {
		return err
	}
	return s.syncAccount(ctx, acc)
}

func (s *Service) SyncAccount(ctx context.Context, claims auth.Claims, accountID string) error {
	acc, err := s.requireOwnAccount(ctx, claims, accountID)
	if err != nil {
		return err
	}
	return s.syncAccount(ctx, acc)
}

func (s *Service) initialSync(ctx context.Context, accountID string) error {
	acc, err := s.repo.GetAccount(ctx, accountID)
	if err != nil || acc == nil {
		return err
	}
	token, err := s.accessToken(ctx, acc)
	if err != nil {
		return err
	}
	refs, err := s.gmail.ListRecent(ctx, token, 40)
	if err != nil {
		_ = s.repo.UpdateSync(ctx, acc.ID, "error", "", "Sync problem")
		return err
	}
	for _, ref := range refs {
		if err := s.ingestMessage(ctx, acc, token, ref.ID); err != nil {
			slog.Warn("gmail ingest failed", "id", ref.ID, "error", err)
		}
	}
	return s.repo.UpdateSync(ctx, acc.ID, "connected", acc.GmailHistoryID, "")
}

func (s *Service) syncAccount(ctx context.Context, acc *accountRecord) error {
	_ = s.repo.UpdateSync(ctx, acc.ID, "syncing", acc.GmailHistoryID, "")
	token, err := s.accessToken(ctx, acc)
	if err != nil {
		return err
	}
	if acc.GmailHistoryID == "" {
		if err := s.initialSync(ctx, acc.ID); err != nil {
			return err
		}
		return nil
	}
	hist, err := s.gmail.History(ctx, token, acc.GmailHistoryID)
	if err != nil {
		if apiErr, ok := err.(*apiError); ok && apiErr.Status == 404 {
			return s.initialSync(ctx, acc.ID)
		}
		if apiErr, ok := err.(*apiError); ok && apiErr.Revoked {
			_ = s.repo.UpdateSync(ctx, acc.ID, "needs_reauth", "", "Reconnect Gmail")
			return nil
		}
		_ = s.repo.UpdateSync(ctx, acc.ID, "error", "", "Sync problem")
		_ = s.audit.Record(ctx, &acc.UserID, "gmail.sync_failed", "email_account", &acc.ID, map[string]any{}, "", "")
		return err
	}
	seen := map[string]struct{}{}
	for _, h := range hist.History {
		for _, added := range h.MessagesAdded {
			id := added.Message.ID
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			if err := s.ingestMessage(ctx, acc, token, id); err != nil {
				slog.Warn("gmail history ingest failed", "id", id, "error", err)
			}
		}
	}
	newHist := acc.GmailHistoryID
	if hist.HistoryID > 0 {
		newHist = fmt.Sprintf("%d", hist.HistoryID)
	}
	if err := s.repo.UpdateSync(ctx, acc.ID, "connected", newHist, ""); err != nil {
		return err
	}
	_ = s.audit.Record(ctx, &acc.UserID, "gmail.sync_restored", "email_account", &acc.ID, map[string]any{}, "", "")
	return nil
}

func (s *Service) ingestMessage(ctx context.Context, acc *accountRecord, token, providerMessageID string) error {
	exists, err := s.repo.HasProviderMessage(ctx, acc.ID, providerMessageID)
	if err != nil || exists {
		return err
	}
	gm, err := s.gmail.GetMessage(ctx, token, providerMessageID)
	if err != nil {
		return err
	}
	fromRaw := headerValue(gm.Payload.Headers, "From")
	fromName, fromAddr := parseAddress(fromRaw)
	to := parseAddressList(headerValue(gm.Payload.Headers, "To"))
	cc := parseAddressList(headerValue(gm.Payload.Headers, "Cc"))
	subj := headerValue(gm.Payload.Headers, "Subject")
	html, text, atts := collectBodies(gm.Payload)
	if text == "" {
		text = stripTags(html)
	}
	snippet := gm.Snippet
	if snippet == "" {
		snippet = truncate(text, 180)
	}
	direction := "inbound"
	status := "received"
	if strings.EqualFold(fromAddr, acc.EmailAddress) {
		direction = "outbound"
		status = "sent"
	}
	when := time.Now().UTC()
	if gm.InternalDate != "" {
		if ms, e := parseInt64(gm.InternalDate); e == nil {
			when = time.UnixMilli(ms).UTC()
		}
	}
	thread, err := s.repo.FindThreadByProvider(ctx, acc.ID, gm.ThreadID)
	if err != nil {
		return err
	}
	participants := uniqueEmails(append(append([]string{fromAddr}, to...), cc...))
	matchStatus := "unmatched"
	var leadID, customerID *string
	if thread != nil {
		leadID = thread.LeadID
		customerID = thread.CustomerID
		if thread.MatchStatus == "matched" {
			matchStatus = "matched"
		}
	}
	if matchStatus != "matched" {
		addrs := participants
		hit, err := s.repo.MatchAddresses(ctx, addrs)
		if err != nil {
			return err
		}
		switch {
		case hit.Count == 0:
			matchStatus = "unmatched"
		case hit.Count == 1 && (hit.LeadID != nil || hit.CustomerID != nil):
			matchStatus = "matched"
			leadID = hit.LeadID
			customerID = hit.CustomerID
		default:
			matchStatus = "needs_association"
			if thread == nil || thread.LeadID == nil {
				leadID = hit.LeadID
			}
			if thread == nil || thread.CustomerID == nil {
				customerID = hit.CustomerID
			}
		}
	}
	if thread == nil {
		prov := gm.ThreadID
		if prov == "" {
			prov = "local-" + uuid.NewString()
		}
		t := Thread{
			AccountID:          acc.ID,
			OwnerUserID:        acc.UserID,
			ProviderThreadID:   prov,
			LeadID:             leadID,
			CustomerID:         customerID,
			Participants:       participants,
			Subject:            subj,
			LastMessageAt:      &when,
			LastMessagePreview: snippet,
			MessageCount:       1,
			MatchStatus:        matchStatus,
		}
		if direction == "inbound" {
			t.UnreadCount = 1
		}
		thread, err = s.repo.UpsertThread(ctx, t)
		if err != nil {
			return err
		}
	} else {
		thread.Participants = uniqueEmails(append(thread.Participants, participants...))
		thread.LastMessageAt = &when
		thread.LastMessagePreview = snippet
		thread.MatchStatus = matchStatus
		if leadID != nil {
			thread.LeadID = leadID
		}
		if customerID != nil {
			thread.CustomerID = customerID
		}
		if direction == "inbound" {
			thread.UnreadCount++
		}
		thread, err = s.repo.UpsertThread(ctx, *thread)
		if err != nil {
			return err
		}
	}
	msg := Message{
		ThreadID:          thread.ID,
		AccountID:         acc.ID,
		OwnerUserID:       acc.UserID,
		ProviderMessageID: gm.ID,
		ProviderThreadID:  gm.ThreadID,
		Direction:         direction,
		FromAddress:       fromAddr,
		FromName:          fromName,
		ToAddresses:       to,
		CcAddresses:       cc,
		Subject:           subj,
		BodyHTML:          html,
		BodyText:          text,
		Snippet:           snippet,
		Status:            status,
		HasAttachments:    len(atts) > 0,
	}
	if direction == "outbound" {
		msg.SentAt = &when
	} else {
		msg.ReceivedAt = &when
	}
	saved, err := s.repo.InsertMessage(ctx, msg)
	if err != nil {
		if strings.Contains(err.Error(), "email_messages_provider_id_unique") {
			return nil
		}
		return err
	}
	for _, a := range atts {
		_ = s.repo.InsertAttachment(ctx, Attachment{
			MessageID:            saved.ID,
			Filename:             a.Filename,
			MimeType:             a.MimeType,
			SizeBytes:            a.Size,
			ProviderAttachmentID: a.ProviderID,
		})
	}
	_ = s.repo.RefreshThreadStats(ctx, thread.ID)
	s.writeTimeline(ctx, acc.UserID, thread, saved)
	return nil
}

func (s *Service) writeTimeline(ctx context.Context, actorID string, thread *Thread, msg *Message) {
	if thread == nil || (thread.LeadID == nil && thread.CustomerID == nil) {
		return
	}
	title := "Email received"
	if msg.Direction == "outbound" {
		title = "Email sent"
	}
	extID := msg.ProviderMessageID
	if extID == "" {
		extID = msg.ID
	}
	_, _, err := s.timeline.RecordIdempotent(ctx, timeline.WriteInput{
		EventType:        timeline.EventEmail,
		Title:            title,
		Body:             msg.Subject,
		ActorUserID:      &actorID,
		Source:           "gmail",
		ExternalID:       extID,
		ExternalProvider: "gmail",
		LeadID:           thread.LeadID,
		CustomerID:       thread.CustomerID,
		DealID:           thread.DealID,
		Metadata: map[string]any{
			"threadId":  thread.ID,
			"messageId": msg.ID,
			"direction": msg.Direction,
		},
	})
	if err != nil {
		slog.Warn("email timeline write failed", "error", err)
	}
}

func (s *Service) ListThreads(ctx context.Context, claims auth.Claims, f ThreadListFilter) ([]Thread, int, FolderCounts, error) {
	items, total, err := s.repo.ListThreads(ctx, claims, f)
	if err != nil {
		return nil, 0, FolderCounts{}, err
	}
	counts, err := s.repo.FolderCounts(ctx, claims, f.OwnerUserID)
	if err != nil {
		return nil, 0, FolderCounts{}, err
	}
	return items, total, counts, nil
}

func (s *Service) GetThread(ctx context.Context, claims auth.Claims, id string) (*ThreadDetail, error) {
	t, err := s.repo.GetThread(ctx, id)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, apperrors.NotFound("thread not found")
	}
	if err := s.assertThreadVisible(claims, t); err != nil {
		return nil, err
	}
	msgs, err := s.repo.ListMessages(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	return &ThreadDetail{Thread: t, Messages: msgs}, nil
}

func (s *Service) assertThreadVisible(claims auth.Claims, t *Thread) error {
	if t.OwnerUserID == claims.UserID {
		return nil
	}
	scope := permissions.ScopeFor(claims.PermissionScopes, permissions.EmailView)
	if scope == permissions.ScopeOrganization {
		return nil
	}
	if scope == permissions.ScopeTeam {
		for _, id := range claims.TeamMemberUserIDs {
			if id == t.OwnerUserID {
				return nil
			}
		}
	}
	return apperrors.Forbidden("thread is outside your scope")
}

func (s *Service) Associate(ctx context.Context, claims auth.Claims, threadID string, in AssociateInput) (*Thread, error) {
	t, err := s.repo.GetThread(ctx, threadID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, apperrors.NotFound("thread not found")
	}
	if err := s.assertThreadVisible(claims, t); err != nil {
		return nil, err
	}
	match := "unmatched"
	if (in.LeadID != nil && *in.LeadID != "") || (in.CustomerID != nil && *in.CustomerID != "") {
		match = "matched"
	}
	if err := s.repo.AssociateThread(ctx, threadID, in.LeadID, in.CustomerID, in.DealID, match); err != nil {
		return nil, err
	}
	return s.repo.GetThread(ctx, threadID)
}

func (s *Service) PatchThread(ctx context.Context, claims auth.Claims, id string, starred, archived *bool) (*Thread, error) {
	t, err := s.repo.GetThread(ctx, id)
	if err != nil || t == nil {
		if err == nil {
			return nil, apperrors.NotFound("thread not found")
		}
		return nil, err
	}
	if err := s.assertThreadVisible(claims, t); err != nil {
		return nil, err
	}
	if err := s.repo.SetThreadFlags(ctx, id, starred, archived); err != nil {
		return nil, err
	}
	return s.repo.GetThread(ctx, id)
}

func (s *Service) Compose(ctx context.Context, claims auth.Claims, in ComposeInput, ip, ua string) (*Message, error) {
	acc, err := s.accountForCompose(ctx, claims, in.AccountID)
	if err != nil {
		return nil, err
	}
	preview, err := s.Preview(ctx, in.Subject, in.BodyHTML, in.BodyText, in.LeadID, in.CustomerID)
	if err != nil {
		return nil, err
	}
	if in.Send && !preview.CanSend {
		return nil, apperrors.Validation("Email still contains unresolved variables")
	}
	to := cleanAddrs(in.To)
	if in.Send && len(to) == 0 {
		return nil, apperrors.Validation("Add at least one recipient")
	}
	thread, err := s.threadForCompose(ctx, acc, in, to)
	if err != nil {
		return nil, err
	}
	status := "draft"
	if in.ScheduledAt != nil && strings.TrimSpace(*in.ScheduledAt) != "" {
		status = "scheduled"
	} else if in.Send {
		status = "sending"
	}
	snippet := truncate(stripTags(preview.BodyHTML), 180)
	if snippet == "" {
		snippet = truncate(preview.BodyText, 180)
	}
	msg := Message{
		ThreadID:     thread.ID,
		AccountID:    acc.ID,
		OwnerUserID:  acc.UserID,
		Direction:    "outbound",
		FromAddress:  acc.EmailAddress,
		FromName:     acc.DisplayName,
		ToAddresses:  to,
		CcAddresses:  cleanAddrs(in.Cc),
		BccAddresses: cleanAddrs(in.Bcc),
		Subject:      preview.Subject,
		BodyHTML:     preview.BodyHTML,
		BodyText:     preview.BodyText,
		Snippet:      snippet,
		Status:       status,
		InReplyTo:    in.InReplyTo,
	}
	if status == "scheduled" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(*in.ScheduledAt))
		if err != nil {
			return nil, apperrors.Validation("Invalid schedule time")
		}
		msg.ScheduledAt = &t
	}
	saved, err := s.repo.InsertMessage(ctx, msg)
	if err != nil {
		return nil, err
	}
	_ = s.repo.RefreshThreadStats(ctx, thread.ID)
	if status == "sending" {
		return s.sendMessage(ctx, claims, acc, thread, saved, ip, ua)
	}
	return saved, nil
}

func (s *Service) accountForCompose(ctx context.Context, claims auth.Claims, accountID string) (*accountRecord, error) {
	if accountID != "" {
		return s.requireOwnAccount(ctx, claims, accountID)
	}
	acc, err := s.repo.GetAccountByUser(ctx, claims.UserID)
	if err != nil {
		return nil, err
	}
	if acc == nil {
		return nil, apperrors.Validation("Connect your Gmail account first")
	}
	if acc.ConnectionStatus == "needs_reauth" {
		return nil, apperrors.Unauthorized("Gmail connection needs attention")
	}
	return acc, nil
}

func (s *Service) threadForCompose(ctx context.Context, acc *accountRecord, in ComposeInput, to []string) (*Thread, error) {
	if in.ThreadID != nil && *in.ThreadID != "" {
		t, err := s.repo.GetThread(ctx, *in.ThreadID)
		if err != nil {
			return nil, err
		}
		if t == nil {
			return nil, apperrors.NotFound("thread not found")
		}
		return t, nil
	}
	now := time.Now().UTC()
	t := Thread{
		AccountID:          acc.ID,
		OwnerUserID:        acc.UserID,
		ProviderThreadID:   "local-" + uuid.NewString(),
		LeadID:             in.LeadID,
		CustomerID:         in.CustomerID,
		DealID:             in.DealID,
		Participants:       uniqueEmails(append([]string{acc.EmailAddress}, to...)),
		Subject:            in.Subject,
		LastMessageAt:      &now,
		LastMessagePreview: truncate(stripTags(in.BodyHTML), 180),
		MessageCount:       0,
		MatchStatus:        "unmatched",
	}
	if in.LeadID != nil || in.CustomerID != nil {
		t.MatchStatus = "matched"
	}
	return s.repo.UpsertThread(ctx, t)
}

func (s *Service) PatchDraft(ctx context.Context, claims auth.Claims, id string, in DraftPatch) (*Message, error) {
	m, err := s.repo.GetMessage(ctx, id)
	if err != nil {
		return nil, err
	}
	if m == nil || m.Status != "draft" {
		return nil, apperrors.NotFound("draft not found")
	}
	if m.OwnerUserID != claims.UserID {
		return nil, apperrors.Forbidden("cannot edit this draft")
	}
	if in.To != nil {
		m.ToAddresses = cleanAddrs(in.To)
	}
	if in.Cc != nil {
		m.CcAddresses = cleanAddrs(in.Cc)
	}
	if in.Bcc != nil {
		m.BccAddresses = cleanAddrs(in.Bcc)
	}
	if in.Subject != nil {
		m.Subject = *in.Subject
	}
	if in.BodyHTML != nil {
		m.BodyHTML = *in.BodyHTML
	}
	if in.BodyText != nil {
		m.BodyText = *in.BodyText
	}
	m.Snippet = truncate(stripTags(m.BodyHTML), 180)
	if err := s.repo.UpdateMessage(ctx, *m); err != nil {
		return nil, err
	}
	return s.repo.GetMessage(ctx, id)
}

func (s *Service) SendExisting(ctx context.Context, claims auth.Claims, id, ip, ua string) (*Message, error) {
	m, err := s.repo.GetMessage(ctx, id)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, apperrors.NotFound("message not found")
	}
	if m.OwnerUserID != claims.UserID {
		return nil, apperrors.Forbidden("cannot send this message")
	}
	acc, err := s.requireOwnAccount(ctx, claims, m.AccountID)
	if err != nil {
		return nil, err
	}
	thread, err := s.repo.GetThread(ctx, m.ThreadID)
	if err != nil {
		return nil, err
	}
	preview, err := s.Preview(ctx, m.Subject, m.BodyHTML, m.BodyText, thread.LeadID, thread.CustomerID)
	if err != nil {
		return nil, err
	}
	if !preview.CanSend {
		return nil, apperrors.Validation("Email still contains unresolved variables")
	}
	m.Subject = preview.Subject
	m.BodyHTML = preview.BodyHTML
	m.BodyText = preview.BodyText
	return s.sendMessage(ctx, claims, acc, thread, m, ip, ua)
}

func (s *Service) sendMessage(ctx context.Context, claims auth.Claims, acc *accountRecord, thread *Thread, m *Message, ip, ua string) (*Message, error) {
	if len(m.ToAddresses) == 0 {
		return nil, apperrors.Validation("Add at least one recipient")
	}
	token, err := s.accessToken(ctx, acc)
	if err != nil {
		m.Status = "failed"
		_ = s.repo.UpdateMessage(ctx, *m)
		_ = s.audit.Record(ctx, &claims.UserID, "email.send_failed", "email_message", &m.ID, map[string]any{
			"reason": "reauth",
		}, ip, ua)
		return m, err
	}
	raw := buildRFC822(acc.DisplayName, acc.EmailAddress, m)
	threadID := thread.ProviderThreadID
	if strings.HasPrefix(threadID, "local-") {
		threadID = ""
	}
	sent, err := s.gmail.Send(ctx, token, raw, threadID)
	if err != nil {
		slog.Error("gmail send failed", "account", acc.ID, "error", err)
		m.Status = "failed"
		_ = s.repo.UpdateMessage(ctx, *m)
		_ = s.audit.Record(ctx, &claims.UserID, "email.send_failed", "email_message", &m.ID, map[string]any{}, ip, ua)
		return m, apperrors.Validation("Failed to send. Check your Gmail connection and try again.")
	}
	now := time.Now().UTC()
	m.Status = "sent"
	m.SentAt = &now
	m.ProviderMessageID = sent.ID
	m.ProviderThreadID = sent.ThreadID
	if err := s.repo.UpdateMessage(ctx, *m); err != nil {
		return nil, err
	}
	thread.ProviderThreadID = sent.ThreadID
	thread.LastMessageAt = &now
	thread.LastMessagePreview = m.Snippet
	if thread.MatchStatus != "matched" {
		hit, _ := s.repo.MatchAddresses(ctx, append(m.ToAddresses, m.CcAddresses...))
		if hit.Count == 1 {
			thread.LeadID = hit.LeadID
			thread.CustomerID = hit.CustomerID
			thread.MatchStatus = "matched"
		} else if hit.Count > 1 {
			thread.MatchStatus = "needs_association"
		}
	}
	_, _ = s.repo.UpsertThread(ctx, *thread)
	_ = s.repo.RefreshThreadStats(ctx, thread.ID)
	saved, _ := s.repo.GetMessage(ctx, m.ID)
	s.writeTimeline(ctx, claims.UserID, thread, saved)
	_ = s.audit.Record(ctx, &claims.UserID, "email.sent", "email_message", &m.ID, map[string]any{
		"to": m.ToAddresses,
	}, ip, ua)
	return saved, nil
}

func (s *Service) Preview(ctx context.Context, subject, html, text string, leadID, customerID *string) (*PreviewResult, error) {
	vars, err := s.repo.PersonalizationContext(ctx, leadID, customerID)
	if err != nil {
		return nil, err
	}
	out := &PreviewResult{
		Subject:  applyVars(subject, vars),
		BodyHTML: applyVars(html, vars),
		BodyText: applyVars(text, vars),
	}
	out.Unresolved = findUnresolved(out.Subject + " " + out.BodyHTML + " " + out.BodyText)
	out.CanSend = len(out.Unresolved) == 0
	return out, nil
}

func (s *Service) ListTemplates(ctx context.Context, claims auth.Claims) ([]Template, error) {
	ids := claims.TeamIDs
	if ids == nil {
		ids = []string{}
	}
	return s.repo.ListTemplates(ctx, ids)
}

func (s *Service) CreateTemplate(ctx context.Context, claims auth.Claims, in TemplateInput) (*Template, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, apperrors.Validation("Template name is required")
	}
	active := true
	if in.IsActive != nil {
		active = *in.IsActive
	}
	cat := in.Category
	if cat == "" {
		cat = "general"
	}
	return s.repo.InsertTemplate(ctx, Template{
		Name:      strings.TrimSpace(in.Name),
		Subject:   in.Subject,
		BodyHTML:  in.BodyHTML,
		Category:  cat,
		Variables: in.Variables,
		IsActive:  active,
		CreatedBy: &claims.UserID,
		TeamID:    in.TeamID,
	})
}

func (s *Service) UpdateTemplate(ctx context.Context, id string, in TemplateInput) (*Template, error) {
	t, err := s.repo.GetTemplate(ctx, id)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, apperrors.NotFound("template not found")
	}
	if strings.TrimSpace(in.Name) != "" {
		t.Name = strings.TrimSpace(in.Name)
	}
	if in.Subject != "" {
		t.Subject = in.Subject
	}
	if in.BodyHTML != "" {
		t.BodyHTML = in.BodyHTML
	}
	if in.Category != "" {
		t.Category = in.Category
	}
	if in.Variables != nil {
		t.Variables = in.Variables
	}
	if in.IsActive != nil {
		t.IsActive = *in.IsActive
	}
	if in.TeamID != nil {
		t.TeamID = in.TeamID
	}
	return s.repo.UpdateTemplate(ctx, *t)
}

func (s *Service) DeleteTemplate(ctx context.Context, id string) error {
	return s.repo.DeleteTemplate(ctx, id)
}

func (s *Service) RenewWatches(ctx context.Context) {
	accs, err := s.repo.ListAccountsNeedingWatch(ctx)
	if err != nil {
		slog.Error("list watches failed", "error", err)
		return
	}
	for i := range accs {
		if err := s.ensureWatch(ctx, &accs[i]); err != nil {
			slog.Warn("renew gmail watch failed", "account", accs[i].ID, "error", err)
		}
	}
}

func (s *Service) SendDueScheduled(ctx context.Context) {
	due, err := s.repo.ListScheduledDue(ctx, time.Now().UTC())
	if err != nil {
		slog.Error("list scheduled email failed", "error", err)
		return
	}
	for i := range due {
		m := due[i]
		acc, err := s.repo.GetAccount(ctx, m.AccountID)
		if err != nil || acc == nil {
			continue
		}
		thread, err := s.repo.GetThread(ctx, m.ThreadID)
		if err != nil || thread == nil {
			continue
		}
		claims := auth.Claims{UserID: m.OwnerUserID}
		_, _ = s.sendMessage(ctx, claims, acc, thread, &m, "", "scheduler")
	}
}

var varPattern = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_]+)\s*\}\}`)

func applyVars(s string, vars map[string]string) string {
	return varPattern.ReplaceAllStringFunc(s, func(m string) string {
		sub := varPattern.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		if v, ok := vars[sub[1]]; ok && v != "" {
			return v
		}
		return m
	})
}

func findUnresolved(s string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, m := range varPattern.FindAllStringSubmatch(s, -1) {
		if len(m) < 2 {
			continue
		}
		if _, ok := seen[m[1]]; ok {
			continue
		}
		seen[m[1]] = struct{}{}
		out = append(out, m[1])
	}
	return out
}

func parseAddress(raw string) (name, addr string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	a, err := mail.ParseAddress(raw)
	if err != nil {
		return "", strings.ToLower(raw)
	}
	return a.Name, strings.ToLower(a.Address)
}

func parseAddressList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	list, err := mail.ParseAddressList(raw)
	if err != nil {
		parts := strings.Split(raw, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			_, a := parseAddress(p)
			if a != "" {
				out = append(out, a)
			}
		}
		return out
	}
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, strings.ToLower(a.Address))
	}
	return out
}

func uniqueEmails(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, e := range in {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		if _, ok := seen[e]; ok {
			continue
		}
		seen[e] = struct{}{}
		out = append(out, e)
	}
	return out
}

func cleanAddrs(in []string) []string {
	return uniqueEmails(in)
}

func stripTags(html string) string {
	re := regexp.MustCompile(`<[^>]+>`)
	return strings.TrimSpace(re.ReplaceAllString(html, " "))
}

func buildRFC822(fromName, fromAddr string, m *Message) string {
	var b strings.Builder
	from := fromAddr
	if fromName != "" {
		from = fmt.Sprintf("%s <%s>", fromName, fromAddr)
	}
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(m.ToAddresses, ", "))
	if len(m.CcAddresses) > 0 {
		fmt.Fprintf(&b, "Cc: %s\r\n", strings.Join(m.CcAddresses, ", "))
	}
	if len(m.BccAddresses) > 0 {
		fmt.Fprintf(&b, "Bcc: %s\r\n", strings.Join(m.BccAddresses, ", "))
	}
	fmt.Fprintf(&b, "Subject: %s\r\n", m.Subject)
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\n")
	if m.BodyHTML != "" {
		fmt.Fprintf(&b, "Content-Type: text/html; charset=UTF-8\r\n\r\n")
		b.WriteString(m.BodyHTML)
	} else {
		fmt.Fprintf(&b, "Content-Type: text/plain; charset=UTF-8\r\n\r\n")
		b.WriteString(m.BodyText)
	}
	return b.String()
}

func parseInt64(s string) (int64, error) {
	var n int64
	_, err := fmt.Sscan(s, &n)
	return n, err
}

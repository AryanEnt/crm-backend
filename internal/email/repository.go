package email

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/crm/backend/internal/auth"
	"github.com/crm/backend/internal/datascope"
	"github.com/crm/backend/internal/permissions"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) InsertOAuthState(ctx context.Context, state, userID, redirect string, exp time.Time) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO email_oauth_states (state, user_id, redirect_to, expires_at)
		VALUES ($1,$2,$3,$4)
	`, state, userID, redirect, exp)
	return err
}

func (r *Repository) ConsumeOAuthState(ctx context.Context, state string) (userID, redirect string, err error) {
	err = r.pool.QueryRow(ctx, `
		DELETE FROM email_oauth_states
		WHERE state=$1 AND expires_at > NOW()
		RETURNING user_id::text, redirect_to
	`, state).Scan(&userID, &redirect)
	if err == pgx.ErrNoRows {
		return "", "", fmt.Errorf("invalid or expired oauth state")
	}
	return userID, redirect, err
}

func (r *Repository) UpsertAccount(ctx context.Context, rec accountRecord) (*accountRecord, error) {
	id := rec.AccountPublic.ID
	if id == "" {
		id = uuid.NewString()
	}
	err := r.pool.QueryRow(ctx, `
		INSERT INTO email_accounts (
			id, user_id, provider, email_address, display_name, avatar_url,
			encrypted_access_token, encrypted_refresh_token, token_expiry,
			gmail_history_id, watch_expiration, connection_status,
			sending_enabled, receiving_enabled, last_sync_at, last_sync_error
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT (user_id, email_address) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			avatar_url = EXCLUDED.avatar_url,
			encrypted_access_token = EXCLUDED.encrypted_access_token,
			encrypted_refresh_token = CASE
				WHEN EXCLUDED.encrypted_refresh_token = '' THEN email_accounts.encrypted_refresh_token
				ELSE EXCLUDED.encrypted_refresh_token
			END,
			token_expiry = EXCLUDED.token_expiry,
			gmail_history_id = CASE
				WHEN EXCLUDED.gmail_history_id = '' THEN email_accounts.gmail_history_id
				ELSE EXCLUDED.gmail_history_id
			END,
			watch_expiration = COALESCE(EXCLUDED.watch_expiration, email_accounts.watch_expiration),
			connection_status = EXCLUDED.connection_status,
			sending_enabled = EXCLUDED.sending_enabled,
			receiving_enabled = EXCLUDED.receiving_enabled,
			last_sync_error = '',
			updated_at = NOW()
		RETURNING id::text
	`, id, rec.UserID, "gmail", rec.EmailAddress, rec.DisplayName, rec.AvatarURL,
		rec.EncryptedAccessToken, rec.EncryptedRefreshToken, rec.TokenExpiry,
		rec.GmailHistoryID, rec.WatchExpiration, rec.ConnectionStatus,
		true, true, rec.LastSyncAt, "").Scan(&id)
	if err != nil {
		return nil, err
	}
	return r.GetAccount(ctx, id)
}

func (r *Repository) GetAccount(ctx context.Context, id string) (*accountRecord, error) {
	row := r.pool.QueryRow(ctx, accountSelect+" WHERE a.id=$1", id)
	return scanAccount(row)
}

func (r *Repository) GetAccountByUser(ctx context.Context, userID string) (*accountRecord, error) {
	row := r.pool.QueryRow(ctx, accountSelect+`
		WHERE a.user_id=$1 AND a.connection_status <> 'disconnected'
		ORDER BY a.updated_at DESC LIMIT 1
	`, userID)
	return scanAccount(row)
}

func (r *Repository) GetAccountByEmail(ctx context.Context, email string) (*accountRecord, error) {
	row := r.pool.QueryRow(ctx, accountSelect+`
		WHERE lower(a.email_address)=lower($1) AND a.connection_status <> 'disconnected'
		LIMIT 1
	`, email)
	return scanAccount(row)
}

func (r *Repository) ListAccountsForUser(ctx context.Context, userID string) ([]AccountPublic, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT a.id::text, a.provider, a.email_address, a.display_name, a.avatar_url,
			a.connection_status, a.sending_enabled, a.receiving_enabled,
			a.last_sync_at, a.last_sync_error, a.created_at
		FROM email_accounts a
		WHERE a.user_id=$1 AND a.connection_status <> 'disconnected'
		ORDER BY a.created_at
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccountPublic
	for rows.Next() {
		var a AccountPublic
		if err := rows.Scan(&a.ID, &a.Provider, &a.EmailAddress, &a.DisplayName, &a.AvatarURL,
			&a.ConnectionStatus, &a.SendingEnabled, &a.ReceivingEnabled,
			&a.LastSyncAt, &a.LastSyncError, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if out == nil {
		out = []AccountPublic{}
	}
	return out, rows.Err()
}

func (r *Repository) UpdateTokens(ctx context.Context, id, access, refresh string, expiry *time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE email_accounts SET
			encrypted_access_token=$2,
			encrypted_refresh_token=CASE WHEN $3='' THEN encrypted_refresh_token ELSE $3 END,
			token_expiry=$4,
			updated_at=NOW()
		WHERE id=$1
	`, id, access, refresh, expiry)
	return err
}

func (r *Repository) UpdateWatch(ctx context.Context, id, historyID string, exp *time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE email_accounts SET gmail_history_id=$2, watch_expiration=$3, updated_at=NOW()
		WHERE id=$1
	`, id, historyID, exp)
	return err
}

func (r *Repository) UpdateSync(ctx context.Context, id, status, historyID, syncErr string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE email_accounts SET
			connection_status=$2,
			gmail_history_id=CASE WHEN $3='' THEN gmail_history_id ELSE $3 END,
			last_sync_at=NOW(),
			last_sync_error=$4,
			updated_at=NOW()
		WHERE id=$1
	`, id, status, historyID, syncErr)
	return err
}

func (r *Repository) Disconnect(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE email_accounts SET
			connection_status='disconnected',
			encrypted_access_token='',
			encrypted_refresh_token='',
			token_expiry=NULL,
			updated_at=NOW()
		WHERE id=$1
	`, id)
	return err
}

func (r *Repository) ListAccountsNeedingWatch(ctx context.Context) ([]accountRecord, error) {
	rows, err := r.pool.Query(ctx, accountSelect+`
		WHERE a.connection_status IN ('connected','syncing','error')
		  AND (a.watch_expiration IS NULL OR a.watch_expiration < NOW() + INTERVAL '24 hours')
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAccounts(rows)
}

func (r *Repository) ListScheduledDue(ctx context.Context, now time.Time) ([]Message, error) {
	rows, err := r.pool.Query(ctx, messageSelect+`
		WHERE m.status='scheduled' AND m.scheduled_at IS NOT NULL AND m.scheduled_at <= $1
	`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (r *Repository) HealthCounts(ctx context.Context) (connected, attention int, err error) {
	err = r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE connection_status IN ('connected','syncing')),
			COUNT(*) FILTER (WHERE connection_status IN ('needs_reauth','error'))
		FROM email_accounts
		WHERE connection_status <> 'disconnected'
	`).Scan(&connected, &attention)
	return
}

const accountSelect = `
	SELECT a.id::text, a.user_id::text, a.provider, a.email_address, a.display_name, a.avatar_url,
		a.encrypted_access_token, a.encrypted_refresh_token, a.token_expiry,
		a.gmail_history_id, a.watch_expiration, a.connection_status,
		a.sending_enabled, a.receiving_enabled, a.last_sync_at, a.last_sync_error, a.created_at
	FROM email_accounts a
`

func scanAccount(row pgx.Row) (*accountRecord, error) {
	var a accountRecord
	err := row.Scan(&a.ID, &a.UserID, &a.Provider, &a.EmailAddress, &a.DisplayName, &a.AvatarURL,
		&a.EncryptedAccessToken, &a.EncryptedRefreshToken, &a.TokenExpiry,
		&a.GmailHistoryID, &a.WatchExpiration, &a.ConnectionStatus,
		&a.SendingEnabled, &a.ReceivingEnabled, &a.LastSyncAt, &a.LastSyncError, &a.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func scanAccounts(rows pgx.Rows) ([]accountRecord, error) {
	var out []accountRecord
	for rows.Next() {
		var a accountRecord
		if err := rows.Scan(&a.ID, &a.UserID, &a.Provider, &a.EmailAddress, &a.DisplayName, &a.AvatarURL,
			&a.EncryptedAccessToken, &a.EncryptedRefreshToken, &a.TokenExpiry,
			&a.GmailHistoryID, &a.WatchExpiration, &a.ConnectionStatus,
			&a.SendingEnabled, &a.ReceivingEnabled, &a.LastSyncAt, &a.LastSyncError, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repository) FindThreadByProvider(ctx context.Context, accountID, providerThreadID string) (*Thread, error) {
	row := r.pool.QueryRow(ctx, threadSelect+" WHERE t.account_id=$1 AND t.provider_thread_id=$2", accountID, providerThreadID)
	return scanThread(row)
}

func (r *Repository) GetThread(ctx context.Context, id string) (*Thread, error) {
	row := r.pool.QueryRow(ctx, threadSelect+" WHERE t.id=$1", id)
	return scanThread(row)
}

func (r *Repository) UpsertThread(ctx context.Context, t Thread) (*Thread, error) {
	id := t.ID
	if id == "" {
		id = uuid.NewString()
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO email_threads (
			id, account_id, owner_user_id, provider_thread_id, lead_id, customer_id, deal_id,
			participants, subject, last_message_at, last_message_preview, message_count,
			unread_count, starred, archived, match_status
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT (account_id, provider_thread_id) DO UPDATE SET
			lead_id = COALESCE(email_threads.lead_id, EXCLUDED.lead_id),
			customer_id = COALESCE(email_threads.customer_id, EXCLUDED.customer_id),
			deal_id = COALESCE(email_threads.deal_id, EXCLUDED.deal_id),
			participants = EXCLUDED.participants,
			subject = CASE WHEN EXCLUDED.subject='' THEN email_threads.subject ELSE EXCLUDED.subject END,
			last_message_at = EXCLUDED.last_message_at,
			last_message_preview = EXCLUDED.last_message_preview,
			message_count = EXCLUDED.message_count,
			unread_count = EXCLUDED.unread_count,
			match_status = CASE
				WHEN email_threads.match_status='matched' THEN email_threads.match_status
				ELSE EXCLUDED.match_status
			END,
			updated_at=NOW()
	`, id, t.AccountID, t.OwnerUserID, t.ProviderThreadID, t.LeadID, t.CustomerID, t.DealID,
		t.Participants, t.Subject, t.LastMessageAt, t.LastMessagePreview, t.MessageCount,
		t.UnreadCount, t.Starred, t.Archived, t.MatchStatus)
	if err != nil {
		return nil, err
	}
	return r.FindThreadByProvider(ctx, t.AccountID, t.ProviderThreadID)
}

func (r *Repository) AssociateThread(ctx context.Context, id string, leadID, customerID, dealID *string, match string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE email_threads SET lead_id=$2, customer_id=$3, deal_id=$4, match_status=$5, updated_at=NOW()
		WHERE id=$1
	`, id, leadID, customerID, dealID, match)
	return err
}

func (r *Repository) SetThreadFlags(ctx context.Context, id string, starred, archived *bool) error {
	sets := []string{"updated_at=NOW()"}
	args := []any{id}
	n := 2
	if starred != nil {
		sets = append(sets, fmt.Sprintf("starred=$%d", n))
		args = append(args, *starred)
		n++
	}
	if archived != nil {
		sets = append(sets, fmt.Sprintf("archived=$%d", n))
		args = append(args, *archived)
	}
	_, err := r.pool.Exec(ctx, "UPDATE email_threads SET "+strings.Join(sets, ", ")+" WHERE id=$1", args...)
	return err
}

func (r *Repository) RefreshThreadStats(ctx context.Context, threadID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE email_threads t SET
			message_count = (SELECT COUNT(*) FROM email_messages m WHERE m.thread_id=t.id AND m.status <> 'draft'),
			last_message_at = (SELECT MAX(COALESCE(m.sent_at, m.received_at, m.created_at)) FROM email_messages m WHERE m.thread_id=t.id),
			last_message_preview = COALESCE((
				SELECT m.snippet FROM email_messages m WHERE m.thread_id=t.id
				ORDER BY COALESCE(m.sent_at, m.received_at, m.created_at) DESC LIMIT 1
			), t.last_message_preview),
			updated_at=NOW()
		WHERE t.id=$1
	`, threadID)
	return err
}

const threadSelect = `
	SELECT t.id::text, t.account_id::text, t.owner_user_id::text, COALESCE(u.full_name,''),
		t.provider_thread_id, t.lead_id::text, l.full_name, t.customer_id::text, c.full_name,
		t.deal_id::text, t.participants, t.subject, t.last_message_at, t.last_message_preview,
		t.message_count, t.unread_count, t.starred, t.archived, t.match_status,
		EXISTS(SELECT 1 FROM email_messages m WHERE m.thread_id=t.id AND m.has_attachments),
		t.created_at, t.updated_at
	FROM email_threads t
	LEFT JOIN users u ON u.id=t.owner_user_id
	LEFT JOIN leads l ON l.id=t.lead_id
	LEFT JOIN customers c ON c.id=t.customer_id
`

func scanThread(row interface{ Scan(dest ...any) error }) (*Thread, error) {
	var t Thread
	var leadID, leadName, custID, custName, dealID *string
	err := row.Scan(&t.ID, &t.AccountID, &t.OwnerUserID, &t.OwnerName, &t.ProviderThreadID,
		&leadID, &leadName, &custID, &custName, &dealID, &t.Participants, &t.Subject,
		&t.LastMessageAt, &t.LastMessagePreview, &t.MessageCount, &t.UnreadCount,
		&t.Starred, &t.Archived, &t.MatchStatus, &t.HasAttachments, &t.CreatedAt, &t.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.LeadID = nilIfEmpty(leadID)
	t.LeadName = leadName
	t.CustomerID = nilIfEmpty(custID)
	t.CustomerName = custName
	t.DealID = nilIfEmpty(dealID)
	if t.Participants == nil {
		t.Participants = []string{}
	}
	return &t, nil
}

func nilIfEmpty(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	return s
}

func (r *Repository) ListThreads(ctx context.Context, claims auth.Claims, f ThreadListFilter) ([]Thread, int, error) {
	vis, err := datascope.Resolve(claims, permissions.EmailView, f.OwnerUserID)
	if err != nil {
		return nil, 0, err
	}
	where := []string{"TRUE"}
	args := []any{}
	where, args = datascope.AppendWhere(where, args, vis, datascope.Columns{Owner: "t.owner_user_id"})
	ownOnly := len(vis.OwnerIDs) == 1 && vis.OwnerIDs[0] == claims.UserID && len(vis.TeamIDs) == 0 && !vis.Unscoped
	if !ownOnly {
		where = append(where, "(t.lead_id IS NOT NULL OR t.customer_id IS NOT NULL OR t.match_status = 'needs_association')")
	}

	switch f.Folder {
	case "sent":
		where = append(where, `EXISTS (SELECT 1 FROM email_messages m WHERE m.thread_id=t.id AND m.direction='outbound' AND m.status IN ('sent','delivered','sending'))`)
		where = append(where, "t.archived=FALSE")
	case "drafts":
		where = append(where, `EXISTS (SELECT 1 FROM email_messages m WHERE m.thread_id=t.id AND m.status='draft')`)
	case "starred":
		where = append(where, "t.starred=TRUE")
	case "unmatched":
		where = append(where, "t.match_status IN ('unmatched','needs_association')")
		where = append(where, "t.archived=FALSE")
	case "scheduled":
		where = append(where, `EXISTS (SELECT 1 FROM email_messages m WHERE m.thread_id=t.id AND m.status='scheduled')`)
	case "archived":
		where = append(where, "t.archived=TRUE")
	default: // inbox
		where = append(where, "t.archived=FALSE")
		where = append(where, "NOT EXISTS (SELECT 1 FROM email_messages m WHERE m.thread_id=t.id AND m.status='draft' AND NOT EXISTS (SELECT 1 FROM email_messages m2 WHERE m2.thread_id=t.id AND m2.status<>'draft'))")
	}
	if f.LeadID != "" {
		args = append(args, f.LeadID)
		where = append(where, fmt.Sprintf("t.lead_id=$%d", len(args)))
	}
	if f.CustomerID != "" {
		args = append(args, f.CustomerID)
		where = append(where, fmt.Sprintf("t.customer_id=$%d", len(args)))
	}
	if f.Q != "" {
		args = append(args, "%"+strings.ToLower(f.Q)+"%")
		n := len(args)
		where = append(where, fmt.Sprintf(`(
			lower(t.subject) LIKE $%d OR lower(t.last_message_preview) LIKE $%d
			OR EXISTS (SELECT 1 FROM unnest(t.participants) p WHERE lower(p) LIKE $%d)
			OR EXISTS (SELECT 1 FROM email_messages m WHERE m.thread_id=t.id AND (lower(m.body_text) LIKE $%d OR lower(m.subject) LIKE $%d))
		)`, n, n, n, n, n))
	}
	if f.Unread == "true" {
		where = append(where, "t.unread_count > 0")
	}
	if f.HasAttach == "true" {
		where = append(where, "EXISTS (SELECT 1 FROM email_messages m WHERE m.thread_id=t.id AND m.has_attachments)")
	}
	if f.Direction != "" {
		args = append(args, f.Direction)
		where = append(where, fmt.Sprintf(`EXISTS (SELECT 1 FROM email_messages m WHERE m.thread_id=t.id AND m.direction=$%d)`, len(args)))
	}
	if f.DateFrom != "" {
		args = append(args, f.DateFrom)
		where = append(where, fmt.Sprintf("t.last_message_at >= $%d::timestamptz", len(args)))
	}
	if f.DateTo != "" {
		args = append(args, f.DateTo)
		where = append(where, fmt.Sprintf("t.last_message_at < $%d::timestamptz", len(args)))
	}

	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 40
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	countSQL := `SELECT COUNT(*) FROM email_threads t WHERE ` + strings.Join(where, " AND ")
	var total int
	if err := r.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, offset)
	listSQL := threadSelect + " WHERE " + strings.Join(where, " AND ") +
		fmt.Sprintf(" ORDER BY t.last_message_at DESC NULLS LAST LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var items []Thread
	for rows.Next() {
		t, err := scanThread(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *t)
	}
	if items == nil {
		items = []Thread{}
	}
	return items, total, rows.Err()
}

func (r *Repository) FolderCounts(ctx context.Context, claims auth.Claims, ownerUserID string) (FolderCounts, error) {
	vis, err := datascope.Resolve(claims, permissions.EmailView, ownerUserID)
	if err != nil {
		return FolderCounts{}, err
	}
	where := []string{"TRUE"}
	args := []any{}
	where, args = datascope.AppendWhere(where, args, vis, datascope.Columns{Owner: "t.owner_user_id"})
	ownOnly := len(vis.OwnerIDs) == 1 && vis.OwnerIDs[0] == claims.UserID && len(vis.TeamIDs) == 0 && !vis.Unscoped
	if !ownOnly {
		where = append(where, "(t.lead_id IS NOT NULL OR t.customer_id IS NOT NULL OR t.match_status = 'needs_association')")
	}
	pred := strings.Join(where, " AND ")
	var c FolderCounts
	err = r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE t.archived=FALSE AND NOT EXISTS (
				SELECT 1 FROM email_messages m WHERE m.thread_id=t.id AND m.status='draft'
				AND NOT EXISTS (SELECT 1 FROM email_messages m2 WHERE m2.thread_id=t.id AND m2.status<>'draft')
			)),
			COUNT(*) FILTER (WHERE t.archived=FALSE AND EXISTS (
				SELECT 1 FROM email_messages m WHERE m.thread_id=t.id AND m.direction='outbound' AND m.status IN ('sent','delivered','sending')
			)),
			COUNT(*) FILTER (WHERE EXISTS (SELECT 1 FROM email_messages m WHERE m.thread_id=t.id AND m.status='draft')),
			COUNT(*) FILTER (WHERE t.starred=TRUE),
			COUNT(*) FILTER (WHERE t.match_status IN ('unmatched','needs_association') AND t.archived=FALSE),
			COUNT(*) FILTER (WHERE EXISTS (SELECT 1 FROM email_messages m WHERE m.thread_id=t.id AND m.status='scheduled')),
			COUNT(*) FILTER (WHERE t.archived=TRUE)
		FROM email_threads t
		WHERE `+pred, args...).Scan(&c.Inbox, &c.Sent, &c.Drafts, &c.Starred, &c.Unmatched, &c.Scheduled, &c.Archived)
	return c, err
}

const messageSelect = `
	SELECT m.id::text, m.thread_id::text, m.account_id::text, m.owner_user_id::text,
		m.provider_message_id, m.provider_thread_id, m.direction, m.from_address, m.from_name,
		m.to_addresses, m.cc_addresses, m.bcc_addresses, m.subject, m.body_html, m.body_text,
		m.snippet, m.sent_at, m.received_at, m.scheduled_at, m.status, m.has_attachments,
		m.in_reply_to::text, m.created_at
	FROM email_messages m
`

func scanMessage(row interface{ Scan(dest ...any) error }) (*Message, error) {
	var m Message
	var inReply *string
	err := row.Scan(&m.ID, &m.ThreadID, &m.AccountID, &m.OwnerUserID, &m.ProviderMessageID,
		&m.ProviderThreadID, &m.Direction, &m.FromAddress, &m.FromName, &m.ToAddresses,
		&m.CcAddresses, &m.BccAddresses, &m.Subject, &m.BodyHTML, &m.BodyText, &m.Snippet,
		&m.SentAt, &m.ReceivedAt, &m.ScheduledAt, &m.Status, &m.HasAttachments, &inReply, &m.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.InReplyTo = nilIfEmpty(inReply)
	if m.ToAddresses == nil {
		m.ToAddresses = []string{}
	}
	if m.CcAddresses == nil {
		m.CcAddresses = []string{}
	}
	if m.BccAddresses == nil {
		m.BccAddresses = []string{}
	}
	return &m, nil
}

func scanMessages(rows pgx.Rows) ([]Message, error) {
	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (r *Repository) GetMessage(ctx context.Context, id string) (*Message, error) {
	row := r.pool.QueryRow(ctx, messageSelect+" WHERE m.id=$1", id)
	m, err := scanMessage(row)
	if err != nil || m == nil {
		return m, err
	}
	atts, err := r.ListAttachments(ctx, m.ID)
	if err != nil {
		return nil, err
	}
	m.Attachments = atts
	return m, nil
}

func (r *Repository) ListMessages(ctx context.Context, threadID string) ([]Message, error) {
	rows, err := r.pool.Query(ctx, messageSelect+`
		WHERE m.thread_id=$1
		ORDER BY COALESCE(m.sent_at, m.received_at, m.created_at) ASC
	`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	msgs, err := scanMessages(rows)
	if err != nil {
		return nil, err
	}
	for i := range msgs {
		atts, err := r.ListAttachments(ctx, msgs[i].ID)
		if err != nil {
			return nil, err
		}
		msgs[i].Attachments = atts
	}
	if msgs == nil {
		msgs = []Message{}
	}
	return msgs, nil
}

func (r *Repository) HasProviderMessage(ctx context.Context, accountID, providerMessageID string) (bool, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM email_messages WHERE account_id=$1 AND provider_message_id=$2
	`, accountID, providerMessageID).Scan(&n)
	return n > 0, err
}

func (r *Repository) InsertMessage(ctx context.Context, m Message) (*Message, error) {
	id := m.ID
	if id == "" {
		id = uuid.NewString()
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO email_messages (
			id, thread_id, account_id, owner_user_id, provider_message_id, provider_thread_id,
			direction, from_address, from_name, to_addresses, cc_addresses, bcc_addresses,
			subject, body_html, body_text, snippet, sent_at, received_at, scheduled_at,
			status, has_attachments, in_reply_to
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)
	`, id, m.ThreadID, m.AccountID, m.OwnerUserID, m.ProviderMessageID, m.ProviderThreadID,
		m.Direction, m.FromAddress, m.FromName, m.ToAddresses, m.CcAddresses, m.BccAddresses,
		m.Subject, m.BodyHTML, m.BodyText, m.Snippet, m.SentAt, m.ReceivedAt, m.ScheduledAt,
		m.Status, m.HasAttachments, m.InReplyTo)
	if err != nil {
		return nil, err
	}
	return r.GetMessage(ctx, id)
}

func (r *Repository) UpdateMessage(ctx context.Context, m Message) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE email_messages SET
			to_addresses=$2, cc_addresses=$3, bcc_addresses=$4, subject=$5,
			body_html=$6, body_text=$7, snippet=$8, status=$9, provider_message_id=$10,
			provider_thread_id=$11, sent_at=$12, scheduled_at=$13, has_attachments=$14,
			updated_at=NOW()
		WHERE id=$1
	`, m.ID, m.ToAddresses, m.CcAddresses, m.BccAddresses, m.Subject, m.BodyHTML, m.BodyText,
		m.Snippet, m.Status, m.ProviderMessageID, m.ProviderThreadID, m.SentAt, m.ScheduledAt, m.HasAttachments)
	return err
}

func (r *Repository) InsertAttachment(ctx context.Context, a Attachment) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO email_attachments (id, message_id, filename, mime_type, size_bytes, provider_attachment_id)
		VALUES ($1,$2,$3,$4,$5,$6)
	`, uuid.NewString(), a.MessageID, a.Filename, a.MimeType, a.SizeBytes, a.ProviderAttachmentID)
	return err
}

func (r *Repository) ListAttachments(ctx context.Context, messageID string) ([]Attachment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, message_id::text, filename, mime_type, size_bytes, provider_attachment_id
		FROM email_attachments WHERE message_id=$1 ORDER BY created_at
	`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.MessageID, &a.Filename, &a.MimeType, &a.SizeBytes, &a.ProviderAttachmentID); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if out == nil {
		out = []Attachment{}
	}
	return out, rows.Err()
}

func (r *Repository) ListTemplates(ctx context.Context, teamIDs []string) ([]Template, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, name, subject, body_html, category, variables, is_active,
			created_by::text, team_id::text, created_at, updated_at
		FROM email_templates
		WHERE is_active=TRUE
		  AND (team_id IS NULL OR team_id = ANY($1::uuid[]) OR cardinality($1::uuid[])=0)
		ORDER BY name
	`, teamIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Template
	for rows.Next() {
		var t Template
		if err := rows.Scan(&t.ID, &t.Name, &t.Subject, &t.BodyHTML, &t.Category, &t.Variables,
			&t.IsActive, &t.CreatedBy, &t.TeamID, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		if t.Variables == nil {
			t.Variables = []string{}
		}
		out = append(out, t)
	}
	if out == nil {
		out = []Template{}
	}
	return out, rows.Err()
}

func (r *Repository) GetTemplate(ctx context.Context, id string) (*Template, error) {
	var t Template
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, name, subject, body_html, category, variables, is_active,
			created_by::text, team_id::text, created_at, updated_at
		FROM email_templates WHERE id=$1
	`, id).Scan(&t.ID, &t.Name, &t.Subject, &t.BodyHTML, &t.Category, &t.Variables,
		&t.IsActive, &t.CreatedBy, &t.TeamID, &t.CreatedAt, &t.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if t.Variables == nil {
		t.Variables = []string{}
	}
	return &t, nil
}

func (r *Repository) InsertTemplate(ctx context.Context, t Template) (*Template, error) {
	id := uuid.NewString()
	err := r.pool.QueryRow(ctx, `
		INSERT INTO email_templates (id, name, subject, body_html, category, variables, is_active, created_by, team_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id::text
	`, id, t.Name, t.Subject, t.BodyHTML, t.Category, t.Variables, t.IsActive, t.CreatedBy, t.TeamID).Scan(&id)
	if err != nil {
		return nil, err
	}
	return r.GetTemplate(ctx, id)
}

func (r *Repository) UpdateTemplate(ctx context.Context, t Template) (*Template, error) {
	_, err := r.pool.Exec(ctx, `
		UPDATE email_templates SET name=$2, subject=$3, body_html=$4, category=$5, variables=$6, is_active=$7, team_id=$8, updated_at=NOW()
		WHERE id=$1
	`, t.ID, t.Name, t.Subject, t.BodyHTML, t.Category, t.Variables, t.IsActive, t.TeamID)
	if err != nil {
		return nil, err
	}
	return r.GetTemplate(ctx, t.ID)
}

func (r *Repository) DeleteTemplate(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM email_templates WHERE id=$1`, id)
	return err
}

type crmMatch struct {
	LeadID       *string
	LeadName     string
	CustomerID   *string
	CustomerName string
	Count        int
}

func (r *Repository) MatchAddresses(ctx context.Context, addresses []string) (crmMatch, error) {
	var m crmMatch
	if len(addresses) == 0 {
		return m, nil
	}
	lower := make([]string, 0, len(addresses))
	for _, a := range addresses {
		a = strings.ToLower(strings.TrimSpace(a))
		if a != "" {
			lower = append(lower, a)
		}
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, full_name FROM leads
		WHERE is_archived=FALSE AND email IS NOT NULL AND lower(email)=ANY($1)
		LIMIT 5
	`, lower)
	if err != nil {
		return m, err
	}
	type hit struct{ id, name string }
	var leads []hit
	for rows.Next() {
		var h hit
		if err := rows.Scan(&h.id, &h.name); err != nil {
			rows.Close()
			return m, err
		}
		leads = append(leads, h)
	}
	rows.Close()

	rows, err = r.pool.Query(ctx, `
		SELECT id::text, full_name FROM customers
		WHERE is_archived=FALSE AND email IS NOT NULL AND lower(email)=ANY($1)
		LIMIT 5
	`, lower)
	if err != nil {
		return m, err
	}
	var customers []hit
	for rows.Next() {
		var h hit
		if err := rows.Scan(&h.id, &h.name); err != nil {
			rows.Close()
			return m, err
		}
		customers = append(customers, h)
	}
	rows.Close()

	m.Count = len(leads) + len(customers)
	if len(leads) == 1 && len(customers) == 0 {
		id := leads[0].id
		m.LeadID = &id
		m.LeadName = leads[0].name
	} else if len(customers) == 1 && len(leads) == 0 {
		id := customers[0].id
		m.CustomerID = &id
		m.CustomerName = customers[0].name
	} else if len(leads) == 1 && len(customers) == 1 {
		// Prefer customer if the lead converted into that customer is unknown — still needs association if both exist independently.
		id := customers[0].id
		m.CustomerID = &id
		m.CustomerName = customers[0].name
		lid := leads[0].id
		m.LeadID = &lid
		m.LeadName = leads[0].name
		m.Count = 2
	}
	return m, nil
}

func (r *Repository) PersonalizationContext(ctx context.Context, leadID, customerID *string) (map[string]string, error) {
	out := map[string]string{}
	id := ""
	table := ""
	if customerID != nil && *customerID != "" {
		id = *customerID
		table = "customers"
	} else if leadID != nil && *leadID != "" {
		id = *leadID
		table = "leads"
	}
	if id == "" {
		return out, nil
	}
	var fullName, email, phone, employer, ownerName *string
	q := fmt.Sprintf(`
		SELECT e.full_name, e.email, e.phone, e.employer, u.full_name
		FROM %s e
		LEFT JOIN users u ON u.id=e.owner_user_id
		WHERE e.id=$1
	`, table)
	err := r.pool.QueryRow(ctx, q, id).Scan(&fullName, &email, &phone, &employer, &ownerName)
	if err == pgx.ErrNoRows {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	name := str(fullName)
	first, last := splitName(name)
	out["full_name"] = name
	out["first_name"] = first
	out["last_name"] = last
	out["email"] = str(email)
	out["phone"] = str(phone)
	out["company_name"] = str(employer)
	out["lead_owner"] = str(ownerName)
	return out, nil
}

func str(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func splitName(full string) (string, string) {
	full = strings.TrimSpace(full)
	if full == "" {
		return "", ""
	}
	parts := strings.Fields(full)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], strings.Join(parts[1:], " ")
}

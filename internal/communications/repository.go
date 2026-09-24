package communications

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) ListAccounts(ctx context.Context, provider string, activeOnly bool) ([]Account, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, provider, name, display_identifier, external_account_id, credentials,
			is_active, owner_user_id::text, team_id::text, allow_recordings, metadata, created_at, updated_at
		FROM communication_accounts
		WHERE ($1='' OR provider=$1) AND ($2=FALSE OR is_active=TRUE)
		ORDER BY name ASC
	`, provider, activeOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	if out == nil {
		out = []Account{}
	}
	return out, rows.Err()
}

func (r *Repository) GetAccount(ctx context.Context, id string) (*Account, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id::text, provider, name, display_identifier, external_account_id, credentials,
			is_active, owner_user_id::text, team_id::text, allow_recordings, metadata, created_at, updated_at
		FROM communication_accounts WHERE id=$1
	`, id)
	a, err := scanAccount(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return a, err
}

func (r *Repository) FindAccountByExternal(ctx context.Context, provider, externalID string) (*Account, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id::text, provider, name, display_identifier, external_account_id, credentials,
			is_active, owner_user_id::text, team_id::text, allow_recordings, metadata, created_at, updated_at
		FROM communication_accounts
		WHERE provider=$1 AND external_account_id=$2 AND is_active=TRUE
		LIMIT 1
	`, provider, externalID)
	a, err := scanAccount(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return a, err
}

func (r *Repository) CreateAccount(ctx context.Context, in CreateAccountInput) (*Account, error) {
	id := uuid.NewString()
	active := true
	if in.IsActive != nil {
		active = *in.IsActive
	}
	creds, _ := json.Marshal(in.Credentials)
	if in.Credentials == nil {
		creds = []byte("{}")
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO communication_accounts (
			id, provider, name, display_identifier, external_account_id, credentials,
			is_active, owner_user_id, team_id, allow_recordings
		) VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10)
	`, id, in.Provider, strings.TrimSpace(in.Name), strings.TrimSpace(in.DisplayIdentifier),
		strings.TrimSpace(in.ExternalAccountID), string(creds), active,
		emptyUUIDPtr(in.OwnerUserID), emptyUUIDPtr(in.TeamID), in.AllowRecordings)
	if err != nil {
		return nil, err
	}
	return r.GetAccount(ctx, id)
}

func (r *Repository) ClaimWebhook(ctx context.Context, provider, eventKey string, payload map[string]any) (bool, error) {
	body, _ := json.Marshal(payload)
	if payload == nil {
		body = []byte("{}")
	}
	ct, err := r.pool.Exec(ctx, `
		INSERT INTO webhook_receipts (id, provider, event_key, payload, result)
		VALUES ($1,$2,$3,$4::jsonb,'processing')
		ON CONFLICT (provider, event_key) DO NOTHING
	`, uuid.NewString(), provider, eventKey, string(body))
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}

func (r *Repository) CompleteWebhook(ctx context.Context, provider, eventKey, result, errMsg string, status int) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE webhook_receipts
		SET result=$3, error_message=$4, http_status=$5, processed_at=NOW()
		WHERE provider=$1 AND event_key=$2
	`, provider, eventKey, result, errMsg, status)
	return err
}

func (r *Repository) InsertWhatsAppMessage(ctx context.Context, m *WhatsAppMessage, raw map[string]any) (*WhatsAppMessage, error) {
	if m.ProviderMessageID != "" {
		if existing, err := r.GetWhatsAppByProviderID(ctx, m.AccountID, m.ProviderMessageID); err != nil {
			return nil, err
		} else if existing != nil {
			return existing, nil
		}
	}
	id := uuid.NewString()
	payload, _ := json.Marshal(raw)
	if raw == nil {
		payload = []byte("{}")
	}
	occurred := time.Now().UTC()
	if m.OccurredAt != "" {
		if t, err := time.Parse(time.RFC3339, m.OccurredAt); err == nil {
			occurred = t.UTC()
		}
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO whatsapp_messages (
			id, account_id, direction, status, provider_message_id, conversation_key,
			from_number, to_number, body, media_url, media_mime, media_filename,
			error_code, error_message, lead_id, customer_id, deal_id, activity_id,
			timeline_event_id, actor_user_id, raw_payload, occurred_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21::jsonb,$22
		)
	`, id, m.AccountID, m.Direction, m.Status, m.ProviderMessageID, m.ConversationKey,
		m.FromNumber, m.ToNumber, m.Body, m.MediaURL, m.MediaMime, m.MediaFilename,
		m.ErrorCode, m.ErrorMessage, emptyUUIDPtr(m.LeadID), emptyUUIDPtr(m.CustomerID),
		emptyUUIDPtr(m.DealID), emptyUUIDPtr(m.ActivityID), emptyUUIDPtr(m.TimelineEventID),
		emptyUUIDPtr(m.ActorUserID), string(payload), occurred)
	if err != nil {
		if m.ProviderMessageID != "" {
			if existing, e2 := r.GetWhatsAppByProviderID(ctx, m.AccountID, m.ProviderMessageID); e2 == nil && existing != nil {
				return existing, nil
			}
		}
		return nil, err
	}
	return r.GetWhatsApp(ctx, id)
}

func (r *Repository) GetWhatsApp(ctx context.Context, id string) (*WhatsAppMessage, error) {
	row := r.pool.QueryRow(ctx, whatsappSelect+` WHERE id=$1`, id)
	return scanWhatsApp(row)
}

func (r *Repository) GetWhatsAppByProviderID(ctx context.Context, accountID, providerMsgID string) (*WhatsAppMessage, error) {
	row := r.pool.QueryRow(ctx, whatsappSelect+` WHERE account_id=$1 AND provider_message_id=$2`, accountID, providerMsgID)
	m, err := scanWhatsApp(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return m, err
}

func (r *Repository) UpdateWhatsAppStatus(ctx context.Context, accountID, providerMsgID, status, errCode, errMsg string) (*WhatsAppMessage, error) {
	_, err := r.pool.Exec(ctx, `
		UPDATE whatsapp_messages
		SET status=$3, error_code=COALESCE(NULLIF($4,''), error_code),
			error_message=COALESCE(NULLIF($5,''), error_message)
		WHERE account_id=$1 AND provider_message_id=$2
	`, accountID, providerMsgID, status, errCode, errMsg)
	if err != nil {
		return nil, err
	}
	return r.GetWhatsAppByProviderID(ctx, accountID, providerMsgID)
}

func (r *Repository) ListWhatsAppMessages(ctx context.Context, customerID, dealID, leadID string, limit, offset int) ([]WhatsAppMessage, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var total int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM whatsapp_messages
		WHERE ($1='' OR customer_id::text=$1) AND ($2='' OR deal_id::text=$2) AND ($3='' OR lead_id::text=$3)
	`, customerID, dealID, leadID).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, whatsappSelect+`
		WHERE ($1='' OR customer_id::text=$1) AND ($2='' OR deal_id::text=$2) AND ($3='' OR lead_id::text=$3)
		ORDER BY occurred_at DESC LIMIT $4 OFFSET $5
	`, customerID, dealID, leadID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []WhatsAppMessage
	for rows.Next() {
		m, err := scanWhatsApp(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *m)
	}
	if out == nil {
		out = []WhatsAppMessage{}
	}
	return out, total, rows.Err()
}

func (r *Repository) InsertCall(ctx context.Context, c *TwilioCall, raw map[string]any) (*TwilioCall, error) {
	if c.ProviderCallSID != "" {
		if existing, err := r.GetCallBySID(ctx, c.AccountID, c.ProviderCallSID); err != nil {
			return nil, err
		} else if existing != nil {
			return existing, nil
		}
	}
	id := uuid.NewString()
	payload, _ := json.Marshal(raw)
	if raw == nil {
		payload = []byte("{}")
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO twilio_calls (
			id, account_id, direction, status, provider_call_sid, from_number, to_number,
			duration_seconds, recording_sid, recording_url, error_code, error_message,
			lead_id, customer_id, deal_id, activity_id, timeline_event_id, actor_user_id,
			raw_payload, started_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19::jsonb,NOW()
		)
	`, id, c.AccountID, c.Direction, c.Status, c.ProviderCallSID, c.FromNumber, c.ToNumber,
		c.DurationSeconds, c.RecordingSID, c.RecordingURL, c.ErrorCode, c.ErrorMessage,
		emptyUUIDPtr(c.LeadID), emptyUUIDPtr(c.CustomerID), emptyUUIDPtr(c.DealID),
		emptyUUIDPtr(c.ActivityID), emptyUUIDPtr(c.TimelineEventID), emptyUUIDPtr(c.ActorUserID),
		string(payload))
	if err != nil {
		if c.ProviderCallSID != "" {
			if existing, e2 := r.GetCallBySID(ctx, c.AccountID, c.ProviderCallSID); e2 == nil && existing != nil {
				return existing, nil
			}
		}
		return nil, err
	}
	return r.GetCall(ctx, id)
}

func (r *Repository) GetCall(ctx context.Context, id string) (*TwilioCall, error) {
	row := r.pool.QueryRow(ctx, callSelect+` WHERE id=$1`, id)
	return scanCall(row)
}

func (r *Repository) GetCallBySID(ctx context.Context, accountID, sid string) (*TwilioCall, error) {
	row := r.pool.QueryRow(ctx, callSelect+` WHERE account_id=$1 AND provider_call_sid=$2`, accountID, sid)
	c, err := scanCall(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return c, err
}

func (r *Repository) GetCallBySIDAny(ctx context.Context, sid string) (*TwilioCall, error) {
	row := r.pool.QueryRow(ctx, callSelect+` WHERE provider_call_sid=$1 ORDER BY created_at DESC LIMIT 1`, sid)
	c, err := scanCall(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return c, err
}

func (r *Repository) UpdateCallStatus(ctx context.Context, sid, status string, duration *int, recordingSID, recordingURL string) (*TwilioCall, error) {
	_, err := r.pool.Exec(ctx, `
		UPDATE twilio_calls
		SET status=$2,
			duration_seconds=COALESCE($3, duration_seconds),
			recording_sid=CASE WHEN $4='' THEN recording_sid ELSE $4 END,
			recording_url=CASE WHEN $5='' THEN recording_url ELSE $5 END,
			ended_at=CASE WHEN $2 IN ('completed','busy','failed','no-answer','canceled') THEN NOW() ELSE ended_at END
		WHERE provider_call_sid=$1
	`, sid, status, duration, recordingSID, recordingURL)
	if err != nil {
		return nil, err
	}
	return r.GetCallBySIDAny(ctx, sid)
}

func (r *Repository) ListCalls(ctx context.Context, customerID, dealID, leadID string, limit, offset int) ([]TwilioCall, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var total int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM twilio_calls
		WHERE ($1='' OR customer_id::text=$1) AND ($2='' OR deal_id::text=$2) AND ($3='' OR lead_id::text=$3)
	`, customerID, dealID, leadID).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, callSelect+`
		WHERE ($1='' OR customer_id::text=$1) AND ($2='' OR deal_id::text=$2) AND ($3='' OR lead_id::text=$3)
		ORDER BY created_at DESC LIMIT $4 OFFSET $5
	`, customerID, dealID, leadID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []TwilioCall
	for rows.Next() {
		c, err := scanCall(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *c)
	}
	if out == nil {
		out = []TwilioCall{}
	}
	return out, total, rows.Err()
}

func (r *Repository) ResolvePhoneContext(ctx context.Context, phone string) (customerID, leadID *string, err error) {
	phone = normalizePhone(phone)
	digits := digitsOnly(phone)
	var cid, lid *string
	_ = r.pool.QueryRow(ctx, `
		SELECT id::text FROM customers
		WHERE regexp_replace(COALESCE(phone,''), '[^0-9]', '', 'g') = $1
		   OR regexp_replace(COALESCE(phone,''), '[^0-9]', '', 'g') LIKE '%' || $1
		ORDER BY updated_at DESC LIMIT 1
	`, digits).Scan(&cid)
	if cid == nil {
		_ = r.pool.QueryRow(ctx, `
			SELECT id::text FROM leads
			WHERE regexp_replace(COALESCE(phone,''), '[^0-9]', '', 'g') = $1
			   OR regexp_replace(COALESCE(phone,''), '[^0-9]', '', 'g') LIKE '%' || $1
			ORDER BY updated_at DESC LIMIT 1
		`, digits).Scan(&lid)
	}
	return cid, lid, nil
}

func (r *Repository) LoadEntityPhone(ctx context.Context, customerID, leadID, dealID string) (string, error) {
	if customerID != "" {
		var phone *string
		err := r.pool.QueryRow(ctx, `SELECT phone FROM customers WHERE id=$1`, customerID).Scan(&phone)
		if err != nil {
			return "", err
		}
		if phone != nil {
			return *phone, nil
		}
	}
	if leadID != "" {
		var phone *string
		err := r.pool.QueryRow(ctx, `SELECT phone FROM leads WHERE id=$1`, leadID).Scan(&phone)
		if err != nil {
			return "", err
		}
		if phone != nil {
			return *phone, nil
		}
	}
	if dealID != "" {
		var phone *string
		err := r.pool.QueryRow(ctx, `
			SELECT c.phone FROM deals d JOIN customers c ON c.id=d.customer_id WHERE d.id=$1
		`, dealID).Scan(&phone)
		if err != nil {
			return "", err
		}
		if phone != nil {
			return *phone, nil
		}
	}
	return "", nil
}

func (r *Repository) DealCustomerID(ctx context.Context, dealID string) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx, `SELECT customer_id::text FROM deals WHERE id=$1`, dealID).Scan(&id)
	return id, err
}

const whatsappSelect = `
	SELECT id::text, account_id::text, direction, status, provider_message_id, conversation_key,
		from_number, to_number, body, media_url, media_mime, media_filename,
		error_code, error_message, lead_id::text, customer_id::text, deal_id::text,
		activity_id::text, timeline_event_id::text, actor_user_id::text, occurred_at, created_at
	FROM whatsapp_messages`

const callSelect = `
	SELECT id::text, account_id::text, direction, status, provider_call_sid, from_number, to_number,
		duration_seconds, recording_sid, recording_url, error_code, error_message,
		lead_id::text, customer_id::text, deal_id::text, activity_id::text, timeline_event_id::text,
		actor_user_id::text, started_at, ended_at, created_at
	FROM twilio_calls`

type scannable interface {
	Scan(dest ...any) error
}

func scanAccount(row scannable) (*Account, error) {
	var a Account
	var creds, meta []byte
	var createdAt, updatedAt time.Time
	var owner, team *string
	if err := row.Scan(&a.ID, &a.Provider, &a.Name, &a.DisplayIdentifier, &a.ExternalAccountID, &creds,
		&a.IsActive, &owner, &team, &a.AllowRecordings, &meta, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	a.OwnerUserID = owner
	a.TeamID = team
	a.Credentials = map[string]string{}
	_ = json.Unmarshal(creds, &a.Credentials)
	a.Metadata = map[string]any{}
	_ = json.Unmarshal(meta, &a.Metadata)
	a.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	a.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return &a, nil
}

func scanWhatsApp(row scannable) (*WhatsAppMessage, error) {
	var m WhatsAppMessage
	var occurred, created time.Time
	if err := row.Scan(&m.ID, &m.AccountID, &m.Direction, &m.Status, &m.ProviderMessageID, &m.ConversationKey,
		&m.FromNumber, &m.ToNumber, &m.Body, &m.MediaURL, &m.MediaMime, &m.MediaFilename,
		&m.ErrorCode, &m.ErrorMessage, &m.LeadID, &m.CustomerID, &m.DealID,
		&m.ActivityID, &m.TimelineEventID, &m.ActorUserID, &occurred, &created); err != nil {
		return nil, err
	}
	m.OccurredAt = occurred.UTC().Format(time.RFC3339)
	m.CreatedAt = created.UTC().Format(time.RFC3339)
	return &m, nil
}

func scanCall(row scannable) (*TwilioCall, error) {
	var c TwilioCall
	var started, ended *time.Time
	var created time.Time
	if err := row.Scan(&c.ID, &c.AccountID, &c.Direction, &c.Status, &c.ProviderCallSID, &c.FromNumber, &c.ToNumber,
		&c.DurationSeconds, &c.RecordingSID, &c.RecordingURL, &c.ErrorCode, &c.ErrorMessage,
		&c.LeadID, &c.CustomerID, &c.DealID, &c.ActivityID, &c.TimelineEventID, &c.ActorUserID,
		&started, &ended, &created); err != nil {
		return nil, err
	}
	if started != nil {
		s := started.UTC().Format(time.RFC3339)
		c.StartedAt = &s
	}
	if ended != nil {
		s := ended.UTC().Format(time.RFC3339)
		c.EndedAt = &s
	}
	c.CreatedAt = created.UTC().Format(time.RFC3339)
	return &c, nil
}

func emptyUUIDPtr(s *string) any {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	return *s
}

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// PublicDTO strips credentials for API responses.
func (a Account) PublicDTO() Account {
	a.Credentials = nil
	return a
}

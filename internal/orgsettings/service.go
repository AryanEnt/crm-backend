package orgsettings

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/crm/backend/internal/audit"
	apperrors "github.com/crm/backend/pkg/apperrors"
)

type Setting struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	ValueType string    `json:"valueType"`
	UpdatedBy *string   `json:"updatedBy"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Service struct {
	pool  *pgxpool.Pool
	audit *audit.Service
}

func NewService(pool *pgxpool.Pool, a *audit.Service) *Service {
	return &Service{pool: pool, audit: a}
}

func (s *Service) List(ctx context.Context) ([]Setting, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT key, value, value_type, updated_by::text, updated_at
		FROM organization_settings ORDER BY key`)
	if err != nil {
		return nil, apperrors.Internal("list settings", err)
	}
	defer rows.Close()
	var out []Setting
	for rows.Next() {
		var it Setting
		_ = rows.Scan(&it.Key, &it.Value, &it.ValueType, &it.UpdatedBy, &it.UpdatedAt)
		out = append(out, it)
	}
	if out == nil {
		out = []Setting{}
	}
	return out, nil
}

func (s *Service) Map(ctx context.Context) (map[string]string, error) {
	items, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(items))
	for _, it := range items {
		m[it.Key] = it.Value
	}
	return m, nil
}

// Defaults returns non-sensitive CRM defaults for form prefill (any authenticated CRM user).
func (s *Service) Defaults(ctx context.Context) (map[string]string, error) {
	all, err := s.Map(ctx)
	if err != nil {
		return nil, err
	}
	keys := []string{
		"organization.timezone", "organization.currency", "organization.date_format", "organization.time_format",
		"crm.default_pipeline_id", "crm.default_lead_source", "crm.default_activity_type",
		"crm.default_lead_priority", "crm.default_deal_currency",
		"crm.default_followup_hours", "crm.default_activity_duration_minutes",
	}
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		if v, ok := all[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, key string) (string, error) {
	var v string
	err := s.pool.QueryRow(ctx, `SELECT value FROM organization_settings WHERE key=$1`, key).Scan(&v)
	if err != nil {
		return "", nil
	}
	return v, nil
}

type UpdateInput struct {
	Settings map[string]string `json:"settings"`
}

var allowedKeys = map[string]bool{
	"organization.name": true, "organization.email": true, "organization.phone": true,
	"organization.address": true, "organization.timezone": true, "organization.currency": true,
	"organization.date_format": true, "organization.time_format": true,
	"crm.default_pipeline_id": true, "crm.default_lead_source": true,
	"crm.default_activity_type": true, "crm.default_lead_priority": true,
	"crm.default_deal_currency": true, "crm.default_followup_hours": true,
	"crm.default_activity_duration_minutes": true,
	"notifications.email_enabled": true, "notifications.activity_reminders": true,
	"notifications.overdue_alerts": true, "notifications.automation": true,
	"security.session_hours": true,
}

func (s *Service) Update(ctx context.Context, actorID string, in UpdateInput, ip, ua string) ([]Setting, error) {
	if len(in.Settings) == 0 {
		return s.List(ctx)
	}
	for k, v := range in.Settings {
		if !allowedKeys[k] {
			return nil, apperrors.Validation("unknown setting key: " + k)
		}
		_, err := s.pool.Exec(ctx, `
			INSERT INTO organization_settings (key, value, value_type, updated_by)
			VALUES ($1,$2,'string',$3::uuid)
			ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_by=EXCLUDED.updated_by, updated_at=NOW()`,
			k, strings.TrimSpace(v), actorID)
		if err != nil {
			return nil, apperrors.Internal("update setting", err)
		}
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "settings.updated", "organization_settings", nil, map[string]any{
		"keys": keysOf(in.Settings),
	}, ip, ua)
	return s.List(ctx)
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

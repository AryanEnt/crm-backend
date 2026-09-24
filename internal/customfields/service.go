package customfields

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/crm/backend/internal/audit"
	apperrors "github.com/crm/backend/pkg/apperrors"
)

var keyRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type Option struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Value    string `json:"value"`
	Position int    `json:"position"`
	IsActive bool   `json:"isActive"`
}

type Field struct {
	ID           string    `json:"id"`
	Entity       string    `json:"entity"`
	Name         string    `json:"name"`
	InternalKey  string    `json:"internalKey"`
	FieldType    string    `json:"fieldType"`
	Description  string    `json:"description"`
	HelpText     string    `json:"helpText"`
	IsRequired   bool      `json:"isRequired"`
	IsActive     bool      `json:"isActive"`
	DisplayOrder int       `json:"displayOrder"`
	CreatedBy    *string   `json:"createdBy"`
	CreatedByName *string  `json:"createdByName,omitempty"`
	Options      []Option  `json:"options"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type OptionInput struct {
	Label    string `json:"label"`
	Value    string `json:"value"`
	Position int    `json:"position"`
}

type CreateInput struct {
	Entity       string        `json:"entity"`
	Name         string        `json:"name"`
	InternalKey  string        `json:"internalKey"`
	FieldType    string        `json:"fieldType"`
	Description  string        `json:"description"`
	HelpText     string        `json:"helpText"`
	IsRequired   bool          `json:"isRequired"`
	IsActive     *bool         `json:"isActive"`
	DisplayOrder *int          `json:"displayOrder"`
	Options      []OptionInput `json:"options"`
}

type UpdateInput struct {
	Name         *string        `json:"name"`
	Description  *string        `json:"description"`
	HelpText     *string        `json:"helpText"`
	IsRequired   *bool          `json:"isRequired"`
	IsActive     *bool          `json:"isActive"`
	DisplayOrder *int           `json:"displayOrder"`
	Options      *[]OptionInput `json:"options"`
}

type ValueMap map[string]any

type Service struct {
	pool  *pgxpool.Pool
	audit *audit.Service
}

func NewService(pool *pgxpool.Pool, a *audit.Service) *Service {
	return &Service{pool: pool, audit: a}
}

func validEntity(e string) bool {
	switch e {
	case "lead", "customer", "deal", "activity":
		return true
	}
	return false
}

func validType(t string) bool {
	switch t {
	case "text", "long_text", "number", "currency", "date", "datetime",
		"boolean", "single_select", "multi_select", "url", "email", "phone":
		return true
	}
	return false
}

func (s *Service) List(ctx context.Context, entity, fieldType string, activeOnly bool, q string) ([]Field, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT cf.id::text, cf.entity, cf.name, cf.internal_key, cf.field_type, cf.description, cf.help_text,
			cf.is_required, cf.is_active, cf.display_order, cf.created_by::text, u.full_name,
			cf.created_at, cf.updated_at
		FROM custom_fields cf
		LEFT JOIN users u ON u.id = cf.created_by
		WHERE ($1 = '' OR cf.entity = $1)
		  AND ($2 = '' OR cf.field_type = $2)
		  AND ($3::bool = false OR cf.is_active = true)
		  AND ($4 = '' OR cf.name ILIKE '%'||$4||'%' OR cf.internal_key ILIKE '%'||$4||'%')
		ORDER BY cf.entity, cf.display_order, cf.name`, entity, fieldType, activeOnly, strings.TrimSpace(q))
	if err != nil {
		return nil, apperrors.Internal("failed to list custom fields", err)
	}
	defer rows.Close()
	var out []Field
	for rows.Next() {
		var f Field
		var createdBy, createdByName *string
		if err := rows.Scan(&f.ID, &f.Entity, &f.Name, &f.InternalKey, &f.FieldType, &f.Description, &f.HelpText,
			&f.IsRequired, &f.IsActive, &f.DisplayOrder, &createdBy, &createdByName, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, apperrors.Internal("scan custom field", err)
		}
		f.CreatedBy = createdBy
		f.CreatedByName = createdByName
		opts, _ := s.loadOptions(ctx, f.ID)
		f.Options = opts
		out = append(out, f)
	}
	if out == nil {
		out = []Field{}
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Field, error) {
	var f Field
	var createdBy, createdByName *string
	err := s.pool.QueryRow(ctx, `
		SELECT cf.id::text, cf.entity, cf.name, cf.internal_key, cf.field_type, cf.description, cf.help_text,
			cf.is_required, cf.is_active, cf.display_order, cf.created_by::text, u.full_name,
			cf.created_at, cf.updated_at
		FROM custom_fields cf
		LEFT JOIN users u ON u.id = cf.created_by
		WHERE cf.id=$1::uuid`, id,
	).Scan(&f.ID, &f.Entity, &f.Name, &f.InternalKey, &f.FieldType, &f.Description, &f.HelpText,
		&f.IsRequired, &f.IsActive, &f.DisplayOrder, &createdBy, &createdByName, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, apperrors.NotFound("custom field not found")
	}
	f.CreatedBy = createdBy
	f.CreatedByName = createdByName
	f.Options, _ = s.loadOptions(ctx, f.ID)
	return &f, nil
}

func (s *Service) loadOptions(ctx context.Context, fieldID string) ([]Option, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, label, value, position, is_active
		FROM custom_field_options WHERE custom_field_id=$1::uuid ORDER BY position, label`, fieldID)
	if err != nil {
		return []Option{}, nil
	}
	defer rows.Close()
	var out []Option
	for rows.Next() {
		var o Option
		_ = rows.Scan(&o.ID, &o.Label, &o.Value, &o.Position, &o.IsActive)
		out = append(out, o)
	}
	if out == nil {
		out = []Option{}
	}
	return out, nil
}

func (s *Service) Create(ctx context.Context, actorID string, in CreateInput, ip, ua string) (*Field, error) {
	entity := strings.ToLower(strings.TrimSpace(in.Entity))
	name := strings.TrimSpace(in.Name)
	key := strings.TrimSpace(in.InternalKey)
	ft := strings.TrimSpace(in.FieldType)
	if !validEntity(entity) {
		return nil, apperrors.Validation("invalid entity")
	}
	if name == "" {
		return nil, apperrors.Validation("field name is required")
	}
	if !keyRe.MatchString(key) {
		return nil, apperrors.Validation("internal key must be lowercase letters, numbers, underscores")
	}
	if !validType(ft) {
		return nil, apperrors.Validation("invalid field type")
	}
	if (ft == "single_select" || ft == "multi_select") && len(in.Options) == 0 {
		return nil, apperrors.Validation("select fields require at least one option")
	}
	active := true
	if in.IsActive != nil {
		active = *in.IsActive
	}
	order := 0
	if in.DisplayOrder != nil {
		order = *in.DisplayOrder
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, apperrors.Internal("begin tx", err)
	}
	defer tx.Rollback(ctx)

	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO custom_fields (entity, name, internal_key, field_type, description, help_text,
			is_required, is_active, display_order, created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::uuid,$10::uuid)
		RETURNING id::text`,
		entity, name, key, ft, strings.TrimSpace(in.Description), strings.TrimSpace(in.HelpText),
		in.IsRequired, active, order, actorID,
	).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "unique") {
			return nil, apperrors.Conflict("internal key already exists for this entity")
		}
		return nil, apperrors.Internal("create custom field", err)
	}
	if err := s.replaceOptions(ctx, tx, id, in.Options); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, apperrors.Internal("commit", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "custom_field.created", "custom_field", audit.Ptr(id), map[string]any{
		"entity": entity, "name": name, "internalKey": key, "fieldType": ft,
	}, ip, ua)
	return s.Get(ctx, id)
}

func (s *Service) Update(ctx context.Context, actorID, id string, in UpdateInput, ip, ua string) (*Field, error) {
	cur, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	name := cur.Name
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	desc := cur.Description
	if in.Description != nil {
		desc = strings.TrimSpace(*in.Description)
	}
	help := cur.HelpText
	if in.HelpText != nil {
		help = strings.TrimSpace(*in.HelpText)
	}
	req := cur.IsRequired
	if in.IsRequired != nil {
		req = *in.IsRequired
	}
	active := cur.IsActive
	if in.IsActive != nil {
		active = *in.IsActive
	}
	order := cur.DisplayOrder
	if in.DisplayOrder != nil {
		order = *in.DisplayOrder
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, apperrors.Internal("begin tx", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		UPDATE custom_fields SET name=$2, description=$3, help_text=$4, is_required=$5, is_active=$6,
			display_order=$7, updated_by=$8::uuid WHERE id=$1::uuid`,
		id, name, desc, help, req, active, order, actorID)
	if err != nil {
		return nil, apperrors.Internal("update custom field", err)
	}
	if in.Options != nil {
		if err := s.replaceOptions(ctx, tx, id, *in.Options); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, apperrors.Internal("commit", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "custom_field.updated", "custom_field", audit.Ptr(id), map[string]any{
		"name": name, "isActive": active,
	}, ip, ua)
	return s.Get(ctx, id)
}

func (s *Service) Delete(ctx context.Context, actorID, id string, ip, ua string) error {
	var usage int
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM custom_field_values WHERE custom_field_id=$1::uuid`, id).Scan(&usage)
	if usage > 0 {
		return apperrors.Validation("cannot delete a field with stored values; deactivate it instead")
	}
	ct, err := s.pool.Exec(ctx, `DELETE FROM custom_fields WHERE id=$1::uuid`, id)
	if err != nil {
		return apperrors.Internal("delete custom field", err)
	}
	if ct.RowsAffected() == 0 {
		return apperrors.NotFound("custom field not found")
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "custom_field.deleted", "custom_field", audit.Ptr(id), nil, ip, ua)
	return nil
}

func (s *Service) replaceOptions(ctx context.Context, tx pgx.Tx, fieldID string, opts []OptionInput) error {
	_, err := tx.Exec(ctx, `DELETE FROM custom_field_options WHERE custom_field_id=$1::uuid`, fieldID)
	if err != nil {
		return apperrors.Internal("clear options", err)
	}
	for i, o := range opts {
		label := strings.TrimSpace(o.Label)
		val := strings.TrimSpace(o.Value)
		if val == "" {
			val = strings.ToLower(strings.ReplaceAll(label, " ", "_"))
		}
		if label == "" {
			continue
		}
		pos := o.Position
		if pos == 0 {
			pos = i + 1
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO custom_field_options (custom_field_id, label, value, position)
			VALUES ($1::uuid,$2,$3,$4)`, fieldID, label, val, pos)
		if err != nil {
			return apperrors.Internal("insert option", err)
		}
	}
	return nil
}

func (s *Service) GetValues(ctx context.Context, entity, recordID string) (ValueMap, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT cf.internal_key, v.value_text, v.value_json
		FROM custom_field_values v
		JOIN custom_fields cf ON cf.id = v.custom_field_id
		WHERE v.entity=$1 AND v.record_id=$2::uuid`, entity, recordID)
	if err != nil {
		return nil, apperrors.Internal("load field values", err)
	}
	defer rows.Close()
	out := ValueMap{}
	for rows.Next() {
		var key string
		var text *string
		var raw []byte
		_ = rows.Scan(&key, &text, &raw)
		if len(raw) > 0 && string(raw) != "null" {
			var v any
			if json.Unmarshal(raw, &v) == nil {
				out[key] = v
				continue
			}
		}
		if text != nil {
			out[key] = *text
		}
	}
	return out, nil
}

func (s *Service) SetValues(ctx context.Context, actorID, entity, recordID string, values ValueMap) error {
	if !validEntity(entity) {
		return apperrors.Validation("invalid entity")
	}
	defs, err := s.List(ctx, entity, "", true, "")
	if err != nil {
		return err
	}
	byKey := map[string]Field{}
	for _, d := range defs {
		byKey[d.InternalKey] = d
	}
	for key, val := range values {
		def, ok := byKey[key]
		if !ok {
			continue
		}
		text, jsonBytes := encodeValue(val)
		_, err = s.pool.Exec(ctx, `
			INSERT INTO custom_field_values (custom_field_id, entity, record_id, value_text, value_json, updated_by)
			VALUES ($1::uuid,$2,$3::uuid,$4,$5::jsonb,$6::uuid)
			ON CONFLICT (custom_field_id, record_id) DO UPDATE
			SET value_text=EXCLUDED.value_text, value_json=EXCLUDED.value_json, updated_by=EXCLUDED.updated_by, updated_at=NOW()`,
			def.ID, entity, recordID, text, string(jsonBytes), actorID)
		if err != nil {
			return apperrors.Internal("save field value", err)
		}
	}
	return nil
}

func encodeValue(v any) (*string, []byte) {
	if v == nil {
		return nil, []byte("null")
	}
	switch t := v.(type) {
	case string:
		s := t
		b, _ := json.Marshal(t)
		return &s, b
	default:
		b, _ := json.Marshal(v)
		s := string(b)
		return &s, b
	}
}

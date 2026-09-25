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
	ID            string    `json:"id"`
	Entity        string    `json:"entity"`
	Name          string    `json:"name"`
	InternalKey   string    `json:"internalKey"`
	FieldType     string    `json:"fieldType"`
	Description   string    `json:"description"`
	HelpText      string    `json:"helpText"`
	IsRequired    bool      `json:"isRequired"`
	IsActive      bool      `json:"isActive"`
	DisplayOrder  int       `json:"displayOrder"`
	PipelineID    *string   `json:"pipelineId"`
	PipelineName  *string   `json:"pipelineName"`
	StageID       *string   `json:"stageId"`
	StageName     *string   `json:"stageName"`
	CreatedBy     *string   `json:"createdBy"`
	CreatedByName *string   `json:"createdByName,omitempty"`
	Options       []Option  `json:"options"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
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
	PipelineID   *string       `json:"pipelineId"`
	StageID      *string       `json:"stageId"`
	Options      []OptionInput `json:"options"`
}

type UpdateInput struct {
	Name         *string        `json:"name"`
	Description  *string        `json:"description"`
	HelpText     *string        `json:"helpText"`
	IsRequired   *bool          `json:"isRequired"`
	IsActive     *bool          `json:"isActive"`
	DisplayOrder *int           `json:"displayOrder"`
	PipelineID   *string        `json:"pipelineId"`
	StageID      *string        `json:"stageId"`
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

const fieldSelect = `
	SELECT cf.id::text, cf.entity, cf.name, cf.internal_key, cf.field_type, cf.description, cf.help_text,
		cf.is_required, cf.is_active, cf.display_order, cf.created_by::text, u.full_name,
		cf.pipeline_id::text, p.name, cf.stage_id::text, ps.name,
		cf.created_at, cf.updated_at
	FROM custom_fields cf
	LEFT JOIN users u ON u.id = cf.created_by
	LEFT JOIN pipelines p ON p.id = cf.pipeline_id
	LEFT JOIN pipeline_stages ps ON ps.id = cf.stage_id
`

func scanField(row pgx.Row) (Field, error) {
	var f Field
	var createdBy, createdByName, pipelineID, pipelineName, stageID, stageName *string
	err := row.Scan(&f.ID, &f.Entity, &f.Name, &f.InternalKey, &f.FieldType, &f.Description, &f.HelpText,
		&f.IsRequired, &f.IsActive, &f.DisplayOrder, &createdBy, &createdByName,
		&pipelineID, &pipelineName, &stageID, &stageName, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return f, err
	}
	f.CreatedBy = createdBy
	f.CreatedByName = createdByName
	f.PipelineID = pipelineID
	f.PipelineName = pipelineName
	f.StageID = stageID
	f.StageName = stageName
	return f, nil
}

func (s *Service) List(ctx context.Context, entity, fieldType string, activeOnly bool, q, pipelineID, stageID string, applyScope bool) ([]Field, error) {
	rows, err := s.pool.Query(ctx, fieldSelect+`
		WHERE ($1 = '' OR cf.entity = $1)
		  AND ($2 = '' OR cf.field_type = $2)
		  AND ($3::bool = false OR cf.is_active = true)
		  AND ($4 = '' OR cf.name ILIKE '%'||$4||'%' OR cf.internal_key ILIKE '%'||$4||'%')
		  AND ($7::bool = false OR (
			(cf.pipeline_id IS NULL AND cf.stage_id IS NULL)
			OR ($5 <> '' AND cf.pipeline_id::text = $5 AND cf.stage_id IS NULL)
			OR ($5 <> '' AND $6 <> '' AND cf.pipeline_id::text = $5 AND cf.stage_id::text = $6)
		  ))
		ORDER BY cf.entity, cf.display_order, cf.name`, entity, fieldType, activeOnly, strings.TrimSpace(q), pipelineID, stageID, applyScope)
	if err != nil {
		return nil, apperrors.Internal("failed to list custom fields", err)
	}
	defer rows.Close()
	var out []Field
	for rows.Next() {
		f, err := scanField(rows)
		if err != nil {
			return nil, apperrors.Internal("scan custom field", err)
		}
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
	f, err := scanField(s.pool.QueryRow(ctx, fieldSelect+` WHERE cf.id=$1::uuid`, id))
	if err != nil {
		return nil, apperrors.NotFound("custom field not found")
	}
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
	pipelineID, stageID := scopeIDs(in.PipelineID, in.StageID)
	if err := s.validateScope(ctx, pipelineID, stageID); err != nil {
		return nil, err
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
			is_required, is_active, display_order, created_by, updated_by, pipeline_id, stage_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::uuid,$10::uuid,$11,$12)
		RETURNING id::text`,
		entity, name, key, ft, strings.TrimSpace(in.Description), strings.TrimSpace(in.HelpText),
		in.IsRequired, active, order, actorID, nullUUID(pipelineID), nullUUID(stageID),
	).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			return nil, apperrors.Conflict("internal key already exists for this pipeline and stage")
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
	pipelineID := ""
	if cur.PipelineID != nil {
		pipelineID = *cur.PipelineID
	}
	stageID := ""
	if cur.StageID != nil {
		stageID = *cur.StageID
	}
	if in.PipelineID != nil {
		pipelineID = strings.TrimSpace(*in.PipelineID)
		if in.StageID == nil {
			stageID = ""
		}
	}
	if in.StageID != nil {
		stageID = strings.TrimSpace(*in.StageID)
	}
	if err := s.validateScope(ctx, pipelineID, stageID); err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, apperrors.Internal("begin tx", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		UPDATE custom_fields SET name=$2, description=$3, help_text=$4, is_required=$5, is_active=$6,
			display_order=$7, updated_by=$8::uuid, pipeline_id=$9, stage_id=$10 WHERE id=$1::uuid`,
		id, name, desc, help, req, active, order, actorID, nullUUID(pipelineID), nullUUID(stageID))
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

func scopeIDs(pipelineID, stageID *string) (string, string) {
	p, s := "", ""
	if pipelineID != nil {
		p = strings.TrimSpace(*pipelineID)
	}
	if stageID != nil {
		s = strings.TrimSpace(*stageID)
	}
	return p, s
}

func nullUUID(id string) any {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	return id
}

func (s *Service) validateScope(ctx context.Context, pipelineID, stageID string) error {
	if stageID != "" && pipelineID == "" {
		return apperrors.Validation("choose a pipeline before a stage")
	}
	if pipelineID != "" {
		var ok bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pipelines WHERE id=$1 AND is_active=TRUE)`, pipelineID).Scan(&ok); err != nil {
			return apperrors.Internal("failed to check pipeline", err)
		}
		if !ok {
			return apperrors.Validation("pipeline is invalid or inactive")
		}
	}
	if stageID != "" {
		var ok bool
		if err := s.pool.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM pipeline_stages WHERE id=$1 AND pipeline_id=$2 AND is_active=TRUE)
		`, stageID, pipelineID).Scan(&ok); err != nil {
			return apperrors.Internal("failed to check stage", err)
		}
		if !ok {
			return apperrors.Validation("stage is not an active stage on this pipeline")
		}
	}
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

func (s *Service) recordScope(ctx context.Context, entity, recordID string) (string, string) {
	table := ""
	switch entity {
	case "lead":
		table = "leads"
	case "customer":
		table = "customers"
	case "deal":
		table = "deals"
	default:
		return "", ""
	}
	var pipelineID, stageID *string
	q := "SELECT pipeline_id::text, stage_id::text FROM " + table + " WHERE id=$1"
	if err := s.pool.QueryRow(ctx, q, recordID).Scan(&pipelineID, &stageID); err != nil {
		return "", ""
	}
	p, st := "", ""
	if pipelineID != nil {
		p = *pipelineID
	}
	if stageID != nil {
		st = *stageID
	}
	return p, st
}

func (s *Service) SetValues(ctx context.Context, actorID, entity, recordID string, values ValueMap) error {
	if !validEntity(entity) {
		return apperrors.Validation("invalid entity")
	}
	pipelineID, stageID := s.recordScope(ctx, entity, recordID)
	defs, err := s.List(ctx, entity, "", true, "", pipelineID, stageID, true)
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

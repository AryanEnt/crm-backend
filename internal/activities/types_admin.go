package activities

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/crm/backend/pkg/apperrors"
)

type TypeCreateInput struct {
	Name              string  `json:"name"`
	Code              string  `json:"code"`
	Description       string  `json:"description"`
	Color             string  `json:"color"`
	Icon              string  `json:"icon"`
	IsActive          *bool   `json:"isActive"`
	Position          *int    `json:"position"`
	RequiresDatetime  bool    `json:"requiresDatetime"`
	RequiresDuration  bool    `json:"requiresDuration"`
	RequiresOutcome   bool    `json:"requiresOutcome"`
	RequiresNotes     bool    `json:"requiresNotes"`
}

type TypeUpdateInput struct {
	Name              *string `json:"name"`
	Description       *string `json:"description"`
	Color             *string `json:"color"`
	Icon              *string `json:"icon"`
	IsActive          *bool   `json:"isActive"`
	Position          *int    `json:"position"`
	RequiresDatetime  *bool   `json:"requiresDatetime"`
	RequiresDuration  *bool   `json:"requiresDuration"`
	RequiresOutcome   *bool   `json:"requiresOutcome"`
	RequiresNotes     *bool   `json:"requiresNotes"`
}

func slugifyTypeCode(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, " ", "_")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" {
		out = "activity_type"
	}
	return out
}

func (r *Repository) TypeByID(ctx context.Context, id string) (*ActivityType, error) {
	var t ActivityType
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, code, name, description, color, icon, is_system, is_active, allows_external, position,
			COALESCE(requires_datetime,false), COALESCE(requires_duration,false),
			COALESCE(requires_outcome,false), COALESCE(requires_notes,false)
		FROM activity_types WHERE id=$1::uuid
	`, id).Scan(&t.ID, &t.Code, &t.Name, &t.Description, &t.Color, &t.Icon,
		&t.IsSystem, &t.IsActive, &t.AllowsExternal, &t.Position,
		&t.RequiresDatetime, &t.RequiresDuration, &t.RequiresOutcome, &t.RequiresNotes)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *Repository) CreateType(ctx context.Context, in TypeCreateInput) (*ActivityType, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, apperrors.Validation("name is required")
	}
	code := strings.TrimSpace(in.Code)
	if code == "" {
		code = slugifyTypeCode(name)
	} else {
		code = slugifyTypeCode(code)
	}
	color := strings.TrimSpace(in.Color)
	if color == "" {
		color = "slate"
	}
	icon := strings.TrimSpace(in.Icon)
	if icon == "" {
		icon = "circle"
	}
	active := true
	if in.IsActive != nil {
		active = *in.IsActive
	}
	pos := 100
	if in.Position != nil {
		pos = *in.Position
	}
	var t ActivityType
	err := r.pool.QueryRow(ctx, `
		INSERT INTO activity_types (code, name, description, color, icon, is_system, is_active, allows_external, position,
			requires_datetime, requires_duration, requires_outcome, requires_notes)
		VALUES ($1,$2,$3,$4,$5,FALSE,$6,FALSE,$7,$8,$9,$10,$11)
		RETURNING id::text, code, name, description, color, icon, is_system, is_active, allows_external, position,
			requires_datetime, requires_duration, requires_outcome, requires_notes
	`, code, name, strings.TrimSpace(in.Description), color, icon, active, pos,
		in.RequiresDatetime, in.RequiresDuration, in.RequiresOutcome, in.RequiresNotes,
	).Scan(&t.ID, &t.Code, &t.Name, &t.Description, &t.Color, &t.Icon,
		&t.IsSystem, &t.IsActive, &t.AllowsExternal, &t.Position,
		&t.RequiresDatetime, &t.RequiresDuration, &t.RequiresOutcome, &t.RequiresNotes)
	if err != nil {
		if strings.Contains(err.Error(), "unique") {
			return nil, apperrors.Conflict("activity type code already exists")
		}
		return nil, err
	}
	return &t, nil
}

func (r *Repository) UpdateType(ctx context.Context, id string, cur *ActivityType, in TypeUpdateInput) (*ActivityType, error) {
	name := cur.Name
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, apperrors.Validation("name is required")
		}
	}
	desc := cur.Description
	if in.Description != nil {
		desc = strings.TrimSpace(*in.Description)
	}
	color := cur.Color
	if in.Color != nil {
		color = strings.TrimSpace(*in.Color)
		if color == "" {
			color = cur.Color
		}
	}
	icon := cur.Icon
	if in.Icon != nil {
		icon = strings.TrimSpace(*in.Icon)
		if icon == "" {
			icon = cur.Icon
		}
	}
	active := cur.IsActive
	if in.IsActive != nil {
		active = *in.IsActive
	}
	pos := cur.Position
	if in.Position != nil {
		pos = *in.Position
	}
	reqDT := cur.RequiresDatetime
	if in.RequiresDatetime != nil {
		reqDT = *in.RequiresDatetime
	}
	reqDur := cur.RequiresDuration
	if in.RequiresDuration != nil {
		reqDur = *in.RequiresDuration
	}
	reqOut := cur.RequiresOutcome
	if in.RequiresOutcome != nil {
		reqOut = *in.RequiresOutcome
	}
	reqNotes := cur.RequiresNotes
	if in.RequiresNotes != nil {
		reqNotes = *in.RequiresNotes
	}

	var t ActivityType
	err := r.pool.QueryRow(ctx, `
		UPDATE activity_types SET
			name=$2, description=$3, color=$4, icon=$5, is_active=$6, position=$7,
			requires_datetime=$8, requires_duration=$9, requires_outcome=$10, requires_notes=$11
		WHERE id=$1::uuid
		RETURNING id::text, code, name, description, color, icon, is_system, is_active, allows_external, position,
			requires_datetime, requires_duration, requires_outcome, requires_notes
	`, id, name, desc, color, icon, active, pos, reqDT, reqDur, reqOut, reqNotes,
	).Scan(&t.ID, &t.Code, &t.Name, &t.Description, &t.Color, &t.Icon,
		&t.IsSystem, &t.IsActive, &t.AllowsExternal, &t.Position,
		&t.RequiresDatetime, &t.RequiresDuration, &t.RequiresOutcome, &t.RequiresNotes)
	if err == pgx.ErrNoRows {
		return nil, apperrors.NotFound("activity type not found")
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *Repository) DeleteType(ctx context.Context, id string) error {
	cur, err := r.TypeByID(ctx, id)
	if err != nil {
		return err
	}
	if cur == nil {
		return apperrors.NotFound("activity type not found")
	}
	var usage int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM activities WHERE kind=$1`, cur.Code).Scan(&usage); err != nil {
		return err
	}
	if usage > 0 {
		return apperrors.Validation("cannot delete activity type in use; deactivate it instead")
	}
	ct, err := r.pool.Exec(ctx, `DELETE FROM activity_types WHERE id=$1::uuid AND is_system=FALSE`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		if cur.IsSystem {
			return apperrors.Validation("system activity types cannot be deleted; deactivate instead")
		}
		return apperrors.NotFound("activity type not found")
	}
	return nil
}

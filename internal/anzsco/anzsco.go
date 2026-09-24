package anzsco

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/crm/backend/pkg/apperrors"
)

type Occupation struct {
	ID              string    `json:"id"`
	Code            string    `json:"code"`
	Title           string    `json:"title"`
	OccupationGroup string    `json:"occupationGroup"`
	SkillLevel      int       `json:"skillLevel"`
	CreatedAt       time.Time `json:"createdAt"`
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Search(ctx context.Context, q string, limit int) ([]Occupation, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	q = strings.TrimSpace(q)
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, code, title, occupation_group, skill_level, created_at
		FROM anzsco_occupations
		WHERE ($1 = '' OR code ILIKE '%' || $1 || '%' OR title ILIKE '%' || $1 || '%'
			OR occupation_group ILIKE '%' || $1 || '%')
		ORDER BY
			CASE WHEN code = $1 THEN 0 WHEN code ILIKE $1 || '%' THEN 1 ELSE 2 END,
			title
		LIMIT $2
	`, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Occupation
	for rows.Next() {
		var o Occupation
		if err := rows.Scan(&o.ID, &o.Code, &o.Title, &o.OccupationGroup, &o.SkillLevel, &o.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, o)
	}
	if items == nil {
		items = []Occupation{}
	}
	return items, rows.Err()
}

func (r *Repository) Exists(ctx context.Context, id string) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM anzsco_occupations WHERE id=$1)`, id).Scan(&ok)
	return ok, err
}

func (r *Repository) Get(ctx context.Context, id string) (*Occupation, error) {
	var o Occupation
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, code, title, occupation_group, skill_level, created_at
		FROM anzsco_occupations WHERE id = $1
	`, id).Scan(&o.ID, &o.Code, &o.Title, &o.OccupationGroup, &o.SkillLevel, &o.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Search(ctx context.Context, q string, limit int) ([]Occupation, error) {
	items, err := s.repo.Search(ctx, q, limit)
	if err != nil {
		return nil, apperrors.Internal("failed to search ANZSCO", err)
	}
	return items, nil
}

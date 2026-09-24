package activities

import (
	"context"
	"errors"

	"github.com/crm/backend/internal/audit"
	"github.com/crm/backend/pkg/apperrors"
)

func (s *Service) AdminListTypes(ctx context.Context) ([]ActivityType, error) {
	items, err := s.repo.ListTypes(ctx, true)
	if err != nil {
		return nil, apperrors.Internal("failed to list activity types", err)
	}
	return items, nil
}

func (s *Service) CreateType(ctx context.Context, actorID string, in TypeCreateInput, ip, ua string) (*ActivityType, error) {
	t, err := s.repo.CreateType(ctx, in)
	if err != nil {
		var appErr *apperrors.AppError
		if errors.As(err, &appErr) {
			return nil, err
		}
		return nil, apperrors.Internal("failed to create activity type", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "activity_type.created", "activity_type", audit.Ptr(t.ID), map[string]any{
		"code": t.Code, "name": t.Name,
	}, ip, ua)
	return t, nil
}

func (s *Service) UpdateType(ctx context.Context, actorID, id string, in TypeUpdateInput, ip, ua string) (*ActivityType, error) {
	cur, err := s.repo.TypeByID(ctx, id)
	if err != nil {
		return nil, apperrors.Internal("failed to load activity type", err)
	}
	if cur == nil {
		return nil, apperrors.NotFound("activity type not found")
	}
	t, err := s.repo.UpdateType(ctx, id, cur, in)
	if err != nil {
		var appErr *apperrors.AppError
		if errors.As(err, &appErr) {
			return nil, err
		}
		return nil, apperrors.Internal("failed to update activity type", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "activity_type.updated", "activity_type", audit.Ptr(id), map[string]any{
		"code": t.Code, "name": t.Name, "isActive": t.IsActive,
	}, ip, ua)
	return t, nil
}

func (s *Service) DeleteType(ctx context.Context, actorID, id string, ip, ua string) error {
	cur, err := s.repo.TypeByID(ctx, id)
	if err != nil {
		return apperrors.Internal("failed to load activity type", err)
	}
	if cur == nil {
		return apperrors.NotFound("activity type not found")
	}
	if err := s.repo.DeleteType(ctx, id); err != nil {
		var appErr *apperrors.AppError
		if errors.As(err, &appErr) {
			return err
		}
		return apperrors.Internal("failed to delete activity type", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "activity_type.deleted", "activity_type", audit.Ptr(id), map[string]any{
		"code": cur.Code, "name": cur.Name,
	}, ip, ua)
	return nil
}

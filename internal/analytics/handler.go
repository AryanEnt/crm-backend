package analytics

import (
	"net/http"

	"github.com/crm/backend/internal/auth"
	"github.com/crm/backend/internal/datascope"
	"github.com/crm/backend/internal/permissions"
	"github.com/crm/backend/pkg/apperrors"
	"github.com/crm/backend/pkg/response"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) filterFromRequest(r *http.Request) (Filter, error) {
	f, _, err := h.scopedFilter(r)
	return f, err
}

func (h *Handler) scopedFilter(r *http.Request) (Filter, permissions.Scope, error) {
	q := map[string]string{}
	for _, k := range []string{
		"from", "to", "pipelineId", "teamId", "ownerUserId", "source", "anzscoId",
		"groupBy", "period", "sortBy", "sortDir",
	} {
		q[k] = r.URL.Query().Get(k)
	}
	f, err := ParseFilter(q)
	if err != nil {
		return f, "", apperrors.Validation(err.Error())
	}
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		return f, "", apperrors.Unauthorized("authentication required")
	}
	rs, err := datascope.ResolveReport(claims, permissions.AnalyticsView, f.TeamID, f.OwnerUserID)
	if err != nil {
		return f, "", err
	}
	f.TeamID = rs.TeamID
	f.OwnerUserID = rs.OwnerUserID
	f.ScopeUserIDs = rs.UserIDs
	f.ScopeTeamIDs = rs.TeamIDs
	return f, rs.Scope, nil
}

func (h *Handler) Funnel(w http.ResponseWriter, r *http.Request) {
	f, err := h.filterFromRequest(r)
	if err != nil {
		response.Fail(w, err)
		return
	}
	res, err := h.service.Funnel(r.Context(), f)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, res)
}

func (h *Handler) Summary(w http.ResponseWriter, r *http.Request) {
	f, err := h.filterFromRequest(r)
	if err != nil {
		response.Fail(w, err)
		return
	}
	res, err := h.service.Summary(r.Context(), f)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, res)
}

func (h *Handler) Leads(w http.ResponseWriter, r *http.Request) {
	f, err := h.filterFromRequest(r)
	if err != nil {
		response.Fail(w, err)
		return
	}
	res, err := h.service.Leads(r.Context(), f)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, res)
}

func (h *Handler) Pipeline(w http.ResponseWriter, r *http.Request) {
	f, err := h.filterFromRequest(r)
	if err != nil {
		response.Fail(w, err)
		return
	}
	res, err := h.service.Pipeline(r.Context(), f)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, res)
}

func (h *Handler) Activities(w http.ResponseWriter, r *http.Request) {
	f, err := h.filterFromRequest(r)
	if err != nil {
		response.Fail(w, err)
		return
	}
	res, err := h.service.Activities(r.Context(), f)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, res)
}

func (h *Handler) Conversions(w http.ResponseWriter, r *http.Request) {
	f, err := h.filterFromRequest(r)
	if err != nil {
		response.Fail(w, err)
		return
	}
	res, err := h.service.Conversions(r.Context(), f)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, res)
}

func (h *Handler) Teams(w http.ResponseWriter, r *http.Request) {
	f, err := h.filterFromRequest(r)
	if err != nil {
		response.Fail(w, err)
		return
	}
	res, err := h.service.Teams(r.Context(), f)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, res)
}

func (h *Handler) Sources(w http.ResponseWriter, r *http.Request) {
	f, err := h.filterFromRequest(r)
	if err != nil {
		response.Fail(w, err)
		return
	}
	res, err := h.service.Sources(r.Context(), f)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, res)
}

// Organization defaults to org-wide totals when no team/owner is chosen, so it
// requires organization scope.
func (h *Handler) Organization(w http.ResponseWriter, r *http.Request) {
	f, scope, err := h.scopedFilter(r)
	if err != nil {
		response.Fail(w, err)
		return
	}
	if scope != permissions.ScopeOrganization {
		response.Fail(w, apperrors.Forbidden("organization analytics requires organization scope"))
		return
	}
	res, err := h.service.Organization(r.Context(), f)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, res)
}

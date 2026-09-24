package activities

import (
	"net/http"
	"strconv"
	"time"

	"github.com/crm/backend/internal/auth"
	"github.com/crm/backend/internal/datascope"
	"github.com/crm/backend/internal/permissions"
	"github.com/crm/backend/pkg/apperrors"
	"github.com/crm/backend/pkg/response"
	"github.com/crm/backend/pkg/validate"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	return r.RemoteAddr
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func (h *Handler) applyListScope(r *http.Request, f *ListFilter, seID string) error {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		return apperrors.Unauthorized("authentication required")
	}
	vis, err := datascope.Resolve(claims, permissions.ActivitiesView, seID)
	if err != nil {
		return err
	}
	if seID == "" && f.OwnerUserID != "" {
		vis, err = datascope.ApplyOwnerFilter(vis, f.OwnerUserID)
		if err != nil {
			return err
		}
	}
	f.ScopeUnscoped = vis.Unscoped
	f.ScopeOwnerIDs = vis.OwnerIDs
	f.ScopeTeamIDs = vis.TeamIDs
	f.OwnerUserID = ""
	if !vis.Unscoped {
		f.TeamID = ""
	}
	return nil
}

func (h *Handler) ListTypes(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.ListTypes(r.Context())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, items)
}

func (h *Handler) AdminListTypes(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.AdminListTypes(r.Context())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, items)
}

func (h *Handler) CreateType(w http.ResponseWriter, r *http.Request) {
	var in TypeCreateInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	item, err := h.service.CreateType(r.Context(), claims.UserID, in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.Created(w, item)
}

func (h *Handler) UpdateType(w http.ResponseWriter, r *http.Request) {
	var in TypeUpdateInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	item, err := h.service.UpdateType(r.Context(), claims.UserID, r.PathValue("id"), in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, item)
}

func (h *Handler) DeleteType(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	if err := h.service.DeleteType(r.Context(), claims.UserID, r.PathValue("id"), clientIP(r), r.UserAgent()); err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, map[string]bool{"deleted": true})
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	var from, to *time.Time
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			t, err = time.Parse("2006-01-02", v)
			if err != nil {
				response.Fail(w, apperrors.Validation("invalid from"))
				return
			}
		}
		u := t.UTC()
		from = &u
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			t, err = time.Parse("2006-01-02", v)
			if err != nil {
				response.Fail(w, apperrors.Validation("invalid to"))
				return
			}
		}
		u := t.UTC()
		to = &u
	}
	seID := firstNonEmpty(q.Get("salesExecutiveId"), q.Get("sales_executive_id"))
	ownerID := q.Get("ownerUserId")
	if seID != "" && ownerID == "" {
		ownerID = seID
	}
	f := ListFilter{
		LeadID: q.Get("leadId"), CustomerID: q.Get("customerId"), DealID: q.Get("dealId"),
		OwnerUserID: ownerID, TeamID: q.Get("teamId"),
		TypeCode: q.Get("type"), PipelineID: q.Get("pipelineId"), Status: q.Get("status"),
		From: from, To: to, Limit: limit, Offset: offset,
	}
	if err := h.applyListScope(r, &f, seID); err != nil {
		response.Fail(w, err)
		return
	}
	items, total, err := h.service.List(r.Context(), f)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{
		Success: true, Data: items, Meta: map[string]any{"total": total, "limit": f.Limit, "offset": f.Offset},
	})
}

func (h *Handler) Calendar(w http.ResponseWriter, r *http.Request) {
	// Calendar is a filtered list over a date range; same as List with required from/to.
	q := r.URL.Query()
	if q.Get("from") == "" || q.Get("to") == "" {
		response.Fail(w, apperrors.Validation("from and to are required"))
		return
	}
	h.List(w, r)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	detail, err := h.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, detail)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var in CreateInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	a, err := h.service.Create(r.Context(), claims.UserID, in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.Created(w, a)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var in UpdateInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	a, err := h.service.Update(r.Context(), claims.UserID, r.PathValue("id"), in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, a)
}

func (h *Handler) FollowUp(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	intel, err := h.service.FollowUp(r.Context(), q.Get("leadId"), q.Get("customerId"), q.Get("dealId"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, intel)
}

func (h *Handler) GetTimezone(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	tz, err := h.service.GetTimezone(r.Context(), claims.UserID)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, map[string]string{"timezone": tz})
}

func (h *Handler) SetTimezone(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Timezone string `json:"timezone"`
	}
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	if err := h.service.SetTimezone(r.Context(), claims.UserID, in.Timezone); err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, map[string]string{"timezone": in.Timezone})
}

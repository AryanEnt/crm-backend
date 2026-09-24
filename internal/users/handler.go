package users

import (
	"net/http"
	"strconv"

	"github.com/crm/backend/internal/auth"
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

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	f := ListFilter{
		Search:   r.URL.Query().Get("q"),
		RoleID:   r.URL.Query().Get("roleId"),
		RoleCode: r.URL.Query().Get("roleCode"),
		TeamID:   firstNonEmpty(r.URL.Query().Get("teamId"), r.URL.Query().Get("team_id")),
		Limit:    limit,
		Offset:   offset,
	}
	if v := r.URL.Query().Get("isActive"); v == "true" || v == "false" {
		b := v == "true"
		f.IsActive = &b
	}
	items, total, err := h.service.List(r.Context(), f)
	if err != nil {
		response.Fail(w, apperrors.Internal("failed to list users", err))
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{
		Success: true,
		Data:    items,
		Meta:    map[string]any{"total": total, "limit": f.Limit, "offset": f.Offset},
	})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	user, err := h.service.Get(r.Context(), id)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, user)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var in CreateInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	user, err := h.service.Create(r.Context(), claims.UserID, in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.Created(w, user)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var in UpdateInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	user, err := h.service.Update(r.Context(), claims.UserID, r.PathValue("id"), in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, user)
}

type statusRequest struct {
	IsActive bool `json:"isActive"`
}

func (h *Handler) SetStatus(w http.ResponseWriter, r *http.Request) {
	var in statusRequest
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	user, err := h.service.SetActive(r.Context(), claims.UserID, r.PathValue("id"), in.IsActive, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, user)
}

type bulkStatusRequest struct {
	IDs      []string `json:"ids"`
	IsActive bool     `json:"isActive"`
}

func (h *Handler) BulkStatus(w http.ResponseWriter, r *http.Request) {
	var in bulkStatusRequest
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	if len(in.IDs) == 0 {
		response.Fail(w, apperrors.Validation("ids are required"))
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	n, err := h.service.BulkSetActive(r.Context(), claims.UserID, in.IDs, in.IsActive, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, map[string]any{"updated": n})
}

func (h *Handler) ListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := h.service.ListRoles(r.Context())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, roles)
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

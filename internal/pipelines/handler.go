package pipelines

import (
	"net/http"
	"strconv"

	"github.com/crm/backend/internal/auth"
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

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	includeInactive, _ := strconv.ParseBool(r.URL.Query().Get("includeInactive"))
	items, err := h.service.List(r.Context(), r.URL.Query().Get("kind"), includeInactive)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, items)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	p, err := h.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, p)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var in CreatePipelineInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	p, err := h.service.Create(r.Context(), claims.UserID, in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.Created(w, p)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var in UpdatePipelineInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	p, err := h.service.Update(r.Context(), claims.UserID, r.PathValue("id"), in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, p)
}

func (h *Handler) CreateStage(w http.ResponseWriter, r *http.Request) {
	var in CreateStageInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	st, err := h.service.CreateStage(r.Context(), claims.UserID, r.PathValue("id"), in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.Created(w, st)
}

func (h *Handler) UpdateStage(w http.ResponseWriter, r *http.Request) {
	var in UpdateStageInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	st, err := h.service.UpdateStage(r.Context(), claims.UserID, r.PathValue("id"), in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, st)
}

func (h *Handler) ReorderStages(w http.ResponseWriter, r *http.Request) {
	var in ReorderStagesInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	p, err := h.service.ReorderStages(r.Context(), claims.UserID, r.PathValue("id"), in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, p)
}

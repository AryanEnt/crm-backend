package leadsources

import (
	"net/http"

	"github.com/crm/backend/internal/auth"
	"github.com/crm/backend/pkg/response"
	"github.com/crm/backend/pkg/validate"
)

type Handler struct {
	service *Service
}

func NewHandler(s *Service) *Handler { return &Handler{service: s} }

func clientIP(r *http.Request) string {
	if x := r.Header.Get("X-Forwarded-For"); x != "" {
		return x
	}
	return r.RemoteAddr
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	activeOnly := q.Get("activeOnly") == "true" || q.Get("active") == "true"
	items, err := h.service.List(r.Context(), activeOnly, q.Get("q"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, items)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var in CreateInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	item, err := h.service.Create(r.Context(), claims.UserID, in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.Created(w, item)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var in UpdateInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	item, err := h.service.Update(r.Context(), claims.UserID, r.PathValue("id"), in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, item)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	if err := h.service.Delete(r.Context(), claims.UserID, r.PathValue("id"), clientIP(r), r.UserAgent()); err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, map[string]bool{"deleted": true})
}

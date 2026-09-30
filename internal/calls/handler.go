package calls

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

func claimsFrom(r *http.Request) (auth.Claims, error) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		return auth.Claims{}, apperrors.Unauthorized("authentication required")
	}
	return claims, nil
}

func (h *Handler) Contacts(w http.ResponseWriter, r *http.Request) {
	claims, err := claimsFrom(r)
	if err != nil {
		response.Fail(w, err)
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	items, err := h.service.Contacts(r.Context(), claims, ContactFilter{
		Search: q.Get("q"), Type: q.Get("type"), Limit: limit,
	})
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, items)
}

func (h *Handler) Context(w http.ResponseWriter, r *http.Request) {
	claims, err := claimsFrom(r)
	if err != nil {
		response.Fail(w, err)
		return
	}
	q := r.URL.Query()
	out, err := h.service.Context(r.Context(), claims, q.Get("type"), q.Get("id"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, out)
}

func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
	claims, err := claimsFrom(r)
	if err != nil {
		response.Fail(w, err)
		return
	}
	q := r.URL.Query()
	items, err := h.service.History(r.Context(), claims, q.Get("type"), q.Get("id"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, items)
}

func (h *Handler) ListNotes(w http.ResponseWriter, r *http.Request) {
	claims, err := claimsFrom(r)
	if err != nil {
		response.Fail(w, err)
		return
	}
	q := r.URL.Query()
	items, err := h.service.ListNotes(r.Context(), claims, q.Get("type"), q.Get("id"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, items)
}

func (h *Handler) CreateNote(w http.ResponseWriter, r *http.Request) {
	claims, err := claimsFrom(r)
	if err != nil {
		response.Fail(w, err)
		return
	}
	var in CreateNoteInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	n, err := h.service.CreateNote(r.Context(), claims, in)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.Created(w, n)
}

func (h *Handler) UpdateNote(w http.ResponseWriter, r *http.Request) {
	claims, err := claimsFrom(r)
	if err != nil {
		response.Fail(w, err)
		return
	}
	var in UpdateNoteInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	n, err := h.service.UpdateNote(r.Context(), claims, r.PathValue("id"), in)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, n)
}

func (h *Handler) DeleteNote(w http.ResponseWriter, r *http.Request) {
	claims, err := claimsFrom(r)
	if err != nil {
		response.Fail(w, err)
		return
	}
	if err := h.service.DeleteNote(r.Context(), claims, r.PathValue("id")); err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, map[string]any{"deleted": true})
}

package systemactivity

import (
	"net/http"
	"strconv"

	"github.com/crm/backend/pkg/response"
)

type Handler struct{ service *Service }

func NewHandler(s *Service) *Handler { return &Handler{service: s} }

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	items, total, err := h.service.List(r.Context(), q.Get("q"), q.Get("eventType"), q.Get("entityType"), q.Get("result"), q.Get("actorUserId"), limit, offset)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{
		Success: true,
		Data:    items,
		Meta:    map[string]any{"total": total, "limit": limit, "offset": offset},
	})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	item, err := h.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, item)
}

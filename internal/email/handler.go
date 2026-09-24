package email

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

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

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	response.OK(w, h.service.Health(r.Context()))
}

func (h *Handler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	items, err := h.service.ListAccounts(r.Context(), claims.UserID)
	if err != nil {
		response.Fail(w, err)
		return
	}
	health := h.service.Health(r.Context())
	response.OK(w, map[string]any{
		"accounts":    items,
		"integration": health,
	})
}

func (h *Handler) Connect(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	var in struct {
		RedirectTo string `json:"redirectTo"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	url, err := h.service.StartConnect(r.Context(), claims, in.RedirectTo)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, map[string]string{"url": url})
}

func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	loc, err := h.service.OAuthCallback(r.Context(), q.Get("code"), q.Get("state"), q.Get("error"))
	if loc == "" {
		loc = "/"
	}
	if err != nil {
		http.Redirect(w, r, loc, http.StatusFound)
		return
	}
	http.Redirect(w, r, loc, http.StatusFound)
}

func (h *Handler) Disconnect(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	id := r.PathValue("id")
	if err := h.service.Disconnect(r.Context(), claims, id, clientIP(r), r.UserAgent()); err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, map[string]bool{"disconnected": true})
}

func (h *Handler) Sync(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	if err := h.service.SyncAccount(r.Context(), claims, r.PathValue("accountId")); err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, map[string]bool{"synced": true})
}

func (h *Handler) PubSub(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err := h.service.HandlePubSub(r.Context(), body); err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, map[string]bool{"ok": true})
}

func (h *Handler) ListThreads(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	items, total, counts, err := h.service.ListThreads(r.Context(), claims, ThreadListFilter{
		Folder:      q.Get("folder"),
		Q:           q.Get("q"),
		LeadID:      q.Get("leadId"),
		CustomerID:  q.Get("customerId"),
		OwnerUserID: q.Get("salesExecutiveId"),
		Direction:   q.Get("direction"),
		Status:      q.Get("status"),
		HasAttach:   q.Get("hasAttachments"),
		Unread:      q.Get("unread"),
		DateFrom:    q.Get("dateFrom"),
		DateTo:      q.Get("dateTo"),
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		response.Fail(w, err)
		return
	}
	if limit <= 0 {
		limit = 40
	}
	response.OK(w, map[string]any{
		"threads": items,
		"total":   total,
		"limit":   limit,
		"offset":  offset,
		"counts":  counts,
	})
}

func (h *Handler) GetThread(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	d, err := h.service.GetThread(r.Context(), claims, r.PathValue("id"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, d)
}

func (h *Handler) Associate(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	var in AssociateInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	t, err := h.service.Associate(r.Context(), claims, r.PathValue("id"), in)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, t)
}

func (h *Handler) PatchThread(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	var in struct {
		Starred  *bool `json:"starred"`
		Archived *bool `json:"archived"`
	}
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	t, err := h.service.PatchThread(r.Context(), claims, r.PathValue("id"), in.Starred, in.Archived)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, t)
}

func (h *Handler) Compose(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	var in ComposeInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	m, err := h.service.Compose(r.Context(), claims, in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.Created(w, m)
}

func (h *Handler) PatchDraft(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	var in DraftPatch
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	m, err := h.service.PatchDraft(r.Context(), claims, r.PathValue("id"), in)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, m)
}

func (h *Handler) Send(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	m, err := h.service.SendExisting(r.Context(), claims, r.PathValue("id"), clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, m)
}

func (h *Handler) Preview(w http.ResponseWriter, r *http.Request) {
	var in PreviewInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	p, err := h.service.Preview(r.Context(), in.Subject, in.BodyHTML, in.BodyText, in.LeadID, in.CustomerID)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, p)
}

func (h *Handler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	items, err := h.service.ListTemplates(r.Context(), claims)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, items)
}

func (h *Handler) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFromContext(r.Context())
	var in TemplateInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	t, err := h.service.CreateTemplate(r.Context(), claims, in)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.Created(w, t)
}

func (h *Handler) UpdateTemplate(w http.ResponseWriter, r *http.Request) {
	var in TemplateInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	t, err := h.service.UpdateTemplate(r.Context(), r.PathValue("id"), in)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, t)
}

func (h *Handler) DeleteTemplate(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteTemplate(r.Context(), r.PathValue("id")); err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, map[string]bool{"deleted": true})
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	return r.RemoteAddr
}

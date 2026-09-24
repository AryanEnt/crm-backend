package communications

import (
	"io"
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

func (h *Handler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.ListAccounts(r.Context(), r.URL.Query().Get("provider"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, items)
}

func (h *Handler) CreateAccount(w http.ResponseWriter, r *http.Request) {
	var in CreateAccountInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	a, err := h.service.CreateAccount(r.Context(), claims.UserID, in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: a})
}

func (h *Handler) ListWhatsApp(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	items, total, err := h.service.ListWhatsApp(r.Context(), q.Get("customerId"), q.Get("dealId"), q.Get("leadId"), limit, offset)
	if err != nil {
		response.Fail(w, err)
		return
	}
	if limit <= 0 {
		limit = 50
	}
	response.JSON(w, http.StatusOK, response.Envelope{
		Success: true, Data: items,
		Meta: map[string]any{"total": total, "limit": limit, "offset": offset},
	})
}

func (h *Handler) SendWhatsApp(w http.ResponseWriter, r *http.Request) {
	var in SendWhatsAppInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	msg, err := h.service.SendWhatsApp(r.Context(), claims.UserID, in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: msg})
}

func (h *Handler) ListCalls(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	items, total, err := h.service.ListCalls(r.Context(), q.Get("customerId"), q.Get("dealId"), q.Get("leadId"), limit, offset)
	if err != nil {
		response.Fail(w, err)
		return
	}
	if limit <= 0 {
		limit = 50
	}
	response.JSON(w, http.StatusOK, response.Envelope{
		Success: true, Data: items,
		Meta: map[string]any{"total": total, "limit": limit, "offset": offset},
	})
}

func (h *Handler) PlaceCall(w http.ResponseWriter, r *http.Request) {
	var in PlaceCallInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	call, err := h.service.PlaceCall(r.Context(), claims.UserID, in, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: call})
}

// Meta WhatsApp webhook verification (GET)
func (h *Handler) MetaWhatsAppVerify(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("hub.mode")
	token := r.URL.Query().Get("hub.verify_token")
	challenge := r.URL.Query().Get("hub.challenge")
	out, ok, err := h.service.VerifyMetaWhatsApp(r.Context(), mode, token, challenge)
	if err != nil {
		response.Fail(w, err)
		return
	}
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(out))
}

// Meta WhatsApp webhook events (POST)
func (h *Handler) MetaWhatsAppWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := h.service.HandleMetaWhatsAppWebhook(r, body); err != nil {
		response.Fail(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"success":true}`))
}

// Twilio voice status callback (POST form)
func (h *Handler) TwilioVoiceStatus(w http.ResponseWriter, r *http.Request) {
	if err := h.service.HandleTwilioStatus(r); err != nil {
		response.Fail(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Response></Response>`))
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	return r.RemoteAddr
}

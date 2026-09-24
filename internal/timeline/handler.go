package timeline

import (
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

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	if limit <= 0 {
		limit = 30
	}
	var types []string
	if raw := q.Get("types"); raw != "" {
		for _, t := range strings.Split(raw, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				types = append(types, t)
			}
		}
	}
	if single := q.Get("type"); single != "" {
		types = append(types, single)
	}
	items, total, err := h.service.List(r.Context(), ListFilter{
		CustomerID: q.Get("customerId"),
		DealID:     q.Get("dealId"),
		LeadID:     q.Get("leadId"),
		EventTypes: types,
		Source:     q.Get("source"),
		Limit:      limit,
		Offset:     offset,
	})
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{
		Success: true,
		Data:    items,
		Meta: map[string]any{
			"total": total, "limit": limit, "offset": offset,
			"hasMore": offset+len(items) < total,
		},
	})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	ev, err := h.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, ev)
}

// WriteExternal allows future WhatsApp/Twilio/email/automation systems to append events.
func (h *Handler) WriteExternal(w http.ResponseWriter, r *http.Request) {
	var in ExternalWriteInput
	if err := validate.DecodeJSON(r, &in); err != nil {
		response.Fail(w, err)
		return
	}
	claims, ok := auth.ClaimsFromContext(r.Context())
	actorID := ""
	if ok {
		actorID = claims.UserID
	}
	ev, err := h.service.WriteExternal(r.Context(), actorID, in)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.Created(w, ev)
}

func (h *Handler) EventTypes(w http.ResponseWriter, r *http.Request) {
	response.OK(w, []map[string]string{
		{"code": EventLeadCreated, "label": "Lead created"},
		{"code": EventCustomerCreated, "label": "Customer created"},
		{"code": EventDealCreated, "label": "Deal created"},
		{"code": EventActivityCreated, "label": "Activity created"},
		{"code": EventActivityCompleted, "label": "Activity completed"},
		{"code": EventCall, "label": "Call"},
		{"code": EventWhatsApp, "label": "WhatsApp message"},
		{"code": EventEmail, "label": "Email"},
		{"code": EventMeeting, "label": "Meeting"},
		{"code": EventNote, "label": "Note"},
		{"code": EventStageChange, "label": "Stage change"},
		{"code": EventAssignmentChange, "label": "Assignment change"},
		{"code": EventDocumentRequested, "label": "Document requested"},
		{"code": EventDocumentUploaded, "label": "Document uploaded"},
		{"code": EventDocumentVerified, "label": "Document verified"},
		{"code": EventDocumentRejected, "label": "Document rejected"},
		{"code": EventAutomation, "label": "Automation event"},
	})
}

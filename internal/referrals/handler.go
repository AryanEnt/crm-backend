package referrals

import (
	"net/http"
	"strconv"

	"github.com/crm/backend/internal/auth"
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

func (h *Handler) Meta(w http.ResponseWriter, r *http.Request) {
	m, err := h.service.Meta(r.Context())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, m)
}

func (h *Handler) Summary(w http.ResponseWriter, r *http.Request) {
	f := h.listFilter(r)
	m, err := h.service.Summary(r.Context(), f)
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, m)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	f := h.listFilter(r)
	items, total, err := h.service.List(r.Context(), f)
	if err != nil {
		response.Fail(w, err)
		return
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	response.JSON(w, http.StatusOK, response.Envelope{
		Success: true,
		Data:    items,
		Meta:    map[string]any{"total": total, "limit": limit, "offset": f.Offset},
	})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	ref, err := h.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, ref)
}

func (h *Handler) GetBySubject(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	leadID := q.Get("leadId")
	customerID := q.Get("customerId")
	var ref *Referral
	var err error
	switch {
	case leadID != "":
		ref, err = h.service.GetForLead(r.Context(), leadID)
	case customerID != "":
		ref, err = h.service.GetForCustomer(r.Context(), customerID)
	default:
		response.Fail(w, apperrors.Validation("leadId or customerId is required"))
		return
	}
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, ref)
}

type createBody struct {
	LeadID     *string `json:"leadId"`
	CustomerID *string `json:"customerId"`
	Input
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var body createBody
	if err := validate.DecodeJSON(r, &body); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	ref, err := h.service.Create(r.Context(), claims.UserID, body.LeadID, body.CustomerID, body.Input, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.Created(w, ref)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var body UpdateInput
	if err := validate.DecodeJSON(r, &body); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	ref, err := h.service.Update(r.Context(), claims.UserID, r.PathValue("id"), body, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, ref)
}

func (h *Handler) ReferrerProfile(w http.ResponseWriter, r *http.Request) {
	p, err := h.service.ReferrerProfile(r.Context(), r.PathValue("type"), r.PathValue("id"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, p)
}

func (h *Handler) ListPartners(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.ListPartners(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.OK(w, items)
}

func (h *Handler) CreatePartner(w http.ResponseWriter, r *http.Request) {
	var body CreatePartnerInput
	if err := validate.DecodeJSON(r, &body); err != nil {
		response.Fail(w, err)
		return
	}
	claims, _ := auth.ClaimsFromContext(r.Context())
	p, err := h.service.CreatePartner(r.Context(), claims.UserID, body, clientIP(r), r.UserAgent())
	if err != nil {
		response.Fail(w, err)
		return
	}
	response.Created(w, p)
}

func (h *Handler) listFilter(r *http.Request) ListFilter {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	f := ListFilter{
		Search:             q.Get("q"),
		OwnerUserID:        q.Get("ownerUserId"),
		TeamID:             q.Get("teamId"),
		PipelineID:         q.Get("pipelineId"),
		ReferrerTypeCode:   q.Get("referrerType"),
		ReferrerUserID:     q.Get("referrerUserId"),
		ReferrerCustomerID: q.Get("referrerCustomerId"),
		ReferrerPartnerID:  q.Get("referrerPartnerId"),
		ReferrerName:       q.Get("referrer"),
		StatusCode:         q.Get("status"),
		DateFrom:           q.Get("from"),
		DateTo:             q.Get("to"),
		Sort:               q.Get("sort"),
		Order:              q.Get("order"),
		Limit:              limit,
		Offset:             offset,
	}

	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		return f
	}
	perms := permissions.ExpandImplies(claims.Permissions)
	if perms.Has(permissions.ReferralsManage) || claims.RoleCode == permissions.RoleSuperAdmin {
		f.Unscoped = true
		return f
	}
	// Managers/executives: own records + team memberships
	f.ScopeOwnerIDs = []string{claims.UserID}
	f.ScopeTeamIDs = claims.TeamIDs
	return f
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	return r.RemoteAddr
}

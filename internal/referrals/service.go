package referrals

import (
	"context"
	"strings"
	"time"

	"github.com/crm/backend/internal/audit"
	"github.com/crm/backend/pkg/apperrors"
)

type Service struct {
	repo  *Repository
	audit *audit.Service
}

func NewService(repo *Repository, auditSvc *audit.Service) *Service {
	return &Service{repo: repo, audit: auditSvc}
}

func (s *Service) Meta(ctx context.Context) (*Meta, error) {
	m, err := s.repo.ListCatalog(ctx)
	if err != nil {
		return nil, apperrors.Internal("failed to load referral catalogs", err)
	}
	return m, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Referral, error) {
	ref, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, apperrors.Internal("failed to load referral", err)
	}
	if ref == nil {
		return nil, apperrors.NotFound("referral not found")
	}
	return ref, nil
}

func (s *Service) GetForLead(ctx context.Context, leadID string) (*Referral, error) {
	ref, err := s.repo.GetByLeadID(ctx, leadID)
	if err != nil {
		return nil, apperrors.Internal("failed to load referral", err)
	}
	return ref, nil
}

func (s *Service) GetForCustomer(ctx context.Context, customerID string) (*Referral, error) {
	ref, err := s.repo.GetByCustomerID(ctx, customerID)
	if err != nil {
		return nil, apperrors.Internal("failed to load referral", err)
	}
	return ref, nil
}

func (s *Service) List(ctx context.Context, f ListFilter) ([]Referral, int, error) {
	items, total, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, 0, apperrors.Internal("failed to list referrals", err)
	}
	return items, total, nil
}

func (s *Service) Summary(ctx context.Context, f ListFilter) (*SummaryMetrics, error) {
	m, err := s.repo.Summary(ctx, f)
	if err != nil {
		return nil, apperrors.Internal("failed to load referral summary", err)
	}
	return m, nil
}

func (s *Service) ReferrerProfile(ctx context.Context, typ, id string) (*ReferrerProfile, error) {
	p, err := s.repo.ReferrerProfile(ctx, typ, id)
	if err != nil {
		return nil, apperrors.Validation(err.Error())
	}
	return p, nil
}

func (s *Service) ListPartners(ctx context.Context, q string) ([]Partner, error) {
	items, err := s.repo.ListPartners(ctx, q, 50)
	if err != nil {
		return nil, apperrors.Internal("failed to list partners", err)
	}
	return items, nil
}

func (s *Service) ListReferrerUsers(ctx context.Context, q string) ([]ReferrerUser, error) {
	items, err := s.repo.ListReferrerUsers(ctx, strings.TrimSpace(q), 20)
	if err != nil {
		return nil, apperrors.Internal("failed to list referrer users", err)
	}
	return items, nil
}

func (s *Service) CreatePartner(ctx context.Context, actorID string, in CreatePartnerInput, ip, ua string) (*Partner, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, apperrors.Validation("partner name is required")
	}
	p, err := s.repo.CreatePartner(ctx, in)
	if err != nil {
		return nil, apperrors.Internal("failed to create partner", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "referral_partner.created", "referral_partner", audit.Ptr(p.ID), map[string]any{
		"name": p.Name,
	}, ip, ua)
	return p, nil
}

// AttachToLead creates a referral for a lead. Call when lead source is Referral.
func (s *Service) AttachToLead(ctx context.Context, actorID, leadID string, in Input, ip, ua string) (*Referral, error) {
	if err := ValidateRequired(in); err != nil {
		return nil, err
	}
	row, err := s.resolveInput(ctx, in, &leadID, nil, actorID)
	if err != nil {
		return nil, err
	}
	ref, err := s.repo.Create(ctx, *row)
	if err != nil {
		return nil, apperrors.Internal("failed to create referral", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "referral.created", "referral", audit.Ptr(ref.ID), map[string]any{
		"leadId": leadID, "referrerType": ref.ReferrerTypeCode, "referrer": ref.ReferrerDisplayName,
	}, ip, ua)
	return ref, nil
}

// AttachToCustomer creates a referral for a customer.
func (s *Service) AttachToCustomer(ctx context.Context, actorID, customerID string, in Input, ip, ua string) (*Referral, error) {
	if err := ValidateRequired(in); err != nil {
		return nil, err
	}
	row, err := s.resolveInput(ctx, in, nil, &customerID, actorID)
	if err != nil {
		return nil, err
	}
	ref, err := s.repo.Create(ctx, *row)
	if err != nil {
		return nil, apperrors.Internal("failed to create referral", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "referral.created", "referral", audit.Ptr(ref.ID), map[string]any{
		"customerId": customerID, "referrerType": ref.ReferrerTypeCode, "referrer": ref.ReferrerDisplayName,
	}, ip, ua)
	return ref, nil
}

func (s *Service) Create(ctx context.Context, actorID string, leadID, customerID *string, in Input, ip, ua string) (*Referral, error) {
	if (leadID == nil || *leadID == "") && (customerID == nil || *customerID == "") {
		return nil, apperrors.Validation("leadId or customerId is required")
	}
	if err := ValidateRequired(in); err != nil {
		return nil, err
	}
	row, err := s.resolveInput(ctx, in, leadID, customerID, actorID)
	if err != nil {
		return nil, err
	}
	ref, err := s.repo.Create(ctx, *row)
	if err != nil {
		return nil, apperrors.Internal("failed to create referral", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "referral.created", "referral", audit.Ptr(ref.ID), map[string]any{
		"leadId": leadID, "customerId": customerID, "referrerType": ref.ReferrerTypeCode,
	}, ip, ua)
	return ref, nil
}

func (s *Service) Update(ctx context.Context, actorID, id string, in UpdateInput, ip, ua string) (*Referral, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	merged := Input{
		ReferrerTypeCode:   current.ReferrerTypeCode,
		ReferrerUserID:     current.ReferrerUserID,
		ReferrerCustomerID: current.ReferrerCustomerID,
		ReferrerPartnerID:  current.ReferrerPartnerID,
		ReferrerName:       current.ReferrerName,
		RelationshipCode:   current.RelationshipCode,
		ReferralDate:       &current.ReferralDate,
		ReferralSource:     current.ReferralSource,
		Notes:              current.Notes,
		ReferralCode:       current.ReferralCode,
		StatusCode:         current.StatusCode,
	}
	prevStatus := current.StatusCode
	prevReferrer := current.ReferrerDisplayName

	if in.ReferrerTypeCode != nil {
		merged.ReferrerTypeCode = *in.ReferrerTypeCode
	}
	if in.ReferrerUserID != nil {
		merged.ReferrerUserID = in.ReferrerUserID
	}
	if in.ReferrerCustomerID != nil {
		merged.ReferrerCustomerID = in.ReferrerCustomerID
	}
	if in.ReferrerPartnerID != nil {
		merged.ReferrerPartnerID = in.ReferrerPartnerID
	}
	if in.ReferrerName != nil {
		merged.ReferrerName = *in.ReferrerName
	}
	if in.RelationshipCode != nil {
		merged.RelationshipCode = in.RelationshipCode
	}
	if in.ReferralDate != nil {
		merged.ReferralDate = in.ReferralDate
	}
	if in.ReferralSource != nil {
		merged.ReferralSource = *in.ReferralSource
	}
	if in.Notes != nil {
		merged.Notes = *in.Notes
	}
	if in.ReferralCode != nil {
		merged.ReferralCode = in.ReferralCode
	}
	if in.StatusCode != nil {
		merged.StatusCode = *in.StatusCode
	}

	if err := ValidateRequired(merged); err != nil {
		return nil, err
	}

	leadID := current.LeadID
	customerID := current.CustomerID
	if in.LeadID != nil {
		leadID = in.LeadID
	}
	if in.CustomerID != nil {
		customerID = in.CustomerID
	}

	row, err := s.resolveInput(ctx, merged, leadID, customerID, actorID)
	if err != nil {
		return nil, err
	}
	ref, err := s.repo.Update(ctx, id, *row)
	if err != nil {
		return nil, apperrors.Internal("failed to update referral", err)
	}

	meta := map[string]any{}
	if prevStatus != ref.StatusCode {
		meta["fromStatus"] = prevStatus
		meta["toStatus"] = ref.StatusCode
		_ = s.audit.Record(ctx, audit.Ptr(actorID), "referral.status_changed", "referral", audit.Ptr(id), meta, ip, ua)
	}
	if prevReferrer != ref.ReferrerDisplayName {
		_ = s.audit.Record(ctx, audit.Ptr(actorID), "referral.referrer_changed", "referral", audit.Ptr(id), map[string]any{
			"from": prevReferrer, "to": ref.ReferrerDisplayName,
		}, ip, ua)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "referral.updated", "referral", audit.Ptr(id), map[string]any{
		"referrerType": ref.ReferrerTypeCode,
	}, ip, ua)
	return ref, nil
}

// MarkLeadConverted links the customer and sets status to converted.
func (s *Service) MarkLeadConverted(ctx context.Context, actorID, leadID, customerID, ip, ua string) error {
	status, err := s.repo.StatusByCode(ctx, "converted")
	if err != nil || status == nil {
		return apperrors.Internal("converted status missing", err)
	}
	existing, err := s.repo.GetByLeadID(ctx, leadID)
	if err != nil {
		return apperrors.Internal("failed to load referral", err)
	}
	if existing == nil {
		return nil
	}
	if err := s.repo.AttachCustomer(ctx, leadID, customerID, status.ID); err != nil {
		return apperrors.Internal("failed to update referral on convert", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "referral.status_changed", "referral", audit.Ptr(existing.ID), map[string]any{
		"fromStatus": existing.StatusCode, "toStatus": "converted", "customerId": customerID,
	}, ip, ua)
	return nil
}

func ValidateRequired(in Input) error {
	if strings.TrimSpace(in.ReferrerTypeCode) == "" {
		return apperrors.Validation("referrer type is required")
	}
	hasLink := (in.ReferrerUserID != nil && strings.TrimSpace(*in.ReferrerUserID) != "") ||
		(in.ReferrerCustomerID != nil && strings.TrimSpace(*in.ReferrerCustomerID) != "") ||
		(in.ReferrerPartnerID != nil && strings.TrimSpace(*in.ReferrerPartnerID) != "") ||
		strings.TrimSpace(in.ReferrerName) != ""
	if !hasLink {
		return apperrors.Validation("referred by is required")
	}
	return nil
}

func IsReferralSource(source string) bool {
	return strings.EqualFold(strings.TrimSpace(source), "referral")
}

func (s *Service) resolveInput(ctx context.Context, in Input, leadID, customerID *string, actorID string) (*createRow, error) {
	typ, err := s.repo.TypeByCode(ctx, in.ReferrerTypeCode)
	if err != nil || typ == nil {
		return nil, apperrors.Validation("invalid referrer type")
	}

	statusCode := in.StatusCode
	if statusCode == "" {
		statusCode = "active"
	}
	status, err := s.repo.StatusByCode(ctx, statusCode)
	if err != nil || status == nil {
		return nil, apperrors.Validation("invalid referral status")
	}

	var relID *string
	if in.RelationshipCode != nil && strings.TrimSpace(*in.RelationshipCode) != "" {
		rel, err := s.repo.RelationshipByCode(ctx, *in.RelationshipCode)
		if err != nil || rel == nil {
			return nil, apperrors.Validation("invalid referral relationship")
		}
		relID = &rel.ID
	}

	date := time.Now().UTC()
	if in.ReferralDate != nil && strings.TrimSpace(*in.ReferralDate) != "" {
		parsed, err := time.Parse("2006-01-02", strings.TrimSpace(*in.ReferralDate))
		if err != nil {
			return nil, apperrors.Validation("invalid referral date (use YYYY-MM-DD)")
		}
		date = parsed
	}

	// Clear conflicting links based on type for consistency
	userID, custID, partnerID := in.ReferrerUserID, in.ReferrerCustomerID, in.ReferrerPartnerID
	switch typ.Code {
	case "existing_customer":
		userID, partnerID = nil, nil
		if custID == nil || strings.TrimSpace(*custID) == "" {
			if strings.TrimSpace(in.ReferrerName) == "" {
				return nil, apperrors.Validation("referrer customer is required for Existing Customer type")
			}
		}
	case "sales_executive", "employee":
		custID, partnerID = nil, nil
		if userID == nil || strings.TrimSpace(*userID) == "" {
			if strings.TrimSpace(in.ReferrerName) == "" {
				return nil, apperrors.Validation("referrer user is required for this referrer type")
			}
		}
	case "partner", "agent":
		userID, custID = nil, nil
		// partner link optional if name provided
	case "other":
		// name required (already checked)
	}

	return &createRow{
		LeadID:             leadID,
		CustomerID:         customerID,
		ReferrerTypeID:     typ.ID,
		ReferrerUserID:     userID,
		ReferrerCustomerID: custID,
		ReferrerPartnerID:  partnerID,
		ReferrerName:       strings.TrimSpace(in.ReferrerName),
		RelationshipID:     relID,
		ReferralDate:       date,
		ReferralSource:     strings.TrimSpace(in.ReferralSource),
		Notes:              in.Notes,
		StatusID:           status.ID,
		ReferralCode:       in.ReferralCode,
		CreatedByUserID:    audit.Ptr(actorID),
	}, nil
}

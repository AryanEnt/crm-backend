package referrals

import "time"

type CatalogItem struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsActive    bool   `json:"isActive"`
	SortOrder   int    `json:"sortOrder"`
	IsConverted bool   `json:"isConverted,omitempty"`
}

type Partner struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     *string   `json:"email"`
	Phone     *string   `json:"phone"`
	Company   string    `json:"company"`
	Notes     string    `json:"notes"`
	IsActive  bool      `json:"isActive"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Referral struct {
	ID                   string     `json:"id"`
	LeadID               *string    `json:"leadId"`
	CustomerID           *string    `json:"customerId"`
	ReferrerTypeID       string     `json:"referrerTypeId"`
	ReferrerTypeCode     string     `json:"referrerTypeCode"`
	ReferrerTypeName     string     `json:"referrerTypeName"`
	ReferrerUserID       *string    `json:"referrerUserId"`
	ReferrerUserName     *string    `json:"referrerUserName"`
	ReferrerCustomerID   *string    `json:"referrerCustomerId"`
	ReferrerCustomerName *string    `json:"referrerCustomerName"`
	ReferrerPartnerID    *string    `json:"referrerPartnerId"`
	ReferrerPartnerName  *string    `json:"referrerPartnerName"`
	ReferrerName         string     `json:"referrerName"`
	ReferrerDisplayName  string     `json:"referrerDisplayName"`
	RelationshipID       *string    `json:"relationshipId"`
	RelationshipCode     *string    `json:"relationshipCode"`
	RelationshipName     *string    `json:"relationshipName"`
	ReferralDate         string     `json:"referralDate"`
	ReferralSource       string     `json:"referralSource"`
	Notes                string     `json:"notes"`
	StatusID             string     `json:"statusId"`
	StatusCode           string     `json:"statusCode"`
	StatusName           string     `json:"statusName"`
	IsConverted          bool       `json:"isConverted"`
	ReferralCode         *string    `json:"referralCode"`
	CreatedByUserID      *string    `json:"createdByUserId"`
	// Denormalized subject fields for admin table
	ReferredName         *string    `json:"referredName,omitempty"`
	OwnerUserID          *string    `json:"ownerUserId,omitempty"`
	OwnerName            *string    `json:"ownerName,omitempty"`
	TeamID               *string    `json:"teamId,omitempty"`
	PipelineID           *string    `json:"pipelineId,omitempty"`
	PipelineName         *string    `json:"pipelineName,omitempty"`
	StageID              *string    `json:"stageId,omitempty"`
	StageName            *string    `json:"stageName,omitempty"`
	PotentialValue       *float64   `json:"potentialValue,omitempty"`
	SubjectKind          string     `json:"subjectKind,omitempty"` // lead | customer
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
}

type Meta struct {
	ReferrerTypes  []CatalogItem `json:"referrerTypes"`
	Relationships  []CatalogItem `json:"relationships"`
	Statuses       []CatalogItem `json:"statuses"`
}

type SummaryMetrics struct {
	TotalReferrals       int      `json:"totalReferrals"`
	ActiveReferrals      int      `json:"activeReferrals"`
	ConvertedReferrals   int      `json:"convertedReferrals"`
	UnconvertedReferrals int      `json:"unconvertedReferrals"`
	ReferralPipelineValue float64 `json:"referralPipelineValue"`
}

type ReferrerProfile struct {
	ReferrerKey          string     `json:"referrerKey"`
	ReferrerTypeCode     string     `json:"referrerTypeCode"`
	ReferrerTypeName     string     `json:"referrerTypeName"`
	ReferrerUserID       *string    `json:"referrerUserId"`
	ReferrerCustomerID   *string    `json:"referrerCustomerId"`
	ReferrerPartnerID    *string    `json:"referrerPartnerId"`
	ReferrerDisplayName  string     `json:"referrerDisplayName"`
	TotalReferrals       int        `json:"totalReferrals"`
	ActiveReferrals      int        `json:"activeReferrals"`
	ConvertedReferrals   int        `json:"convertedReferrals"`
	History              []Referral `json:"history"`
}

// Input is used when attaching a referral to a lead/customer (nested on create).
type Input struct {
	ReferrerTypeCode   string  `json:"referrerTypeCode"`
	ReferrerUserID     *string `json:"referrerUserId"`
	ReferrerCustomerID *string `json:"referrerCustomerId"`
	ReferrerPartnerID  *string `json:"referrerPartnerId"`
	ReferrerName       string  `json:"referrerName"`
	RelationshipCode   *string `json:"relationshipCode"`
	ReferralDate       *string `json:"referralDate"` // YYYY-MM-DD
	ReferralSource     string  `json:"referralSource"`
	Notes              string  `json:"notes"`
	ReferralCode       *string `json:"referralCode"`
	StatusCode         string  `json:"statusCode"`
}

type UpdateInput struct {
	ReferrerTypeCode   *string `json:"referrerTypeCode"`
	ReferrerUserID     *string `json:"referrerUserId"`
	ReferrerCustomerID *string `json:"referrerCustomerId"`
	ReferrerPartnerID  *string `json:"referrerPartnerId"`
	ReferrerName       *string `json:"referrerName"`
	RelationshipCode   *string `json:"relationshipCode"`
	ReferralDate       *string `json:"referralDate"`
	ReferralSource     *string `json:"referralSource"`
	Notes              *string `json:"notes"`
	ReferralCode       *string `json:"referralCode"`
	StatusCode         *string `json:"statusCode"`
	LeadID             *string `json:"leadId"`
	CustomerID         *string `json:"customerId"`
}

type ListFilter struct {
	Search             string
	OwnerUserID        string
	TeamID             string
	PipelineID         string
	ReferrerTypeCode   string
	ReferrerUserID     string
	ReferrerCustomerID string
	ReferrerPartnerID  string
	ReferrerName       string
	StatusCode         string
	DateFrom           string
	DateTo             string
	// Scope: when not manage, restrict to these owner IDs / team IDs
	ScopeOwnerIDs []string
	ScopeTeamIDs  []string
	Unscoped      bool // referrals:manage
	Sort          string
	Order         string
	Limit         int
	Offset        int
}

type CreatePartnerInput struct {
	Name    string  `json:"name"`
	Email   *string `json:"email"`
	Phone   *string `json:"phone"`
	Company string  `json:"company"`
	Notes   string  `json:"notes"`
}

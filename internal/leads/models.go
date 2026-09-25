package leads

import (
	"time"

	"github.com/crm/backend/internal/referrals"
)

type Lead struct {
	ID                  string     `json:"id"`
	FullName            string     `json:"fullName"`
	Email               *string    `json:"email"`
	Phone               *string    `json:"phone"`
	Country             string     `json:"country"`
	Nationality         string     `json:"nationality"`
	Location            string     `json:"location"`
	OwnerUserID         *string    `json:"ownerUserId"`
	OwnerName           *string    `json:"ownerName"`
	TeamID              *string    `json:"teamId"`
	TeamName            *string    `json:"teamName"`
	Source              string     `json:"source"`
	Priority            string     `json:"priority"`
	Tags                []string   `json:"tags"`
	AnzscoID            *string    `json:"anzscoId"`
	AnzscoCode          *string    `json:"anzscoCode"`
	AnzscoTitle         *string    `json:"anzscoTitle"`
	PipelineID          *string    `json:"pipelineId"`
	PipelineName        *string    `json:"pipelineName"`
	StageID             *string    `json:"stageId"`
	StageName           *string    `json:"stageName"`
	Notes               string     `json:"notes"`
	Occupation          string     `json:"occupation"`
	JobTitle            string     `json:"jobTitle"`
	Employer            string     `json:"employer"`
	ExperienceYears     *float64   `json:"experienceYears"`
	Qualification       string     `json:"qualification"`
	Skills              []string   `json:"skills"`
	PotentialValue      *float64   `json:"potentialValue"`
	ExpectedOutcome     string     `json:"expectedOutcome"`
	LastActivityAt      *time.Time `json:"lastActivityAt"`
	NextActivityAt      *time.Time `json:"nextActivityAt"`
	Status              string     `json:"status"`
	ConvertedCustomerID *string    `json:"convertedCustomerId"`
	ConvertedAt         *time.Time `json:"convertedAt"`
	IsArchived          bool       `json:"isArchived"`
	ArchivedAt          *time.Time `json:"archivedAt"`
	AgeDays             int        `json:"ageDays"`
	Attention           string     `json:"attention"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
}

type CreateInput struct {
	FullName        string         `json:"fullName"`
	Email           *string        `json:"email"`
	Phone           *string        `json:"phone"`
	Country         string         `json:"country"`
	Nationality     string         `json:"nationality"`
	Location        string         `json:"location"`
	OwnerUserID     *string        `json:"ownerUserId"`
	TeamID          *string        `json:"teamId"`
	Source          string         `json:"source"`
	Priority        string         `json:"priority"`
	Tags            []string       `json:"tags"`
	AnzscoID        *string        `json:"anzscoId"`
	PipelineID      *string        `json:"pipelineId"`
	StageID         *string        `json:"stageId"`
	Notes           string         `json:"notes"`
	Occupation      string         `json:"occupation"`
	JobTitle        string         `json:"jobTitle"`
	Employer        string         `json:"employer"`
	ExperienceYears *float64       `json:"experienceYears"`
	Qualification   string         `json:"qualification"`
	Skills          []string       `json:"skills"`
	PotentialValue  *float64       `json:"potentialValue"`
	ExpectedOutcome string         `json:"expectedOutcome"`
	NextActivityAt  *string        `json:"nextActivityAt"`
	ForceCreate     bool           `json:"forceCreate"`
	CustomFields    map[string]any `json:"customFields"`
	// When Source is "Referral", Referral is required (structured — not free-text referredBy).
	Referral *referrals.Input `json:"referral"`
}

type UpdateInput struct {
	FullName        *string        `json:"fullName"`
	Email           *string        `json:"email"`
	Phone           *string        `json:"phone"`
	Country         *string        `json:"country"`
	Nationality     *string        `json:"nationality"`
	Location        *string        `json:"location"`
	OwnerUserID     *string        `json:"ownerUserId"`
	TeamID          *string        `json:"teamId"`
	Source          *string        `json:"source"`
	Priority        *string        `json:"priority"`
	Tags            []string       `json:"tags"`
	AnzscoID        *string        `json:"anzscoId"`
	PipelineID      *string        `json:"pipelineId"`
	StageID         *string        `json:"stageId"`
	Notes           *string        `json:"notes"`
	Occupation      *string        `json:"occupation"`
	JobTitle        *string        `json:"jobTitle"`
	Employer        *string        `json:"employer"`
	ExperienceYears *float64       `json:"experienceYears"`
	Qualification   *string        `json:"qualification"`
	Skills          []string       `json:"skills"`
	PotentialValue  *float64       `json:"potentialValue"`
	ExpectedOutcome *string        `json:"expectedOutcome"`
	NextActivityAt  *string        `json:"nextActivityAt"`
	Status          *string        `json:"status"`
	ForceUpdate     bool           `json:"forceUpdate"`
	CustomFields    map[string]any `json:"customFields"`
}

type ListFilter struct {
	Search          string
	OwnerUserID     string
	TeamID          string
	PipelineID      string
	StageID         string
	Source          string
	Priority        string
	AnzscoID        string
	Tag             string
	CreatedFrom     string
	CreatedTo       string
	InactiveDays    int
	IncludeArchived bool
	Status          string
	Attention       string
	Sort            string
	Order           string
	Limit           int
	Offset          int
	// Authorization visibility (set by handler, never trusted from client alone)
	ScopeUnscoped bool
	ScopeOwnerIDs []string
	ScopeTeamIDs  []string
}

type DuplicateMatch struct {
	ID       string  `json:"id"`
	FullName string  `json:"fullName"`
	Email    *string `json:"email"`
	Phone    *string `json:"phone"`
	Entity   string  `json:"entity"` // lead | customer
	Reason   string  `json:"reason"`
}

type CreateResult struct {
	Lead        *Lead            `json:"lead,omitempty"`
	Duplicates  []DuplicateMatch `json:"duplicates,omitempty"`
	NeedsReview bool             `json:"needsReview"`
}

type BulkAssignInput struct {
	IDs         []string `json:"ids"`
	OwnerUserID *string  `json:"ownerUserId"`
	TeamID      *string  `json:"teamId"`
}

type BulkArchiveInput struct {
	IDs     []string `json:"ids"`
	Archive bool     `json:"archive"`
}

type BulkStageInput struct {
	IDs        []string `json:"ids"`
	PipelineID string   `json:"pipelineId"`
	StageID    string   `json:"stageId"`
}

type ConvertInput struct {
	Force bool `json:"force"`
}

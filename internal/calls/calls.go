// Package calls powers the Calls workspace: preparing for and recording a
// conversation with a lead or customer. It reads existing CRM records and the
// unified timeline, and owns only conversation notes.
package calls

import (
	"time"
)

const (
	EntityLead     = "lead"
	EntityCustomer = "customer"

	// Timeline rows mirroring a note use this provider with external_id = note id.
	timelineProvider = "calls"

	maxNoteLength = 5000
)

// Contact is one row in the workspace selector (a lead or a customer).
type Contact struct {
	Type      string    `json:"type"`
	ID        string    `json:"id"`
	FullName  string    `json:"fullName"`
	Email     *string   `json:"email"`
	Phone     *string   `json:"phone"`
	Employer  string    `json:"employer"`
	StageName *string   `json:"stageName"`
	Priority  string    `json:"priority"`
	OwnerName *string   `json:"ownerName"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ContactFilter struct {
	Search string
	Type   string // "", lead, customer
	Limit  int
}

// Person is the selected record, normalised across leads and customers.
// Empty strings / nil mean "not recorded"; the UI hides them.
type Person struct {
	Type            string     `json:"type"`
	ID              string     `json:"id"`
	FullName        string     `json:"fullName"`
	Email           *string    `json:"email"`
	Phone           *string    `json:"phone"`
	Employer        string     `json:"employer"`
	JobTitle        string     `json:"jobTitle"`
	Occupation      string     `json:"occupation"`
	AnzscoCode      *string    `json:"anzscoCode"`
	AnzscoTitle     *string    `json:"anzscoTitle"`
	Qualification   string     `json:"qualification"`
	ExperienceYears *float64   `json:"experienceYears"`
	Skills          []string   `json:"skills"`
	Country         string     `json:"country"`
	Location        string     `json:"location"`
	Source          string     `json:"source"`
	Priority        string     `json:"priority"`
	Status          string     `json:"status"`
	PipelineName    *string    `json:"pipelineName"`
	StageName       *string    `json:"stageName"`
	OwnerUserID     *string    `json:"ownerUserId"`
	OwnerName       *string    `json:"ownerName"`
	TeamName        *string    `json:"teamName"`
	ExpectedOutcome string     `json:"expectedOutcome"`
	PotentialValue  *float64   `json:"potentialValue"`
	Notes           string     `json:"notes"`
	LastContactAt   *time.Time `json:"lastContactAt"`
	NextFollowUpAt  *time.Time `json:"nextFollowUpAt"`
	Tags            []string   `json:"tags"`
	CreatedAt       time.Time  `json:"createdAt"`
}

type DealSummary struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	Status          string   `json:"status"`
	StageName       *string  `json:"stageName"`
	Value           *float64 `json:"value"`
	Currency        string   `json:"currency"`
	ExpectedCloseAt *string  `json:"expectedCloseAt"`
	LostReason      string   `json:"lostReason"`
}

type UpcomingActivity struct {
	ID       string     `json:"id"`
	Title    string     `json:"title"`
	Kind     string     `json:"kind"`
	TypeName *string    `json:"typeName"`
	Status   string     `json:"status"`
	DueAt    *time.Time `json:"dueAt"`
}

type Interaction struct {
	EventType  string    `json:"eventType"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	OccurredAt time.Time `json:"occurredAt"`
	ActorName  *string   `json:"actorName"`
}

type DocumentSummary struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	DocType string `json:"docType"`
	Status  string `json:"status"`
}

// Context is everything the workspace shows about the selected person,
// loaded in one round trip.
type Context struct {
	Person          *Person            `json:"person"`
	Deals           []DealSummary      `json:"deals"`
	Upcoming        []UpcomingActivity `json:"upcoming"`
	LastInteraction *Interaction       `json:"lastInteraction"`
	Documents       []DocumentSummary  `json:"documents"`
}

type Note struct {
	ID           string    `json:"id"`
	LeadID       *string   `json:"leadId"`
	CustomerID   *string   `json:"customerId"`
	Topic        string    `json:"topic"`
	Content      string    `json:"content"`
	AuthorUserID *string   `json:"authorUserId"`
	AuthorName   *string   `json:"authorName"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type CreateNoteInput struct {
	EntityType string `json:"entityType"`
	EntityID   string `json:"entityId"`
	Topic      string `json:"topic"`
	Content    string `json:"content"`
}

type UpdateNoteInput struct {
	Topic   *string `json:"topic"`
	Content *string `json:"content"`
}

// entityType / entityID of a stored note.
func (n *Note) subject() (string, string) {
	if n.LeadID != nil {
		return EntityLead, *n.LeadID
	}
	if n.CustomerID != nil {
		return EntityCustomer, *n.CustomerID
	}
	return "", ""
}

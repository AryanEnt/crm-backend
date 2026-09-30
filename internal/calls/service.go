package calls

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/crm/backend/internal/auth"
	"github.com/crm/backend/internal/customers"
	"github.com/crm/backend/internal/datascope"
	"github.com/crm/backend/internal/leads"
	"github.com/crm/backend/internal/permissions"
	"github.com/crm/backend/internal/timeline"
	"github.com/crm/backend/pkg/apperrors"
)

// store is the persistence the service needs; *Repository implements it.
type store interface {
	Contacts(ctx context.Context, f ContactFilter, leadVis, customerVis *datascope.Visibility) ([]Contact, error)
	InScope(ctx context.Context, entityType, id string, vis datascope.Visibility) (bool, error)
	Deals(ctx context.Context, customerID string) ([]DealSummary, error)
	Upcoming(ctx context.Context, entityType, id string) ([]UpcomingActivity, error)
	LastInteraction(ctx context.Context, entityType, id string) (*Interaction, error)
	Documents(ctx context.Context, entityType, id string) ([]DocumentSummary, error)
	ListNotes(ctx context.Context, entityType, id string) ([]Note, error)
	GetNote(ctx context.Context, id string) (*Note, error)
	CreateNote(ctx context.Context, entityType, entityID, topic, content, authorID string) (*Note, error)
	UpdateNote(ctx context.Context, id, topic, content string) (*Note, error)
	DeleteNote(ctx context.Context, id string) error
}

type leadReader interface {
	Get(ctx context.Context, id string) (*leads.Lead, error)
}

type customerReader interface {
	Get(ctx context.Context, id string) (*customers.Customer, error)
}

type timelineReader interface {
	List(ctx context.Context, f timeline.ListFilter) ([]timeline.Event, int, error)
}

type Service struct {
	repo      store
	leads     leadReader
	customers customerReader
	timeline  timelineReader
}

func NewService(repo store, leads leadReader, customers customerReader, tl timelineReader) *Service {
	return &Service{repo: repo, leads: leads, customers: customers, timeline: tl}
}

var topicPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

func viewPermission(entityType string) (string, error) {
	switch entityType {
	case EntityLead:
		return permissions.LeadsView, nil
	case EntityCustomer:
		return permissions.CustomersView, nil
	default:
		return "", apperrors.Validation("type must be lead or customer")
	}
}

func hasPermission(claims auth.Claims, code string) bool {
	return permissions.ExpandImplies(claims.Permissions).Has(code)
}

// authorize confirms the caller may view the record. Missing and out-of-scope
// records both return not found so the API does not reveal which records exist.
func (s *Service) authorize(ctx context.Context, claims auth.Claims, entityType, id string) error {
	perm, err := viewPermission(entityType)
	if err != nil {
		return err
	}
	if _, err := uuid.Parse(id); err != nil {
		return apperrors.Validation("id must be a valid UUID")
	}
	vis, err := datascope.Resolve(claims, perm, "")
	if err != nil {
		return err
	}
	ok, err := s.repo.InScope(ctx, entityType, id, vis)
	if err != nil {
		return apperrors.Internal("failed to check record access", err)
	}
	if !ok {
		return apperrors.NotFound(entityType + " not found")
	}
	return nil
}

func (s *Service) Contacts(ctx context.Context, claims auth.Claims, f ContactFilter) ([]Contact, error) {
	if f.Type != "" && f.Type != EntityLead && f.Type != EntityCustomer {
		return nil, apperrors.Validation("type must be lead or customer")
	}
	var leadVis, customerVis *datascope.Visibility
	if hasPermission(claims, permissions.LeadsView) {
		v, err := datascope.Resolve(claims, permissions.LeadsView, "")
		if err != nil {
			return nil, err
		}
		leadVis = &v
	}
	if hasPermission(claims, permissions.CustomersView) {
		v, err := datascope.Resolve(claims, permissions.CustomersView, "")
		if err != nil {
			return nil, err
		}
		customerVis = &v
	}
	if leadVis == nil && customerVis == nil {
		return nil, apperrors.Forbidden("insufficient permissions")
	}
	items, err := s.repo.Contacts(ctx, f, leadVis, customerVis)
	if err != nil {
		return nil, apperrors.Internal("failed to list contacts", err)
	}
	return items, nil
}

func (s *Service) Context(ctx context.Context, claims auth.Claims, entityType, id string) (*Context, error) {
	if err := s.authorize(ctx, claims, entityType, id); err != nil {
		return nil, err
	}
	person, err := s.person(ctx, entityType, id)
	if err != nil {
		return nil, err
	}
	out := &Context{Person: person, Deals: []DealSummary{}}
	if entityType == EntityCustomer {
		if out.Deals, err = s.repo.Deals(ctx, id); err != nil {
			return nil, apperrors.Internal("failed to load deals", err)
		}
	}
	if out.Upcoming, err = s.repo.Upcoming(ctx, entityType, id); err != nil {
		return nil, apperrors.Internal("failed to load upcoming activities", err)
	}
	if out.LastInteraction, err = s.repo.LastInteraction(ctx, entityType, id); err != nil {
		return nil, apperrors.Internal("failed to load last interaction", err)
	}
	if out.Documents, err = s.repo.Documents(ctx, entityType, id); err != nil {
		return nil, apperrors.Internal("failed to load documents", err)
	}
	return out, nil
}

func (s *Service) person(ctx context.Context, entityType, id string) (*Person, error) {
	if entityType == EntityLead {
		l, err := s.leads.Get(ctx, id)
		if err != nil {
			return nil, apperrors.Internal("failed to load lead", err)
		}
		if l == nil {
			return nil, apperrors.NotFound("lead not found")
		}
		return &Person{
			Type: EntityLead, ID: l.ID, FullName: l.FullName, Email: l.Email, Phone: l.Phone,
			Employer: l.Employer, JobTitle: l.JobTitle, Occupation: l.Occupation,
			AnzscoCode: l.AnzscoCode, AnzscoTitle: l.AnzscoTitle, Qualification: l.Qualification,
			ExperienceYears: l.ExperienceYears, Skills: l.Skills, Country: l.Country, Location: l.Location,
			Source: l.Source, Priority: l.Priority, Status: l.Status,
			PipelineName: l.PipelineName, StageName: l.StageName,
			OwnerUserID: l.OwnerUserID, OwnerName: l.OwnerName, TeamName: l.TeamName,
			ExpectedOutcome: l.ExpectedOutcome, PotentialValue: l.PotentialValue, Notes: l.Notes,
			LastContactAt: l.LastActivityAt, NextFollowUpAt: l.NextActivityAt, Tags: l.Tags,
			CreatedAt: l.CreatedAt,
		}, nil
	}
	c, err := s.customers.Get(ctx, id)
	if err != nil {
		return nil, apperrors.Internal("failed to load customer", err)
	}
	if c == nil {
		return nil, apperrors.NotFound("customer not found")
	}
	return &Person{
		Type: EntityCustomer, ID: c.ID, FullName: c.FullName, Email: c.Email, Phone: c.Phone,
		Employer: c.Employer, JobTitle: c.JobTitle, Occupation: c.Occupation,
		AnzscoCode: c.AnzscoCode, AnzscoTitle: c.AnzscoTitle, Qualification: c.Qualification,
		ExperienceYears: c.ExperienceYears, Skills: c.Skills, Country: c.Country, Location: c.Location,
		Source: c.Source, Priority: c.Priority,
		PipelineName: c.PipelineName, StageName: c.StageName,
		OwnerUserID: c.OwnerUserID, OwnerName: c.OwnerName, TeamName: c.TeamName,
		ExpectedOutcome: c.ExpectedOutcome, PotentialValue: c.PotentialValue, Notes: c.Notes,
		LastContactAt: c.LastContactedAt, NextFollowUpAt: c.NextFollowUpAt, Tags: c.Tags,
		CreatedAt: c.CreatedAt,
	}, nil
}

const historyLimit = 50

// History returns the person's timeline, newest first. For a customer converted
// from a lead, the lead's earlier events are included.
func (s *Service) History(ctx context.Context, claims auth.Claims, entityType, id string) ([]timeline.Event, error) {
	if err := s.authorize(ctx, claims, entityType, id); err != nil {
		return nil, err
	}
	filter := timeline.ListFilter{Limit: historyLimit}
	if entityType == EntityLead {
		filter.LeadID = id
	} else {
		filter.CustomerID = id
	}
	items, _, err := s.timeline.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	if entityType != EntityCustomer {
		return items, nil
	}
	c, err := s.customers.Get(ctx, id)
	if err != nil {
		return nil, apperrors.Internal("failed to load customer", err)
	}
	if c == nil || c.ConvertedFromLeadID == nil {
		return items, nil
	}
	earlier, _, err := s.timeline.List(ctx, timeline.ListFilter{LeadID: *c.ConvertedFromLeadID, Limit: historyLimit})
	if err != nil {
		return nil, err
	}
	return mergeEvents(items, earlier, historyLimit), nil
}

func mergeEvents(a, b []timeline.Event, limit int) []timeline.Event {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]timeline.Event, 0, len(a)+len(b))
	for _, list := range [][]timeline.Event{a, b} {
		for _, e := range list {
			if !seen[e.ID] {
				seen[e.ID] = true
				out = append(out, e)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].OccurredAt.After(out[j].OccurredAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Service) ListNotes(ctx context.Context, claims auth.Claims, entityType, id string) ([]Note, error) {
	if err := s.authorize(ctx, claims, entityType, id); err != nil {
		return nil, err
	}
	items, err := s.repo.ListNotes(ctx, entityType, id)
	if err != nil {
		return nil, apperrors.Internal("failed to list notes", err)
	}
	return items, nil
}

func normaliseNote(topic, content string) (string, string, error) {
	topic = strings.TrimSpace(topic)
	content = strings.TrimSpace(content)
	if !topicPattern.MatchString(topic) {
		return "", "", apperrors.Validation("choose a topic for this note")
	}
	if content == "" {
		return "", "", apperrors.Validation("write something before saving the note")
	}
	if len([]rune(content)) > maxNoteLength {
		return "", "", apperrors.Validation("notes can be up to 5000 characters")
	}
	return topic, content, nil
}

func (s *Service) CreateNote(ctx context.Context, claims auth.Claims, in CreateNoteInput) (*Note, error) {
	if !hasPermission(claims, permissions.ActivitiesCreate) {
		return nil, apperrors.Forbidden("insufficient permissions")
	}
	topic, content, err := normaliseNote(in.Topic, in.Content)
	if err != nil {
		return nil, err
	}
	if err := s.authorize(ctx, claims, in.EntityType, in.EntityID); err != nil {
		return nil, err
	}
	n, err := s.repo.CreateNote(ctx, in.EntityType, in.EntityID, topic, content, claims.UserID)
	if err != nil {
		return nil, apperrors.Internal("failed to save note", err)
	}
	return n, nil
}

// ownNote loads a note the caller may modify: they must still see the record
// and must be the note's author.
func (s *Service) ownNote(ctx context.Context, claims auth.Claims, id string) (*Note, error) {
	if !hasPermission(claims, permissions.ActivitiesEdit) {
		return nil, apperrors.Forbidden("insufficient permissions")
	}
	if _, err := uuid.Parse(id); err != nil {
		return nil, apperrors.Validation("id must be a valid UUID")
	}
	n, err := s.repo.GetNote(ctx, id)
	if err != nil {
		return nil, apperrors.Internal("failed to load note", err)
	}
	if n == nil {
		return nil, apperrors.NotFound("note not found")
	}
	entityType, entityID := n.subject()
	if err := s.authorize(ctx, claims, entityType, entityID); err != nil {
		return nil, apperrors.NotFound("note not found")
	}
	if n.AuthorUserID == nil || *n.AuthorUserID != claims.UserID {
		return nil, apperrors.Forbidden("you can only change notes you wrote")
	}
	return n, nil
}

func (s *Service) UpdateNote(ctx context.Context, claims auth.Claims, id string, in UpdateNoteInput) (*Note, error) {
	current, err := s.ownNote(ctx, claims, id)
	if err != nil {
		return nil, err
	}
	topic, content := current.Topic, current.Content
	if in.Topic != nil {
		topic = *in.Topic
	}
	if in.Content != nil {
		content = *in.Content
	}
	topic, content, err = normaliseNote(topic, content)
	if err != nil {
		return nil, err
	}
	n, err := s.repo.UpdateNote(ctx, id, topic, content)
	if err != nil {
		return nil, apperrors.Internal("failed to update note", err)
	}
	return n, nil
}

func (s *Service) DeleteNote(ctx context.Context, claims auth.Claims, id string) error {
	if _, err := s.ownNote(ctx, claims, id); err != nil {
		return err
	}
	if err := s.repo.DeleteNote(ctx, id); err != nil {
		return apperrors.Internal("failed to delete note", err)
	}
	return nil
}

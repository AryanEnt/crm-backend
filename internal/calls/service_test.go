package calls

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/crm/backend/internal/auth"
	"github.com/crm/backend/internal/datascope"
	"github.com/crm/backend/internal/permissions"
	"github.com/crm/backend/pkg/apperrors"
)

type record struct {
	entityType string
	owner      string
	team       string
}

// fakeStore keeps records and notes in memory and applies visibility the way
// datascope.AppendWhere does for owner/team columns.
type fakeStore struct {
	records map[string]record
	notes   map[string]*Note
	creates int
}

func newFakeStore() *fakeStore {
	return &fakeStore{records: map[string]record{}, notes: map[string]*Note{}}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (f *fakeStore) InScope(_ context.Context, entityType, id string, vis datascope.Visibility) (bool, error) {
	rec, ok := f.records[id]
	if !ok || rec.entityType != entityType {
		return false, nil
	}
	switch {
	case vis.Unscoped:
		return true, nil
	case len(vis.OwnerIDs) > 0:
		return contains(vis.OwnerIDs, rec.owner) && (len(vis.TeamIDs) == 0 || contains(vis.TeamIDs, rec.team)), nil
	case len(vis.TeamIDs) > 0:
		return contains(vis.TeamIDs, rec.team), nil
	}
	return false, nil
}

func (f *fakeStore) CreateNote(_ context.Context, entityType, entityID, topic, content, authorID string) (*Note, error) {
	f.creates++
	id := uuid.NewString()
	now := time.Now()
	n := &Note{ID: id, Topic: topic, Content: content, AuthorUserID: &authorID, CreatedAt: now, UpdatedAt: now}
	if entityType == EntityLead {
		n.LeadID = &entityID
	} else {
		n.CustomerID = &entityID
	}
	f.notes[id] = n
	return n, nil
}

func (f *fakeStore) GetNote(_ context.Context, id string) (*Note, error) { return f.notes[id], nil }

func (f *fakeStore) UpdateNote(_ context.Context, id, topic, content string) (*Note, error) {
	n := f.notes[id]
	n.Topic, n.Content = topic, content
	return n, nil
}

func (f *fakeStore) DeleteNote(_ context.Context, id string) error {
	delete(f.notes, id)
	return nil
}

func (f *fakeStore) ListNotes(_ context.Context, entityType, id string) ([]Note, error) {
	out := []Note{}
	for _, n := range f.notes {
		if t, e := n.subject(); t == entityType && e == id {
			out = append(out, *n)
		}
	}
	return out, nil
}

func (f *fakeStore) Contacts(context.Context, ContactFilter, *datascope.Visibility, *datascope.Visibility) ([]Contact, error) {
	return []Contact{}, nil
}
func (f *fakeStore) Deals(context.Context, string) ([]DealSummary, error) { return nil, nil }
func (f *fakeStore) Upcoming(context.Context, string, string) ([]UpcomingActivity, error) {
	return nil, nil
}
func (f *fakeStore) LastInteraction(context.Context, string, string) (*Interaction, error) {
	return nil, nil
}
func (f *fakeStore) Documents(context.Context, string, string) ([]DocumentSummary, error) {
	return nil, nil
}

var (
	seRyan    = uuid.NewString()
	seAnsu    = uuid.NewString()
	seOther   = uuid.NewString()
	teamLead  = uuid.NewString()
	admin     = uuid.NewString()
	teamA     = uuid.NewString()
	teamB     = uuid.NewString()
	leadRyan  = uuid.NewString()
	leadAnsu  = uuid.NewString()
	leadOther = uuid.NewString()
	custRyan  = uuid.NewString()
)

func seed() *fakeStore {
	s := newFakeStore()
	s.records[leadRyan] = record{EntityLead, seRyan, teamA}
	s.records[leadAnsu] = record{EntityLead, seAnsu, teamA}
	s.records[leadOther] = record{EntityLead, seOther, teamB}
	s.records[custRyan] = record{EntityCustomer, seRyan, teamA}
	return s
}

func salesExecutive(id string) auth.Claims {
	return auth.Claims{
		UserID: id, RoleCode: permissions.RoleSalesExecutive, TeamIDs: []string{teamA},
		Permissions: []string{
			permissions.LeadsView, permissions.CustomersView, permissions.ActivitiesView,
			permissions.ActivitiesCreate, permissions.ActivitiesEdit,
		},
		PermissionScopes: map[string]permissions.Scope{
			permissions.LeadsView: permissions.ScopeOwn, permissions.CustomersView: permissions.ScopeOwn,
		},
	}
}

func teamLeadClaims() auth.Claims {
	return auth.Claims{
		UserID: teamLead, RoleCode: permissions.RoleSalesManager,
		TeamIDs: []string{teamA}, TeamMemberUserIDs: []string{teamLead, seRyan, seAnsu},
		Permissions: []string{
			permissions.LeadsView, permissions.CustomersView, permissions.ActivitiesView, permissions.ActivitiesEdit,
		},
		PermissionScopes: map[string]permissions.Scope{
			permissions.LeadsView: permissions.ScopeTeam, permissions.CustomersView: permissions.ScopeTeam,
		},
	}
}

func superAdmin() auth.Claims {
	return auth.Claims{
		UserID: admin, RoleCode: permissions.RoleSuperAdmin,
		Permissions: []string{
			permissions.LeadsView, permissions.CustomersView, permissions.ActivitiesView,
			permissions.ActivitiesCreate, permissions.ActivitiesEdit,
		},
		PermissionScopes: map[string]permissions.Scope{
			permissions.LeadsView: permissions.ScopeOrganization, permissions.CustomersView: permissions.ScopeOrganization,
		},
	}
}

func expectCode(t *testing.T, err error, code apperrors.Code) {
	t.Helper()
	ae, ok := apperrors.AsAppError(err)
	if !ok || ae.Code != code {
		t.Fatalf("expected %s, got %#v", code, err)
	}
}

func TestCreateNote_OwnLeadIsSavedWithAuthorAndTopic(t *testing.T) {
	store := seed()
	svc := NewService(store, nil, nil, nil)
	n, err := svc.CreateNote(context.Background(), salesExecutive(seRyan), CreateNoteInput{
		EntityType: EntityLead, EntityID: leadRyan, Topic: " budget ", Content: "  Around 40k, flexible  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if n.Topic != "budget" || n.Content != "Around 40k, flexible" {
		t.Fatalf("note not normalised: %#v", n)
	}
	if n.AuthorUserID == nil || *n.AuthorUserID != seRyan {
		t.Fatalf("author should be the caller, got %v", n.AuthorUserID)
	}
	if n.LeadID == nil || *n.LeadID != leadRyan || n.CustomerID != nil {
		t.Fatalf("note should reference only the lead: %#v", n)
	}
}

func TestCreateNote_OwnCustomer(t *testing.T) {
	svc := NewService(seed(), nil, nil, nil)
	n, err := svc.CreateNote(context.Background(), salesExecutive(seRyan), CreateNoteInput{
		EntityType: EntityCustomer, EntityID: custRyan, Topic: "next_steps", Content: "Send checklist",
	})
	if err != nil {
		t.Fatal(err)
	}
	if n.CustomerID == nil || *n.CustomerID != custRyan || n.LeadID != nil {
		t.Fatalf("note should reference only the customer: %#v", n)
	}
}

func TestCreateNote_SalesExecutiveCannotWriteOnAnotherExecutivesLead(t *testing.T) {
	store := seed()
	svc := NewService(store, nil, nil, nil)
	_, err := svc.CreateNote(context.Background(), salesExecutive(seRyan), CreateNoteInput{
		EntityType: EntityLead, EntityID: leadAnsu, Topic: "budget", Content: "x",
	})
	expectCode(t, err, apperrors.CodeNotFound)
	if store.creates != 0 {
		t.Fatal("note must not be written for an out-of-scope record")
	}
}

func TestCreateNote_RequiresActivitiesCreate(t *testing.T) {
	svc := NewService(seed(), nil, nil, nil)
	_, err := svc.CreateNote(context.Background(), teamLeadClaims(), CreateNoteInput{
		EntityType: EntityLead, EntityID: leadRyan, Topic: "budget", Content: "x",
	})
	expectCode(t, err, apperrors.CodeForbidden)
}

func TestCreateNote_Validation(t *testing.T) {
	svc := NewService(seed(), nil, nil, nil)
	cases := map[string]CreateNoteInput{
		"empty content":  {EntityType: EntityLead, EntityID: leadRyan, Topic: "budget", Content: "   "},
		"missing topic":  {EntityType: EntityLead, EntityID: leadRyan, Topic: "", Content: "x"},
		"bad topic":      {EntityType: EntityLead, EntityID: leadRyan, Topic: "Budget!", Content: "x"},
		"too long":       {EntityType: EntityLead, EntityID: leadRyan, Topic: "budget", Content: strings.Repeat("a", maxNoteLength+1)},
		"bad type":       {EntityType: "deal", EntityID: leadRyan, Topic: "budget", Content: "x"},
		"malformed uuid": {EntityType: EntityLead, EntityID: "not-a-uuid", Topic: "budget", Content: "x"},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := svc.CreateNote(context.Background(), salesExecutive(seRyan), in)
			expectCode(t, err, apperrors.CodeValidation)
		})
	}
}

func TestListNotes_ScopeByRole(t *testing.T) {
	store := seed()
	svc := NewService(store, nil, nil, nil)
	ctx := context.Background()
	if _, err := svc.CreateNote(ctx, salesExecutive(seAnsu), CreateNoteInput{
		EntityType: EntityLead, EntityID: leadAnsu, Topic: "goals", Content: "Wants PR in 2 years",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.ListNotes(ctx, salesExecutive(seRyan), EntityLead, leadAnsu); err == nil {
		t.Fatal("sales executive must not read another executive's lead notes")
	}
	notes, err := svc.ListNotes(ctx, teamLeadClaims(), EntityLead, leadAnsu)
	if err != nil || len(notes) != 1 {
		t.Fatalf("team lead should see team notes: %v %d", err, len(notes))
	}
	_, err = svc.ListNotes(ctx, teamLeadClaims(), EntityLead, leadOther)
	expectCode(t, err, apperrors.CodeNotFound)
	if _, err := svc.ListNotes(ctx, superAdmin(), EntityLead, leadOther); err != nil {
		t.Fatalf("super admin has organization scope: %v", err)
	}
}

func TestUpdateAndDeleteNote_OnlyTheAuthor(t *testing.T) {
	store := seed()
	svc := NewService(store, nil, nil, nil)
	ctx := context.Background()
	n, err := svc.CreateNote(ctx, salesExecutive(seRyan), CreateNoteInput{
		EntityType: EntityLead, EntityID: leadRyan, Topic: "concerns", Content: "Worried about fees",
	})
	if err != nil {
		t.Fatal(err)
	}

	content := "edited by someone else"
	_, err = svc.UpdateNote(ctx, teamLeadClaims(), n.ID, UpdateNoteInput{Content: &content})
	expectCode(t, err, apperrors.CodeForbidden)
	expectCode(t, svc.DeleteNote(ctx, teamLeadClaims(), n.ID), apperrors.CodeForbidden)

	topic := "objections"
	updated, err := svc.UpdateNote(ctx, salesExecutive(seRyan), n.ID, UpdateNoteInput{Topic: &topic})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Topic != "objections" || updated.Content != "Worried about fees" {
		t.Fatalf("partial update should keep content: %#v", updated)
	}
	if err := svc.DeleteNote(ctx, salesExecutive(seRyan), n.ID); err != nil {
		t.Fatal(err)
	}
	if store.notes[n.ID] != nil {
		t.Fatal("note should be deleted")
	}
}

func TestUpdateNote_AuthorLosesAccessAfterReassignment(t *testing.T) {
	store := seed()
	svc := NewService(store, nil, nil, nil)
	ctx := context.Background()
	n, err := svc.CreateNote(ctx, salesExecutive(seRyan), CreateNoteInput{
		EntityType: EntityLead, EntityID: leadRyan, Topic: "budget", Content: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	store.records[leadRyan] = record{EntityLead, seAnsu, teamA}
	content := "y"
	_, err = svc.UpdateNote(ctx, salesExecutive(seRyan), n.ID, UpdateNoteInput{Content: &content})
	expectCode(t, err, apperrors.CodeNotFound)
}

func TestContacts_RequiresAViewPermission(t *testing.T) {
	svc := NewService(seed(), nil, nil, nil)
	_, err := svc.Contacts(context.Background(), auth.Claims{UserID: seRyan, RoleCode: permissions.RoleSalesExecutive}, ContactFilter{})
	expectCode(t, err, apperrors.CodeForbidden)
	_, err = svc.Contacts(context.Background(), salesExecutive(seRyan), ContactFilter{Type: "deal"})
	expectCode(t, err, apperrors.CodeValidation)
}

func TestContactSearch_PlaceholdersContinueAcrossHalves(t *testing.T) {
	cond, args := contactSearch("l", "0412 345", nil)
	if len(args) != 2 || !strings.Contains(cond, "$1") || !strings.Contains(cond, "$2") {
		t.Fatalf("lead half: %s %v", cond, args)
	}
	cond, args = contactSearch("c", "0412 345", args)
	if len(args) != 4 || !strings.Contains(cond, "$3") || !strings.Contains(cond, "$4") || strings.Contains(cond, "$1") {
		t.Fatalf("customer half should continue numbering: %s %v", cond, args)
	}
	if cond, args := contactSearch("l", "  ", nil); cond != "" || len(args) != 0 {
		t.Fatal("blank search adds no predicate")
	}
}

func TestTimelineTitle(t *testing.T) {
	if got := timelineTitle("next_steps"); got != "Call note · Next steps" {
		t.Fatalf("got %q", got)
	}
}

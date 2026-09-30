package datascope_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/crm/backend/internal/auth"
	"github.com/crm/backend/internal/datascope"
	"github.com/crm/backend/internal/permissions"
	"github.com/crm/backend/pkg/apperrors"
)

type fakeRow struct {
	value any
}

func (r fakeRow) Scan(dest ...any) error {
	switch d := dest[0].(type) {
	case *bool:
		*d = r.value.(bool)
	case *int:
		*d = r.value.(int)
	}
	return nil
}

type fakeQuerier struct {
	result any
	sql    string
	args   []any
	calls  int
}

func (q *fakeQuerier) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	q.calls++
	q.sql, q.args = sql, args
	return fakeRow{value: q.result}
}

const (
	leadID  = "8d2b6c1e-8f1a-4d0e-9a57-1f3c2b4a5d6e"
	leadID2 = "0b7f3a9c-2e4d-4c1b-8a6f-9d8e7c6b5a4f"
)

var (
	salesExec = auth.Claims{
		UserID:           "11111111-aaaa-4aaa-8aaa-111111111111",
		RoleCode:         permissions.RoleSalesExecutive,
		Permissions:      []string{permissions.LeadsView, permissions.LeadsEdit, permissions.DocumentsView},
		PermissionScopes: map[string]permissions.Scope{permissions.LeadsView: permissions.ScopeOwn},
	}
	teamLead = auth.Claims{
		UserID:      "22222222-bbbb-4bbb-8bbb-222222222222",
		RoleCode:    permissions.RoleSalesManager,
		Permissions: []string{permissions.LeadsView},
		TeamIDs:     []string{"team-a"},
	}
	superAdmin = auth.Claims{
		UserID:      "33333333-cccc-4ccc-8ccc-333333333333",
		RoleCode:    permissions.RoleSuperAdmin,
		Permissions: []string{permissions.LeadsView, permissions.CustomFieldsManage},
	}
)

func assertCode(t *testing.T, err error, code apperrors.Code) {
	t.Helper()
	ae, ok := apperrors.AsAppError(err)
	if !ok || ae.Code != code {
		t.Fatalf("want %s, got %#v", code, err)
	}
}

func TestRequireRecord_SalesExecOnlySeesOwnRows(t *testing.T) {
	q := &fakeQuerier{result: false}
	err := datascope.RequireRecord(context.Background(), q, salesExec, datascope.Leads, leadID, permissions.LeadsView)
	assertCode(t, err, apperrors.CodeNotFound)
	if !strings.Contains(q.sql, "l.owner_user_id = ANY($2::uuid[])") {
		t.Fatalf("expected owner predicate, got %s", q.sql)
	}
	if owners := q.args[1].([]string); len(owners) != 1 || owners[0] != salesExec.UserID {
		t.Fatalf("expected caller as the only owner, got %v", q.args[1])
	}

	q.result = true
	if err := datascope.RequireRecord(context.Background(), q, salesExec, datascope.Leads, leadID, permissions.LeadsView); err != nil {
		t.Fatalf("own record should pass: %v", err)
	}
}

func TestRequireRecord_TeamLeadIsLimitedToTheirTeams(t *testing.T) {
	q := &fakeQuerier{result: true}
	if err := datascope.RequireRecord(context.Background(), q, teamLead, datascope.Leads, leadID, permissions.LeadsView); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q.sql, "l.team_id = ANY($2::uuid[])") {
		t.Fatalf("expected team predicate, got %s", q.sql)
	}

	noTeam := teamLead
	noTeam.TeamIDs = nil
	q = &fakeQuerier{result: false}
	err := datascope.RequireRecord(context.Background(), q, noTeam, datascope.Leads, leadID, permissions.LeadsView)
	assertCode(t, err, apperrors.CodeNotFound)
	if owners := q.args[1].([]string); owners[0] == noTeam.UserID {
		t.Fatalf("a Team Lead without a team must match nothing, got owners %v", owners)
	}
}

func TestRequireRecord_OrganizationScopeIsUnfiltered(t *testing.T) {
	q := &fakeQuerier{result: true}
	if err := datascope.RequireRecord(context.Background(), q, superAdmin, datascope.Leads, leadID, permissions.LeadsView); err != nil {
		t.Fatal(err)
	}
	if len(q.args) != 1 || strings.Contains(q.sql, "owner_user_id") {
		t.Fatalf("expected id-only lookup, got %s %v", q.sql, q.args)
	}
}

func TestRequireRecord_WidestScopeAcrossPermissions(t *testing.T) {
	// Super Admin setting custom field values holds custom_fields:manage, not leads:edit.
	q := &fakeQuerier{result: true}
	err := datascope.RequireRecord(context.Background(), q, superAdmin, datascope.Leads, leadID,
		permissions.LeadsEdit, permissions.CustomFieldsManage)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.args) != 1 {
		t.Fatalf("expected organization scope, got %v", q.args)
	}
}

func TestRequireRecord_RejectsWithoutQueryingWhenIDIsMalformed(t *testing.T) {
	q := &fakeQuerier{result: true}
	err := datascope.RequireRecord(context.Background(), q, superAdmin, datascope.Leads, "not-a-uuid", permissions.LeadsView)
	assertCode(t, err, apperrors.CodeNotFound)
	if q.calls != 0 {
		t.Fatal("malformed id should not reach the database")
	}
}

func TestRequireRecord_MissingPermissionIsForbidden(t *testing.T) {
	q := &fakeQuerier{result: true}
	err := datascope.RequireRecord(context.Background(), q, salesExec, datascope.Deals, leadID, permissions.DealsView)
	assertCode(t, err, apperrors.CodeForbidden)
}

func TestRequireRecord_DocumentsFollowTheirParentOrUploader(t *testing.T) {
	q := &fakeQuerier{result: true}
	if err := datascope.RequireRecord(context.Background(), q, salesExec, datascope.Documents, leadID, permissions.DocumentsView); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"FROM leads pl", "FROM customers pc", "FROM deals pd", "d.uploaded_by = $"} {
		if !strings.Contains(q.sql, want) {
			t.Fatalf("document predicate missing %q: %s", want, q.sql)
		}
	}
	if last := q.args[len(q.args)-1]; last != salesExec.UserID {
		t.Fatalf("uploader arg should be the caller, got %v", last)
	}
}

func TestRequireRecords_EveryIDMustBeVisible(t *testing.T) {
	q := &fakeQuerier{result: 1}
	err := datascope.RequireRecords(context.Background(), q, salesExec, datascope.Leads,
		[]string{leadID, leadID2, leadID}, permissions.LeadsEdit)
	assertCode(t, err, apperrors.CodeNotFound)
	if ids := q.args[0].([]string); len(ids) != 2 {
		t.Fatalf("duplicates should collapse, got %v", ids)
	}

	q.result = 2
	if err := datascope.RequireRecords(context.Background(), q, salesExec, datascope.Leads,
		[]string{leadID, leadID2}, permissions.LeadsEdit); err != nil {
		t.Fatalf("all visible should pass: %v", err)
	}
}

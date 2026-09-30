package teams

import (
	"testing"

	"github.com/crm/backend/internal/auth"
	"github.com/crm/backend/internal/permissions"
	"github.com/crm/backend/pkg/apperrors"
)

func claimsFor(role string, teamIDs []string, grants map[string]permissions.Scope) auth.Claims {
	codes := make([]string, 0, len(grants))
	for code := range grants {
		codes = append(codes, code)
	}
	return auth.Claims{UserID: role + "-user", RoleCode: role, TeamIDs: teamIDs, Permissions: codes, PermissionScopes: grants}
}

func sampleTeam() Team {
	return Team{
		ID:        "team-a",
		MemberIDs: []string{"tl-a", "se-a1"},
		Members:   []TeamMember{{ID: "tl-a"}, {ID: "se-a1"}},
	}
}

func TestTeamLeadSeesOnlyOwnTeamWithMembers(t *testing.T) {
	claims := claimsFor(permissions.RoleSalesManager, []string{"team-a"}, map[string]permissions.Scope{
		permissions.TeamsView: permissions.ScopeTeam,
	})
	a, err := accessFor(claims, permissions.TeamsView)
	if err != nil {
		t.Fatal(err)
	}
	if !a.CanSee("team-a") || a.CanSee("team-b") {
		t.Fatal("team lead must see team-a only")
	}
	team := sampleTeam()
	a.Redact(&team)
	if len(team.Members) != 2 {
		t.Fatal("team lead keeps the member list of their own team")
	}
}

func TestOwnScopeSeesOwnTeamWithoutMembers(t *testing.T) {
	for _, role := range []string{permissions.RoleSalesExecutive, permissions.RoleSalesSupport} {
		claims := claimsFor(role, []string{"team-a"}, map[string]permissions.Scope{
			permissions.TeamsView: permissions.ScopeOwn,
		})
		a, err := accessFor(claims, permissions.TeamsView)
		if err != nil {
			t.Fatal(err)
		}
		if !a.CanSee("team-a") || a.CanSee("team-b") {
			t.Fatalf("%s must see only their own team", role)
		}
		team := sampleTeam()
		a.Redact(&team)
		if len(team.Members) != 0 || len(team.MemberIDs) != 0 {
			t.Fatalf("%s must not receive member lists", role)
		}
	}
}

func TestTeamSetupIsSuperAdminOnly(t *testing.T) {
	lead := claimsFor(permissions.RoleSalesManager, []string{"team-a"}, map[string]permissions.Scope{
		permissions.TeamsEdit:   permissions.ScopeTeam,
		permissions.TeamsAssign: permissions.ScopeTeam,
	})
	a, err := accessFor(lead, permissions.TeamsEdit, permissions.TeamsAssign)
	if err != nil {
		t.Fatal(err)
	}
	ae, ok := apperrors.AsAppError(a.requireOrganization())
	if !ok || ae.Code != apperrors.CodeForbidden {
		t.Fatalf("team lead must not change team setup, got %#v", ae)
	}

	admin := claimsFor(permissions.RoleSuperAdmin, nil, map[string]permissions.Scope{
		permissions.TeamsManage: permissions.ScopeOrganization,
		permissions.TeamsEdit:   permissions.ScopeOrganization,
	})
	a, err = accessFor(admin, permissions.TeamsEdit, permissions.TeamsAssign)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.requireOrganization(); err != nil || !a.CanSee("team-b") {
		t.Fatalf("super admin manages every team: %v", err)
	}
}

func TestZeroAccessSeesNoTeams(t *testing.T) {
	var a Access
	if a.CanSee("team-a") {
		t.Fatal("zero Access must see nothing")
	}
}

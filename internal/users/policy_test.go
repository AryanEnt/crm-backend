package users

import (
	"testing"

	"github.com/crm/backend/internal/auth"
	"github.com/crm/backend/internal/permissions"
	"github.com/crm/backend/pkg/apperrors"
)

const (
	teamA = "team-a"
	teamB = "team-b"
)

func claimsFor(role, userID string, teamIDs []string, grants map[string]permissions.Scope) auth.Claims {
	codes := make([]string, 0, len(grants))
	for code := range grants {
		codes = append(codes, code)
	}
	return auth.Claims{UserID: userID, RoleCode: role, TeamIDs: teamIDs, Permissions: codes, PermissionScopes: grants}
}

// Grants as seeded by migrations 000015 and 000026.
func teamLead() auth.Claims {
	return claimsFor(permissions.RoleSalesManager, "tl-a", []string{teamA}, map[string]permissions.Scope{
		permissions.UsersView:   permissions.ScopeTeam,
		permissions.UsersCreate: permissions.ScopeTeam,
		permissions.UsersEdit:   permissions.ScopeTeam,
		permissions.UsersDelete: permissions.ScopeTeam,
	})
}

func salesExecutive() auth.Claims {
	return claimsFor(permissions.RoleSalesExecutive, "se-a1", []string{teamA}, map[string]permissions.Scope{
		permissions.LeadsView: permissions.ScopeOwn,
		permissions.TeamsView: permissions.ScopeOwn,
	})
}

func salesSupport() auth.Claims {
	return claimsFor(permissions.RoleSalesSupport, "support-1", nil, map[string]permissions.Scope{
		permissions.LeadsView: permissions.ScopeOwn,
		permissions.TeamsView: permissions.ScopeOwn,
	})
}

func superAdmin() auth.Claims {
	return claimsFor(permissions.RoleSuperAdmin, "admin", nil, map[string]permissions.Scope{
		permissions.UsersManage: permissions.ScopeOrganization,
		permissions.UsersView:   permissions.ScopeOrganization,
		permissions.UsersCreate: permissions.ScopeOrganization,
		permissions.UsersEdit:   permissions.ScopeOrganization,
		permissions.UsersDelete: permissions.ScopeOrganization,
	})
}

var (
	seOnA    = &User{ID: "se-a1", RoleCode: permissions.RoleSalesExecutive, TeamIDs: []string{teamA}, RoleID: "role-se"}
	seOnB    = &User{ID: "se-b1", RoleCode: permissions.RoleSalesExecutive, TeamIDs: []string{teamB}, RoleID: "role-se"}
	leadOnA  = &User{ID: "tl-a", RoleCode: permissions.RoleSalesManager, TeamIDs: []string{teamA}, RoleID: "role-tl"}
	leadOnB  = &User{ID: "tl-b", RoleCode: permissions.RoleSalesManager, TeamIDs: []string{teamB}, RoleID: "role-tl"}
	unteamed = &User{ID: "se-x", RoleCode: permissions.RoleSalesExecutive, RoleID: "role-se"}
)

func mustAccess(t *testing.T, claims auth.Claims, codes ...string) Access {
	t.Helper()
	a, err := accessFor(claims, codes...)
	if err != nil {
		t.Fatalf("accessFor(%v): %v", codes, err)
	}
	return a
}

func wantCode(t *testing.T, err error, code apperrors.Code) {
	t.Helper()
	ae, ok := apperrors.AsAppError(err)
	if !ok || ae.Code != code {
		t.Fatalf("want %s, got %#v", code, err)
	}
}

func TestTeamLeadSeesOnlyOwnTeam(t *testing.T) {
	a := mustAccess(t, teamLead(), permissions.UsersView)
	if a.Scope != permissions.ScopeTeam {
		t.Fatalf("scope = %s, want team", a.Scope)
	}
	for _, u := range []*User{seOnA, leadOnA} {
		if !a.CanSee(u) {
			t.Errorf("should see %s", u.ID)
		}
	}
	for _, u := range []*User{seOnB, leadOnB, unteamed} {
		if a.CanSee(u) {
			t.Errorf("must not see %s", u.ID)
		}
	}
}

func TestTeamLeadManagesOnlyOwnSalesExecutives(t *testing.T) {
	a := mustAccess(t, teamLead(), permissions.UsersEdit)
	if err := a.requireManage(seOnA); err != nil {
		t.Fatalf("own SE: %v", err)
	}
	wantCode(t, a.requireManage(seOnB), apperrors.CodeNotFound)
	wantCode(t, a.requireManage(unteamed), apperrors.CodeNotFound)
	wantCode(t, a.requireManage(leadOnA), apperrors.CodeForbidden)
}

func TestTeamLeadCreateDefaultsToOwnTeam(t *testing.T) {
	a := mustAccess(t, teamLead(), permissions.UsersCreate)
	teams, err := a.ResolveCreateTeams(permissions.RoleSalesExecutive, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(teams) != 1 || teams[0] != teamA {
		t.Fatalf("teams = %v, want [%s]", teams, teamA)
	}
	if _, err := a.ResolveCreateTeams(permissions.RoleSalesExecutive, []string{teamA}); err != nil {
		t.Fatalf("explicit own team: %v", err)
	}
}

func TestTeamLeadCreateRejectsOtherTeamAndRoles(t *testing.T) {
	a := mustAccess(t, teamLead(), permissions.UsersCreate)
	_, err := a.ResolveCreateTeams(permissions.RoleSalesExecutive, []string{teamB})
	wantCode(t, err, apperrors.CodeForbidden)
	_, err = a.ResolveCreateTeams(permissions.RoleSalesExecutive, []string{teamA, teamB})
	wantCode(t, err, apperrors.CodeForbidden)
	for _, role := range []string{permissions.RoleSalesManager, permissions.RoleSuperAdmin, permissions.RoleSalesSupport} {
		_, err = a.ResolveCreateTeams(role, []string{teamA})
		wantCode(t, err, apperrors.CodeForbidden)
	}
}

func TestTeamLeadWithoutTeamCannotCreate(t *testing.T) {
	claims := teamLead()
	claims.TeamIDs = nil
	a := mustAccess(t, claims, permissions.UsersCreate)
	_, err := a.ResolveCreateTeams(permissions.RoleSalesExecutive, nil)
	wantCode(t, err, apperrors.CodeForbidden)
}

func TestTeamLeadUpdateKeepsRoleAndTeam(t *testing.T) {
	a := mustAccess(t, teamLead(), permissions.UsersEdit, permissions.UsersAssign)
	name := "Renamed"
	if err := a.AuthorizeUpdate(seOnA, UpdateInput{FullName: &name}, seOnA.TeamIDs, false); err != nil {
		t.Fatalf("profile edit: %v", err)
	}
	same := seOnA.RoleID
	if err := a.AuthorizeUpdate(seOnA, UpdateInput{RoleID: &same}, seOnA.TeamIDs, false); err != nil {
		t.Fatalf("unchanged role: %v", err)
	}
	promoted := "role-admin"
	wantCode(t, a.AuthorizeUpdate(seOnA, UpdateInput{RoleID: &promoted}, seOnA.TeamIDs, false), apperrors.CodeForbidden)
	wantCode(t, a.AuthorizeUpdate(seOnA, UpdateInput{TeamIDs: []string{teamB}}, []string{teamB}, true), apperrors.CodeForbidden)
	wantCode(t, a.AuthorizeUpdate(seOnB, UpdateInput{FullName: &name}, seOnB.TeamIDs, false), apperrors.CodeNotFound)
}

func TestSalesExecutiveAndSupportHaveNoUserAccess(t *testing.T) {
	for name, claims := range map[string]auth.Claims{"sales executive": salesExecutive(), "sales support": salesSupport()} {
		for _, code := range []string{permissions.UsersView, permissions.UsersCreate, permissions.UsersEdit, permissions.UsersDelete} {
			_, err := accessFor(claims, code)
			if err == nil {
				t.Errorf("%s: %s should be forbidden", name, code)
				continue
			}
			wantCode(t, err, apperrors.CodeForbidden)
		}
	}
}

func TestZeroAccessSeesNothing(t *testing.T) {
	var a Access
	if a.CanSee(seOnA) || a.CanManage(seOnA) {
		t.Fatal("zero Access must not see or manage anyone")
	}
}

func TestSuperAdminIsOrganizationWide(t *testing.T) {
	a := mustAccess(t, superAdmin(), permissions.UsersCreate)
	if _, err := a.ResolveCreateTeams(permissions.RoleSalesExecutive, []string{teamB}); err != nil {
		t.Fatalf("any team: %v", err)
	}
	edit := mustAccess(t, superAdmin(), permissions.UsersEdit, permissions.UsersAssign)
	promoted := "role-tl"
	if err := edit.AuthorizeUpdate(seOnB, UpdateInput{RoleID: &promoted, TeamIDs: []string{teamA}}, []string{teamA}, true); err != nil {
		t.Fatalf("role and team change: %v", err)
	}
	for _, u := range []*User{seOnA, seOnB, leadOnB, unteamed} {
		if !edit.CanSee(u) || !edit.CanManage(u) {
			t.Errorf("super admin should see and manage %s", u.ID)
		}
	}
}

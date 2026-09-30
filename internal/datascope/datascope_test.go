package datascope_test

import (
	"testing"

	"github.com/crm/backend/internal/auth"
	"github.com/crm/backend/internal/datascope"
	"github.com/crm/backend/internal/permissions"
	"github.com/crm/backend/pkg/apperrors"
)

func TestResolve_OwnRejectsOtherSE(t *testing.T) {
	claims := auth.Claims{
		UserID:   "se-1",
		RoleCode: permissions.RoleSalesExecutive,
		PermissionScopes: map[string]permissions.Scope{
			permissions.LeadsView: permissions.ScopeOwn,
		},
		Permissions: []string{permissions.LeadsView},
	}
	_, err := datascope.Resolve(claims, permissions.LeadsView, "se-2")
	if err == nil {
		t.Fatal("expected forbidden")
	}
	if ae, ok := apperrors.AsAppError(err); !ok || ae.Code != apperrors.CodeForbidden {
		t.Fatalf("got %#v", err)
	}
}

func TestWidestScope_PicksBroadestHeldGrant(t *testing.T) {
	claims := auth.Claims{
		UserID:   "tl-1",
		RoleCode: permissions.RoleSalesManager,
		PermissionScopes: map[string]permissions.Scope{
			permissions.UsersEdit: permissions.ScopeTeam,
		},
		Permissions: []string{permissions.UsersEdit},
	}
	scope, err := datascope.WidestScope(claims, permissions.UsersEdit, permissions.UsersAssign)
	if err != nil || scope != permissions.ScopeTeam {
		t.Fatalf("got %q, %v; want team", scope, err)
	}
	if _, err := datascope.WidestScope(claims, permissions.UsersDelete); err == nil {
		t.Fatal("expected forbidden when no listed permission is held")
	}
}

func TestResolve_TeamRejectsOutsideMember(t *testing.T) {
	claims := auth.Claims{
		UserID:            "tl-1",
		RoleCode:          permissions.RoleSalesManager,
		TeamIDs:           []string{"team-a"},
		TeamMemberUserIDs: []string{"tl-1", "se-ryan", "se-ansu"},
		PermissionScopes: map[string]permissions.Scope{
			permissions.LeadsView: permissions.ScopeTeam,
		},
		Permissions: []string{permissions.LeadsView},
	}
	_, err := datascope.Resolve(claims, permissions.LeadsView, "se-beta")
	if err == nil {
		t.Fatal("expected forbidden for other team SE")
	}
}

func TestResolve_TeamAllowsOwnSE(t *testing.T) {
	claims := auth.Claims{
		UserID:            "tl-1",
		RoleCode:          permissions.RoleSalesManager,
		TeamIDs:           []string{"team-a"},
		TeamMemberUserIDs: []string{"tl-1", "se-ryan"},
		PermissionScopes: map[string]permissions.Scope{
			permissions.LeadsView: permissions.ScopeTeam,
		},
		Permissions: []string{permissions.LeadsView},
	}
	vis, err := datascope.Resolve(claims, permissions.LeadsView, "se-ryan")
	if err != nil {
		t.Fatal(err)
	}
	if len(vis.OwnerIDs) != 1 || vis.OwnerIDs[0] != "se-ryan" {
		t.Fatalf("owner = %#v", vis.OwnerIDs)
	}
	if len(vis.TeamIDs) != 1 || vis.TeamIDs[0] != "team-a" {
		t.Fatalf("teams = %#v", vis.TeamIDs)
	}
}

func TestResolve_OrgUnscoped(t *testing.T) {
	claims := auth.Claims{
		UserID:   "admin",
		RoleCode: permissions.RoleSuperAdmin,
		PermissionScopes: map[string]permissions.Scope{
			permissions.LeadsView: permissions.ScopeOrganization,
		},
		Permissions: []string{permissions.LeadsView},
	}
	vis, err := datascope.Resolve(claims, permissions.LeadsView, "")
	if err != nil {
		t.Fatal(err)
	}
	if !vis.Unscoped {
		t.Fatal("expected unscoped")
	}
}

func TestResolveReport_OwnPinsCaller(t *testing.T) {
	claims := auth.Claims{
		UserID:   "se-1",
		RoleCode: permissions.RoleSalesExecutive,
		PermissionScopes: map[string]permissions.Scope{
			permissions.AnalyticsView: permissions.ScopeOwn,
		},
		Permissions: []string{permissions.AnalyticsView},
	}
	rs, err := datascope.ResolveReport(claims, permissions.AnalyticsView, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if rs.OwnerUserID != "se-1" {
		t.Fatalf("owner = %q", rs.OwnerUserID)
	}
	if len(rs.UserIDs) != 1 || rs.UserIDs[0] != "se-1" || rs.TeamIDs == nil || len(rs.TeamIDs) != 0 {
		t.Fatalf("users=%#v teams=%#v", rs.UserIDs, rs.TeamIDs)
	}
	if _, err := datascope.ResolveReport(claims, permissions.AnalyticsView, "", "se-2"); err == nil {
		t.Fatal("expected forbidden for another owner")
	}
}

func TestResolveReport_TeamPinsOwnTeam(t *testing.T) {
	claims := auth.Claims{
		UserID:            "tl-1",
		RoleCode:          permissions.RoleSalesManager,
		TeamIDs:           []string{"team-a"},
		TeamMemberUserIDs: []string{"se-ryan"},
		PermissionScopes: map[string]permissions.Scope{
			permissions.AnalyticsView: permissions.ScopeTeam,
		},
		Permissions: []string{permissions.AnalyticsView},
	}
	rs, err := datascope.ResolveReport(claims, permissions.AnalyticsView, "", "se-ryan")
	if err != nil {
		t.Fatal(err)
	}
	if rs.TeamID != "team-a" || rs.OwnerUserID != "se-ryan" {
		t.Fatalf("team=%q owner=%q", rs.TeamID, rs.OwnerUserID)
	}
	if _, err := datascope.ResolveReport(claims, permissions.AnalyticsView, "team-b", ""); err == nil {
		t.Fatal("expected forbidden for another team")
	}
	if _, err := datascope.ResolveReport(claims, permissions.AnalyticsView, "", "se-beta"); err == nil {
		t.Fatal("expected forbidden for owner outside team")
	}
}

func TestResolveReport_TeamWithoutTeamsMatchesNothing(t *testing.T) {
	claims := auth.Claims{
		UserID:   "tl-1",
		RoleCode: permissions.RoleSalesManager,
		PermissionScopes: map[string]permissions.Scope{
			permissions.ForecastsView: permissions.ScopeTeam,
		},
		Permissions: []string{permissions.ForecastsView},
	}
	rs, err := datascope.ResolveReport(claims, permissions.ForecastsView, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if rs.OwnerUserID == "" || rs.UserIDs == nil || rs.TeamIDs == nil {
		t.Fatalf("expected no-match scope, got %#v", rs)
	}
}

func TestResolveReport_OrgPassesThrough(t *testing.T) {
	claims := auth.Claims{
		UserID:   "admin",
		RoleCode: permissions.RoleSuperAdmin,
		PermissionScopes: map[string]permissions.Scope{
			permissions.AnalyticsView: permissions.ScopeOrganization,
		},
		Permissions: []string{permissions.AnalyticsView},
	}
	rs, err := datascope.ResolveReport(claims, permissions.AnalyticsView, "team-x", "u-9")
	if err != nil {
		t.Fatal(err)
	}
	if rs.TeamID != "team-x" || rs.OwnerUserID != "u-9" || rs.UserIDs != nil || rs.TeamIDs != nil {
		t.Fatalf("got %#v", rs)
	}
}

func TestAppendWhere_Own(t *testing.T) {
	v := datascope.Visibility{OwnerIDs: []string{"u1"}}
	where, args := datascope.AppendWhere(nil, nil, v, datascope.Columns{Owner: "l.owner_user_id", Team: "l.team_id"})
	if len(where) != 1 || len(args) != 1 {
		t.Fatalf("where=%v args=%v", where, args)
	}
}

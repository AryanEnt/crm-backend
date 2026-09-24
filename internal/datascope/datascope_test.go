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

func TestAppendWhere_Own(t *testing.T) {
	v := datascope.Visibility{OwnerIDs: []string{"u1"}}
	where, args := datascope.AppendWhere(nil, nil, v, datascope.Columns{Owner: "l.owner_user_id", Team: "l.team_id"})
	if len(where) != 1 || len(args) != 1 {
		t.Fatalf("where=%v args=%v", where, args)
	}
}

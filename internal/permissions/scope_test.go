package permissions_test

import (
	"testing"

	"github.com/crm/backend/internal/permissions"
)

func TestExpandImpliesScoped_CarriesTeamScope(t *testing.T) {
	scopes := permissions.ExpandImpliesScoped([]permissions.Grant{
		{Code: "leads:view", Scope: permissions.ScopeTeam},
	})
	if scopes["leads:view"] != permissions.ScopeTeam {
		t.Fatalf("got %s", scopes["leads:view"])
	}
}

func TestWidenScope(t *testing.T) {
	if permissions.WidenScope(permissions.ScopeOwn, permissions.ScopeTeam) != permissions.ScopeTeam {
		t.Fatal("team should win")
	}
	if permissions.WidenScope(permissions.ScopeTeam, permissions.ScopeOrganization) != permissions.ScopeOrganization {
		t.Fatal("org should win")
	}
}

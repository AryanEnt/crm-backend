package teams

import (
	"github.com/crm/backend/internal/auth"
	"github.com/crm/backend/internal/datascope"
	"github.com/crm/backend/internal/permissions"
	"github.com/crm/backend/pkg/apperrors"
)

// Access is the caller's reach over teams. The zero value sees nothing.
type Access struct {
	Scope   permissions.Scope
	TeamIDs []string
}

func accessFor(claims auth.Claims, codes ...string) (Access, error) {
	scope, err := datascope.WidestScope(claims, codes...)
	if err != nil {
		return Access{}, err
	}
	return Access{Scope: scope, TeamIDs: claims.TeamIDs}, nil
}

func (a Access) organization() bool { return a.Scope == permissions.ScopeOrganization }

// CanSee: organization sees every team; team and own scope see only the caller's teams.
func (a Access) CanSee(teamID string) bool {
	switch a.Scope {
	case permissions.ScopeOrganization:
		return true
	case permissions.ScopeTeam, permissions.ScopeOwn:
		for _, id := range a.TeamIDs {
			if id == teamID {
				return true
			}
		}
	}
	return false
}

// Redact drops the member list for own-scope callers, who may know their team but not its people.
func (a Access) Redact(t *Team) {
	if a.Scope == permissions.ScopeTeam || a.organization() {
		return
	}
	t.Members = []TeamMember{}
	t.MemberIDs = []string{}
}

// requireOrganization keeps team setup (create, rename, membership, Team Lead, status) with Super Admin.
func (a Access) requireOrganization() error {
	if a.organization() {
		return nil
	}
	return apperrors.Forbidden("Only Super Admin can change team setup")
}

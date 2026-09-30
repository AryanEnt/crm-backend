package users

import (
	"github.com/crm/backend/internal/auth"
	"github.com/crm/backend/internal/datascope"
	"github.com/crm/backend/internal/permissions"
	"github.com/crm/backend/pkg/apperrors"
)

// Access is the caller's reach over user records for one kind of action.
// The zero value sees nothing.
type Access struct {
	Scope   permissions.Scope
	UserID  string
	TeamIDs []string
}

func accessFor(claims auth.Claims, codes ...string) (Access, error) {
	scope, err := datascope.WidestScope(claims, codes...)
	if err != nil {
		return Access{}, err
	}
	return Access{Scope: scope, UserID: claims.UserID, TeamIDs: claims.TeamIDs}, nil
}

func (a Access) organization() bool { return a.Scope == permissions.ScopeOrganization }

// CanSee: organization sees everyone, team sees themselves and anyone on their teams, own sees themselves.
func (a Access) CanSee(u *User) bool {
	if u == nil {
		return false
	}
	switch a.Scope {
	case permissions.ScopeOrganization:
		return true
	case permissions.ScopeTeam:
		return u.ID == a.UserID || sharesTeam(u.TeamIDs, a.TeamIDs)
	case permissions.ScopeOwn:
		return a.UserID != "" && u.ID == a.UserID
	default:
		return false
	}
}

// CanManage: team scope manages only the Sales Executives on the caller's teams, never themselves.
func (a Access) CanManage(u *User) bool {
	if u == nil {
		return false
	}
	switch a.Scope {
	case permissions.ScopeOrganization:
		return true
	case permissions.ScopeTeam:
		return u.ID != a.UserID && u.RoleCode == permissions.RoleSalesExecutive && sharesTeam(u.TeamIDs, a.TeamIDs)
	default:
		return false
	}
}

// requireManage hides records the caller can't see (404) and refuses visible ones they can't manage (403).
func (a Access) requireManage(u *User) error {
	if !a.CanSee(u) {
		return apperrors.NotFound("user not found")
	}
	if !a.CanManage(u) {
		return apperrors.Forbidden("You can only manage Sales Executives on your own team")
	}
	return nil
}

// ResolveCreateTeams returns the team assignment for a new user, filling in the caller's
// own team for team-scoped callers and refusing any other team.
func (a Access) ResolveCreateTeams(roleCode string, teamIDs []string) ([]string, error) {
	switch a.Scope {
	case permissions.ScopeOrganization:
		return teamIDs, nil
	case permissions.ScopeTeam:
	default:
		return nil, apperrors.Forbidden("You don't have permission to add users")
	}
	if roleCode != permissions.RoleSalesExecutive {
		return nil, apperrors.Forbidden("Team Leads can only add Sales Executives")
	}
	if len(a.TeamIDs) == 0 {
		return nil, apperrors.Forbidden("You aren't assigned to a team yet, so you can't add Sales Executives")
	}
	if len(teamIDs) == 0 {
		if len(a.TeamIDs) > 1 {
			return nil, apperrors.FieldValidation("team_id", "Choose which of your teams to add them to")
		}
		return []string{a.TeamIDs[0]}, nil
	}
	for _, id := range teamIDs {
		if !contains(a.TeamIDs, id) {
			return nil, apperrors.Forbidden("You can only add Sales Executives to your own team")
		}
	}
	return teamIDs, nil
}

// AuthorizeUpdate keeps role changes and team moves with organization-scoped callers.
func (a Access) AuthorizeUpdate(current *User, in UpdateInput, teamIDs []string, teamsProvided bool) error {
	if err := a.requireManage(current); err != nil {
		return err
	}
	if a.organization() {
		return nil
	}
	if in.RoleID != nil && *in.RoleID != current.RoleID {
		return apperrors.Forbidden("Only Super Admin can change a user's role")
	}
	if teamsProvided && !teamsEqual(current.TeamIDs, teamIDs) {
		return apperrors.Forbidden("Only Super Admin can move people between teams")
	}
	return nil
}

func sharesTeam(a, b []string) bool {
	for _, x := range a {
		if contains(b, x) {
			return true
		}
	}
	return false
}

func contains(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

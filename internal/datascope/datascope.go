package datascope

import (
	"fmt"
	"strings"

	"github.com/crm/backend/internal/auth"
	"github.com/crm/backend/internal/permissions"
	"github.com/crm/backend/pkg/apperrors"
)

// Visibility describes how list/get queries must restrict rows for the caller.
type Visibility struct {
	Unscoped bool
	OwnerIDs []string
	TeamIDs  []string
}

// Resolve builds visibility for a view permission and optional sales_executive_id filter.
func Resolve(claims auth.Claims, viewPermission string, salesExecutiveID string) (Visibility, error) {
	scope := permissions.ScopeFor(claims.PermissionScopes, viewPermission)
	if scope == "" {
		perms := permissions.ExpandImplies(claims.Permissions)
		if !perms.Has(viewPermission) {
			return Visibility{}, apperrors.Forbidden("missing permission")
		}
		switch claims.RoleCode {
		case permissions.RoleSuperAdmin:
			scope = permissions.ScopeOrganization
		case permissions.RoleSalesManager:
			scope = permissions.ScopeTeam
		default:
			scope = permissions.ScopeOwn
		}
	}

	seID := strings.TrimSpace(salesExecutiveID)
	v := Visibility{}

	switch scope {
	case permissions.ScopeOrganization:
		if seID != "" {
			v.OwnerIDs = []string{seID}
		} else {
			v.Unscoped = true
		}
		return v, nil

	case permissions.ScopeTeam:
		if len(claims.TeamIDs) == 0 {
			v.OwnerIDs = []string{"00000000-0000-0000-0000-000000000000"}
			return v, nil
		}
		v.TeamIDs = append([]string{}, claims.TeamIDs...)
		if seID != "" {
			if len(claims.TeamMemberUserIDs) > 0 && !contains(claims.TeamMemberUserIDs, seID) && seID != claims.UserID {
				return Visibility{}, apperrors.Forbidden("sales executive is outside your team scope")
			}
			v.OwnerIDs = []string{seID}
		}
		return v, nil

	default: // own
		if seID != "" && seID != claims.UserID {
			return Visibility{}, apperrors.Forbidden("cannot filter records outside your scope")
		}
		v.OwnerIDs = []string{claims.UserID}
		return v, nil
	}
}

// ApplyOwnerFilter narrows visibility with an explicit ownerUserId query param.
func ApplyOwnerFilter(v Visibility, ownerUserID string) (Visibility, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	if ownerUserID == "" {
		return v, nil
	}
	if v.Unscoped {
		v.Unscoped = false
		v.OwnerIDs = []string{ownerUserID}
		return v, nil
	}
	if len(v.OwnerIDs) == 1 && v.OwnerIDs[0] == ownerUserID {
		return v, nil
	}
	if len(v.OwnerIDs) > 0 && !contains(v.OwnerIDs, ownerUserID) {
		return Visibility{}, apperrors.Forbidden("requested owner is outside your scope")
	}
	if len(v.TeamIDs) > 0 && len(claimsTeamMembersHint(v)) > 0 {
		// Team scope without prior owner list — accept; SQL still constrains by team
	}
	v.OwnerIDs = []string{ownerUserID}
	return v, nil
}

func claimsTeamMembersHint(v Visibility) []string { return v.TeamIDs }

func contains(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// Columns names the ownership columns on a CRM entity.
type Columns struct {
	Owner string // e.g. "l.owner_user_id"
	Team  string // e.g. "l.team_id"; empty for activities without team_id
}

// AppendWhere appends a visibility predicate to where/args.
// args are 1-indexed placeholders already populated; next index is len(args)+1.
func AppendWhere(where []string, args []any, v Visibility, cols Columns) ([]string, []any) {
	if v.Unscoped {
		return where, args
	}

	// Explicit owner filter (OWN scope, or TEAM/ORG with SE selected)
	if len(v.OwnerIDs) > 0 && cols.Owner != "" {
		args = append(args, v.OwnerIDs)
		n := len(args)
		ownerPred := fmt.Sprintf("%s = ANY($%d::uuid[])", cols.Owner, n)

		if len(v.TeamIDs) == 0 {
			return append(where, ownerPred), args
		}
		// Owner + team: require owner match AND team membership (prevents IDOR if SE list stale)
		args = append(args, v.TeamIDs)
		tn := len(args)
		teamPred := teamMembershipPred(cols, tn)
		return append(where, "("+ownerPred+" AND "+teamPred+")"), args
	}

	if len(v.TeamIDs) > 0 {
		args = append(args, v.TeamIDs)
		tn := len(args)
		return append(where, teamMembershipPred(cols, tn)), args
	}

	return append(where, "FALSE"), args
}

func teamMembershipPred(cols Columns, teamArg int) string {
	parts := []string{}
	n := fmt.Sprintf("$%d", teamArg)
	if cols.Team != "" {
		parts = append(parts, fmt.Sprintf("%s = ANY(%s::uuid[])", cols.Team, n))
	}
	if cols.Owner != "" {
		parts = append(parts, fmt.Sprintf(
			"%s IN (SELECT tm.user_id FROM team_members tm WHERE tm.team_id = ANY(%s::uuid[]))",
			cols.Owner, n,
		))
	}
	if len(parts) == 0 {
		return "FALSE"
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}

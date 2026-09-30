package leads

import (
	"testing"

	"github.com/crm/backend/internal/auth"
	"github.com/crm/backend/internal/permissions"
	"github.com/crm/backend/pkg/apperrors"
)

func TestAssignTargetInScope(t *testing.T) {
	teamLead := auth.Claims{
		UserID:            "tl",
		RoleCode:          permissions.RoleSalesManager,
		Permissions:       []string{permissions.LeadsAssign},
		TeamIDs:           []string{"team-a"},
		TeamMemberUserIDs: []string{"se-a"},
	}
	admin := auth.Claims{UserID: "admin", RoleCode: permissions.RoleSuperAdmin, Permissions: []string{permissions.LeadsAssign}}

	cases := []struct {
		name          string
		claims        auth.Claims
		owner, teamID string
		wantForbidden bool
	}{
		{"team lead to own SE", teamLead, "se-a", "team-a", false},
		{"team lead to self", teamLead, "tl", "", false},
		{"team lead to another team's SE", teamLead, "se-b", "", true},
		{"team lead to another team", teamLead, "", "team-b", true},
		{"super admin anywhere", admin, "se-b", "team-b", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := assignTargetInScope(tc.claims, tc.owner, tc.teamID)
			if !tc.wantForbidden {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if ae, ok := apperrors.AsAppError(err); !ok || ae.Code != apperrors.CodeForbidden {
				t.Fatalf("want forbidden, got %#v", err)
			}
		})
	}
}

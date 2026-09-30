package datascope

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/crm/backend/internal/auth"
	"github.com/crm/backend/internal/permissions"
	"github.com/crm/backend/pkg/apperrors"
)

// Querier is the slice of *pgxpool.Pool the record checks need.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Record describes how rows of one CRM table are owned, for single-record and batch checks.
type Record struct {
	Noun    string // "lead"; used in the not-found error
	From    string // "leads l"
	Alias   string // "l"
	Columns Columns
	// Where replaces the owner/team predicate for tables that inherit visibility
	// from another record (documents follow their lead, customer or deal).
	Where func(where []string, args []any, v Visibility, claims auth.Claims) ([]string, []any)
}

var (
	Leads      = Record{Noun: "lead", From: "leads l", Alias: "l", Columns: Columns{Owner: "l.owner_user_id", Team: "l.team_id"}}
	Customers  = Record{Noun: "customer", From: "customers c", Alias: "c", Columns: Columns{Owner: "c.owner_user_id", Team: "c.team_id"}}
	Deals      = Record{Noun: "deal", From: "deals d", Alias: "d", Columns: Columns{Owner: "d.owner_user_id", Team: "d.team_id"}}
	Activities = Record{Noun: "activity", From: "activities a", Alias: "a", Columns: Columns{Owner: "a.owner_user_id"}}
	Documents  = Record{Noun: "document", From: "documents d", Alias: "d", Where: documentWhere}
)

// documentWhere: a document is visible through the lead, customer or deal it belongs
// to, or to whoever uploaded it.
func documentWhere(where []string, args []any, v Visibility, claims auth.Claims) ([]string, []any) {
	parents := []struct{ column, from, alias string }{
		{"d.lead_id", "leads pl", "pl"},
		{"d.customer_id", "customers pc", "pc"},
		{"d.deal_id", "deals pd", "pd"},
	}
	var via []string
	for _, p := range parents {
		var sub []string
		sub, args = AppendWhere([]string{p.alias + ".id = " + p.column}, args, v,
			Columns{Owner: p.alias + ".owner_user_id", Team: p.alias + ".team_id"})
		via = append(via, "EXISTS (SELECT 1 FROM "+p.from+" WHERE "+strings.Join(sub, " AND ")+")")
	}
	args = append(args, claims.UserID)
	via = append(via, fmt.Sprintf("d.uploaded_by = $%d::uuid", len(args)))
	return append(where, "("+strings.Join(via, " OR ")+")"), args
}

func (rec Record) notFound() error {
	return apperrors.NotFound(rec.Noun + " not found")
}

// AppendScope adds the record's visibility predicate to a query already selecting FROM rec.From.
func (rec Record) AppendScope(where []string, args []any, v Visibility, claims auth.Claims) ([]string, []any) {
	if v.Unscoped {
		return where, args
	}
	if rec.Where != nil {
		return rec.Where(where, args, v, claims)
	}
	return AppendWhere(where, args, v, rec.Columns)
}

// visibilityFor is the row filter for a scope, with no extra owner narrowing.
func visibilityFor(claims auth.Claims, scope permissions.Scope) Visibility {
	switch scope {
	case permissions.ScopeOrganization:
		return Visibility{Unscoped: true}
	case permissions.ScopeTeam:
		if len(claims.TeamIDs) == 0 {
			return Visibility{OwnerIDs: []string{noMatchID}}
		}
		return Visibility{TeamIDs: append([]string{}, claims.TeamIDs...)}
	default:
		return Visibility{OwnerIDs: []string{claims.UserID}}
	}
}

func recordVisibility(claims auth.Claims, perms []string) (Visibility, error) {
	scope, err := WidestScope(claims, perms...)
	if err != nil {
		return Visibility{}, err
	}
	return visibilityFor(claims, scope), nil
}

// RequireRecord fails with not-found unless id exists and sits inside the caller's
// widest scope across perms. Out-of-scope rows look exactly like missing ones.
func RequireRecord(ctx context.Context, q Querier, claims auth.Claims, rec Record, id string, perms ...string) error {
	if _, err := uuid.Parse(id); err != nil {
		return rec.notFound()
	}
	v, err := recordVisibility(claims, perms)
	if err != nil {
		return err
	}
	where, args := rec.AppendScope([]string{rec.Alias + ".id = $1"}, []any{id}, v, claims)
	var ok bool
	sql := `SELECT EXISTS(SELECT 1 FROM ` + rec.From + ` WHERE ` + strings.Join(where, " AND ") + `)`
	if err := q.QueryRow(ctx, sql, args...).Scan(&ok); err != nil {
		return apperrors.Internal("failed to check record access", err)
	}
	if !ok {
		return rec.notFound()
	}
	return nil
}

// RequireRecords is RequireRecord for a batch: every id must be visible, or nothing happens.
func RequireRecords(ctx context.Context, q Querier, claims auth.Claims, rec Record, ids []string, perms ...string) error {
	unique := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, err := uuid.Parse(id); err != nil {
			return rec.notFound()
		}
		if _, dup := seen[id]; !dup {
			seen[id] = struct{}{}
			unique = append(unique, id)
		}
	}
	if len(unique) == 0 {
		return nil
	}
	v, err := recordVisibility(claims, perms)
	if err != nil {
		return err
	}
	where, args := rec.AppendScope([]string{rec.Alias + ".id = ANY($1::uuid[])"}, []any{unique}, v, claims)
	var visible int
	sql := `SELECT COUNT(*) FROM ` + rec.From + ` WHERE ` + strings.Join(where, " AND ")
	if err := q.QueryRow(ctx, sql, args...).Scan(&visible); err != nil {
		return apperrors.Internal("failed to check record access", err)
	}
	if visible != len(unique) {
		return rec.notFound()
	}
	return nil
}

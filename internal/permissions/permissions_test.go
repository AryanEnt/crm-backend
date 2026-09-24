package permissions

import "testing"

func TestExpandImplies(t *testing.T) {
	t.Parallel()
	s := ExpandImplies([]string{UsersManage, TeamsView})
	if !s.Has(UsersView) || !s.Has(UsersDelete) || !s.Has(UsersAssign) {
		t.Fatalf("manage should imply users actions: %#v", s.List())
	}
	if !s.Has(TeamsView) {
		t.Fatal("expected teams:view")
	}
	if s.Has(TeamsDelete) {
		t.Fatal("did not expect teams:delete")
	}
}

func TestHasAny(t *testing.T) {
	t.Parallel()
	s := NewSet(UsersView, TeamsView)
	if !s.HasAny(UsersEdit, UsersView) {
		t.Fatal("expected HasAny true")
	}
	if s.HasAny(UsersEdit, UsersDelete) {
		t.Fatal("expected HasAny false")
	}
}

package apperrors

import "testing"

func TestAsAppError(t *testing.T) {
	t.Parallel()

	err := Validation("bad input")
	got, ok := AsAppError(err)
	if !ok {
		t.Fatal("expected AppError")
	}
	if got.Code != CodeValidation {
		t.Fatalf("code = %s, want %s", got.Code, CodeValidation)
	}
}

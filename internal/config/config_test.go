package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateRequiresSecrets(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		AppEnv:      "development",
		APIPort:     "8080",
		DatabaseURL: "",
		JWTSecret:   "",
		FrontendURL: "http://localhost:3000",
	}

	err := cfg.validate()
	if err == nil {
		t.Fatal("expected validation error for missing secrets")
	}
}

func TestValidateAcceptsValidConfig(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		AppEnv:      "development",
		APIPort:     "8080",
		DatabaseURL: "postgres://localhost/crm",
		JWTSecret:   "test-secret",
		FrontendURL: "http://localhost:3000",
	}

	if err := cfg.validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsDevelopment(t *testing.T) {
	t.Parallel()

	cfg := &Config{AppEnv: "development"}
	if !cfg.IsDevelopment() {
		t.Fatal("expected development mode")
	}

	cfg.AppEnv = "production"
	if cfg.IsDevelopment() {
		t.Fatal("expected non-development mode")
	}
}

func TestLoadPortFallsBackToPORT(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/crm")
	t.Setenv("JWT_SECRET", "test-secret")
	t.Setenv("API_PORT", "")
	t.Setenv("PORT", "9090")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.APIPort != "9090" {
		t.Fatalf("got %q, want 9090", cfg.APIPort)
	}
}

func TestLoadAPIPortWinsOverPORT(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/crm")
	t.Setenv("JWT_SECRET", "test-secret")
	t.Setenv("API_PORT", "8080")
	t.Setenv("PORT", "9090")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.APIPort != "8080" {
		t.Fatalf("got %q, want 8080", cfg.APIPort)
	}
}

func TestLoadRejectsMalformedEnvFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("cat > .env <<'EOF'\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(); err == nil {
		t.Fatal("expected error for malformed .env")
	}
}

func TestPrimaryFrontendURL(t *testing.T) {
	t.Parallel()
	got := primaryFrontendURL("http://localhost:3000, http://10.110.110.77:3000")
	if got != "http://localhost:3000" {
		t.Fatalf("got %q", got)
	}
}

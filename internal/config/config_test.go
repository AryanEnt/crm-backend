package config

import "testing"

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

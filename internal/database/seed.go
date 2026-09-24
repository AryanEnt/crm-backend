package database

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

const superAdminRoleID = "11111111-1111-1111-1111-111111111111"
const defaultAdminID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"

// SeedBootstrapAdmin creates the initial Super Admin if no users exist.
func SeedBootstrapAdmin(ctx context.Context, pool *pgxpool.Pool) error {
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return nil
	}

	email := os.Getenv("BOOTSTRAP_ADMIN_EMAIL")
	if email == "" {
		email = "admin@crm.local"
	}
	password := os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")
	if password == "" {
		password = "ChangeMe123!"
	}
	name := os.Getenv("BOOTSTRAP_ADMIN_NAME")
	if name == "" {
		name = "Super Admin"
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return err
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, full_name, role_id, is_active)
		VALUES ($1, lower($2), $3, $4, $5, TRUE)
	`, defaultAdminID, email, string(hash), name, superAdminRoleID)
	if err != nil {
		return fmt.Errorf("seed admin: %w", err)
	}

	slog.Info("bootstrap super admin created", "email", email)
	return nil
}

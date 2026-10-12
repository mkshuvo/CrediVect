package storage

import (
	"context"
	"errors"
	"fmt"

	"credivect/core/internal/domain"

	"github.com/jackc/pgx/v5"
)

func (r *Repository) GetUserByEmail(ctx context.Context, email string) (domain.User, error) {
	var u domain.User
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, email, full_name, role, password_hash, created_at
		FROM users WHERE lower(email) = lower($1)`, email).
		Scan(&u.ID, &u.Email, &u.FullName, &u.Role, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, fmt.Errorf("no account for %s", email)
	}
	return u, err
}

func (r *Repository) UpsertUser(ctx context.Context, u domain.User) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO users (id, email, full_name, role, password_hash, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (email) DO UPDATE SET full_name = EXCLUDED.full_name, role = EXCLUDED.role,
		                                   password_hash = EXCLUDED.password_hash`,
		u.ID, u.Email, u.FullName, u.Role, u.PasswordHash, u.CreatedAt)
	return err
}

func (r *Repository) FindApplicantByBlindIndex(ctx context.Context, blindIndex string) (domain.Applicant, error) {
	var a domain.Applicant
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, external_id, first_name, last_name, email, to_char(date_of_birth,'YYYY-MM-DD'),
		       monthly_income_cents, currency_code, employment_status, created_at
		FROM applicants WHERE ssn_blind_index = $1`, blindIndex).
		Scan(&a.ID, &a.ExternalID, &a.FirstName, &a.LastName, &a.Email, &a.DateOfBirth,
			&a.MonthlyIncomeCents, &a.CurrencyCode, &a.EmploymentStatus, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, fmt.Errorf("no applicant for this identifier")
	}
	a.SSNBlindIndex = blindIndex
	return a, err
}

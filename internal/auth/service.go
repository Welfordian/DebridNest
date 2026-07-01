package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/debridnest/debridnest/internal/storage"
)

var (
	ErrUserExists   = errors.New("user already exists")
	ErrUserNotFound = errors.New("user not found")
	ErrInvalidRole  = errors.New("invalid role")
	ErrLastAdmin    = errors.New("cannot delete last admin")
	ErrSelfDelete   = errors.New("cannot delete own account")
)

type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	Admin bool   `json:"admin"`
}

type UserRecord struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	Disabled  bool      `json:"disabled"`
	CreatedAt time.Time `json:"createdAt"`
}

type Service struct {
	db               *storage.DB
	multiUserEnabled bool
	legacyToken      string
}

func New(db *storage.DB, multiUserEnabled bool, legacyToken string) (*Service, error) {
	s := &Service{
		db:               db,
		multiUserEnabled: multiUserEnabled,
		legacyToken:      legacyToken,
	}
	if multiUserEnabled {
		if err := s.bootstrap(context.Background()); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Service) bootstrap(ctx context.Context) error {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return nil
	}
	id := uuid.NewString()
	return s.insertUser(ctx, id, "owner", HashToken(s.legacyToken), "admin")
}

func (s *Service) MultiUserEnabled() bool {
	return s.multiUserEnabled
}

func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Service) ValidateToken(ctx context.Context, bearer string) (User, bool) {
	token := strings.TrimSpace(strings.TrimPrefix(bearer, "Bearer "))
	if token == "" {
		return User{}, false
	}

	if !s.multiUserEnabled {
		if token == s.legacyToken {
			return User{ID: "legacy", Name: "owner", Role: "admin", Admin: true}, true
		}
		return User{}, false
	}

	hash := HashToken(token)
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, role FROM users
		WHERE token_hash = ? AND disabled = 0`, hash)

	var u User
	if err := row.Scan(&u.ID, &u.Name, &u.Role); err != nil {
		return User{}, false
	}
	u.Admin = u.Role == "admin"
	return u, true
}

func (s *Service) CreateUser(ctx context.Context, name, role string) (UserRecord, string, error) {
	if !s.multiUserEnabled {
		return UserRecord{}, "", errors.New("multi-user disabled")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return UserRecord{}, "", errors.New("name required")
	}
	if role != "admin" && role != "user" {
		return UserRecord{}, "", ErrInvalidRole
	}

	token, err := GenerateToken()
	if err != nil {
		return UserRecord{}, "", err
	}

	id := uuid.NewString()
	if err := s.insertUser(ctx, id, name, HashToken(token), role); err != nil {
		return UserRecord{}, "", err
	}
	rec, err := s.getUserByID(ctx, id)
	if err != nil {
		return UserRecord{}, "", err
	}
	return rec, token, nil
}

func (s *Service) ListUsers(ctx context.Context) ([]UserRecord, error) {
	if !s.multiUserEnabled {
		return nil, errors.New("multi-user disabled")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, role, disabled, created_at
		FROM users
		ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []UserRecord
	for rows.Next() {
		rec, err := scanUserRecord(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Service) CountAdmins(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin' AND disabled = 0`).Scan(&count)
	return count, err
}

func (s *Service) DeleteUser(ctx context.Context, id string) error {
	if !s.multiUserEnabled {
		return errors.New("multi-user disabled")
	}

	rec, err := s.getUserByID(ctx, id)
	if err != nil {
		return ErrUserNotFound
	}
	if rec.Role == "admin" {
		count, err := s.CountAdmins(ctx)
		if err != nil {
			return err
		}
		if count <= 1 {
			return ErrLastAdmin
		}
	}

	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (s *Service) RotateToken(ctx context.Context, id string) (string, error) {
	if !s.multiUserEnabled {
		return "", errors.New("multi-user disabled")
	}

	token, err := GenerateToken()
	if err != nil {
		return "", err
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE users SET token_hash = ? WHERE id = ? AND disabled = 0`,
		HashToken(token), id)
	if err != nil {
		return "", err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", ErrUserNotFound
	}
	return token, nil
}

func (s *Service) insertUser(ctx context.Context, id, name, tokenHash, role string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (id, name, token_hash, role, disabled, created_at)
		VALUES (?, ?, ?, ?, 0, ?)`,
		id, name, tokenHash, role, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return ErrUserExists
	}
	return err
}

func (s *Service) getUserByID(ctx context.Context, id string) (UserRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, role, disabled, created_at
		FROM users WHERE id = ?`, id)
	return scanUserRecord(row.Scan)
}

type scanFn func(dest ...any) error

func scanUserRecord(scan scanFn) (UserRecord, error) {
	var rec UserRecord
	var disabled int
	var created string
	if err := scan(&rec.ID, &rec.Name, &rec.Role, &disabled, &created); err != nil {
		return UserRecord{}, err
	}
	rec.Disabled = disabled != 0
	rec.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return rec, nil
}

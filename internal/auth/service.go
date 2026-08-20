package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/apperr"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
)

type Repository interface {
	CreateUserIfAbsent(ctx context.Context, user *domain.User) error
	GetUserByUsername(ctx context.Context, username string) (*domain.User, error)
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
	CreateSession(ctx context.Context, session *domain.Session) error
	GetSessionByTokenHash(ctx context.Context, tokenHash string) (*domain.Session, error)
	RevokeSession(ctx context.Context, tokenHash string, revokedAt string) (int, error)
	DeleteExpiredSessions(ctx context.Context, before string) (int, error)
}

type BootstrapUser struct {
	Username string
	Password string
	Role     domain.AuthRole
}

type Service struct {
	repo         Repository
	clock        apperr.Clock
	sessionTTL   time.Duration
	passwordCost int
}

type LoginResult struct {
	Token     string
	ExpiresAt time.Time
	Principal domain.Principal
}

func New(repo Repository, clock apperr.Clock, sessionTTL time.Duration, passwordCost int) *Service {
	return &Service{repo: repo, clock: clock, sessionTTL: sessionTTL, passwordCost: passwordCost}
}

func (s *Service) Bootstrap(ctx context.Context, users []BootstrapUser) error {
	for _, candidate := range users {
		if strings.TrimSpace(candidate.Username) == "" || candidate.Password == "" || !candidate.Role.Valid() {
			return fmt.Errorf("invalid bootstrap user")
		}
		if _, err := s.repo.GetUserByUsername(ctx, candidate.Username); err == nil {
			continue
		} else if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check bootstrap user %s: %w", candidate.Username, err)
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(candidate.Password), s.passwordCost)
		if err != nil {
			return fmt.Errorf("hash bootstrap password: %w", err)
		}
		now := s.clock.Now().UTC()
		user := &domain.User{
			ID: uuid.NewString(), Username: candidate.Username, PasswordHash: string(hash),
			Role: candidate.Role, Active: true, CreatedAt: now, UpdatedAt: now,
		}
		if err := s.repo.CreateUserIfAbsent(ctx, user); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Login(ctx context.Context, username, password string) (*LoginResult, error) {
	user, err := s.repo.GetUserByUsername(ctx, strings.TrimSpace(username))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperr.Unauthorized("invalid username or password")
		}
		return nil, apperr.Wrap(apperr.CodeInternal, "load operator identity", err)
	}
	if !user.Active || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return nil, apperr.Unauthorized("invalid username or password")
	}
	token, err := randomToken()
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "generate session token", err)
	}
	now := s.clock.Now().UTC()
	session := &domain.Session{
		ID: uuid.NewString(), UserID: user.ID, TokenHash: HashToken(token),
		ExpiresAt: now.Add(s.sessionTTL), CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.CreateSession(ctx, session); err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "persist operator session", err)
	}
	return &LoginResult{
		Token: token, ExpiresAt: session.ExpiresAt,
		Principal: domain.Principal{UserID: user.ID, Username: user.Username, Role: user.Role, SessionID: session.ID},
	}, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (domain.Principal, error) {
	if strings.TrimSpace(token) == "" {
		return domain.Principal{}, apperr.Unauthorized("missing bearer token")
	}
	tokenHash := HashToken(token)
	session, err := s.repo.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Principal{}, apperr.Unauthorized("invalid bearer token")
		}
		return domain.Principal{}, apperr.Wrap(apperr.CodeInternal, "load session", err)
	}
	if session.RevokedAt != nil {
		return domain.Principal{}, apperr.Unauthorized("session has been revoked")
	}
	if !s.clock.Now().UTC().Before(session.ExpiresAt) {
		return domain.Principal{}, apperr.Unauthorized("session has expired")
	}
	user, err := s.repo.GetUserByID(ctx, session.UserID)
	if err != nil || !user.Active {
		return domain.Principal{}, apperr.Unauthorized("operator identity is unavailable")
	}
	return domain.Principal{UserID: user.ID, Username: user.Username, Role: user.Role, SessionID: session.ID}, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if _, err := s.Authenticate(ctx, token); err != nil {
		return err
	}
	now := s.clock.Now().UTC().Format(time.RFC3339Nano)
	affected, err := s.repo.RevokeSession(ctx, HashToken(token), now)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "revoke session", err)
	}
	if affected != 1 {
		return apperr.Unauthorized("session is no longer active")
	}
	return nil
}

func (s *Service) CleanupExpired(ctx context.Context) (int, error) {
	return s.repo.DeleteExpiredSessions(ctx, s.clock.Now().UTC().Format(time.RFC3339Nano))
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/apperr"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
)

const ctxKeyPrincipal contextKey = "principal"

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := bearerToken(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		principal, err := s.authSvc.Authenticate(r.Context(), token)
		if err != nil {
			writeError(w, r, err)
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeyPrincipal, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) requireRoles(roles ...domain.AuthRole) func(http.Handler) http.Handler {
	allowed := make(map[domain.AuthRole]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := principalFromContext(r.Context())
			if !ok {
				writeError(w, r, apperr.Unauthorized("authentication required"))
				return
			}
			if _, ok := allowed[principal.Role]; !ok {
				writeError(w, r, apperr.Forbidden("operator role cannot perform this action"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func principalFromContext(ctx context.Context) (domain.Principal, bool) {
	principal, ok := ctx.Value(ctxKeyPrincipal).(domain.Principal)
	return principal, ok
}

func actorFromRequest(r *http.Request) string {
	principal, ok := principalFromContext(r.Context())
	if !ok {
		return "unknown"
	}
	return principal.Username
}

func partyFromRequest(r *http.Request) domain.PartyRole {
	principal, ok := principalFromContext(r.Context())
	if !ok {
		return domain.PartyOperationsDispatch
	}
	switch principal.Role {
	case domain.RoleEngineer:
		return domain.PartyFieldEngineering
	case domain.RoleAuditor:
		return domain.PartySafetyAudit
	default:
		return domain.PartyOperationsDispatch
	}
}

func bearerToken(r *http.Request) (string, error) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", apperr.Unauthorized("valid bearer authorization is required")
	}
	return parts[1], nil
}

package server

import (
	"net/http"
	"time"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/apperr"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/audit"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token     string     `json:"token"`
	ExpiresAt string     `json:"expires_at"`
	User      meResponse `json:"user"`
}

type meResponse struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

func (s *Server) Login() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request loginRequest
		if err := decodeJSON(r, &request); err != nil {
			writeValidation(w, r, "invalid login request")
			return
		}
		result, err := s.authSvc.Login(r.Context(), request.Username, request.Password)
		if err != nil {
			writeError(w, r, err)
			return
		}
		_ = s.auditSvc.Record(r.Context(), audit.Entry{
			Actor: result.Principal.Username, Action: "login", EntityType: domain.EntitySession,
			EntityID: result.Principal.SessionID, After: map[string]string{"role": string(result.Principal.Role)},
			RequestID: requestIDFromContext(r.Context()),
		})
		writeJSON(w, http.StatusOK, loginResponse{
			Token: result.Token, ExpiresAt: result.ExpiresAt.Format(time.RFC3339),
			User: meResponse{ID: result.Principal.UserID, Username: result.Principal.Username, Role: string(result.Principal.Role)},
		})
	}
}

func (s *Server) Logout() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, err := bearerToken(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		principal, _ := principalFromContext(r.Context())
		if err := s.authSvc.Logout(r.Context(), token); err != nil {
			writeError(w, r, err)
			return
		}
		_ = s.auditSvc.Record(r.Context(), audit.Entry{
			Actor: principal.Username, Action: "logout", EntityType: domain.EntitySession,
			EntityID: principal.SessionID, Before: map[string]string{"status": "active"},
			After: map[string]string{"status": "revoked"}, RequestID: requestIDFromContext(r.Context()),
		})
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) Me() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := principalFromContext(r.Context())
		if !ok {
			writeError(w, r, apperr.Unauthorized("authentication required"))
			return
		}
		writeJSON(w, http.StatusOK, meResponse{ID: principal.UserID, Username: principal.Username, Role: string(principal.Role)})
	}
}

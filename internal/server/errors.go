package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/apperr"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
)

// writeError writes a structured JSON error response with the correct HTTP status code.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	code := apperr.AsCode(err)
	status := httpStatusForCode(code)

	resp := ErrorResponse{
		Error:     err.Error(),
		Code:      string(code),
		RequestID: requestIDFromContext(r.Context()),
	}
	if errors.Is(err, domain.ErrInvalidState) {
		resp.Code = string(apperr.CodeInvalidState)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func httpStatusForCode(code apperr.Code) int {
	switch code {
	case apperr.CodeNotFound:
		return http.StatusNotFound
	case apperr.CodeConflict:
		return http.StatusConflict
	case apperr.CodeInvalidState:
		return http.StatusUnprocessableEntity
	case apperr.CodeValidationFailed:
		return http.StatusBadRequest
	case apperr.CodeQuotaExceeded:
		return http.StatusUnprocessableEntity
	case apperr.CodeDeadlineExceeded:
		return http.StatusGatewayTimeout
	case apperr.CodeLeaseExpired:
		return http.StatusConflict
	case apperr.CodePreempted:
		return http.StatusConflict
	case apperr.CodeDuplicate:
		return http.StatusConflict
	case apperr.CodeForbidden:
		return http.StatusForbidden
	case apperr.CodeUnauthorized:
		return http.StatusUnauthorized
	case apperr.CodeUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// writeValidation writes a validation error.
func writeValidation(w http.ResponseWriter, r *http.Request, msg string) {
	writeJSON(w, http.StatusBadRequest, ErrorResponse{
		Error:     msg,
		Code:      string(apperr.CodeValidationFailed),
		RequestID: requestIDFromContext(r.Context()),
	})
}

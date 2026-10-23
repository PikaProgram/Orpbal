package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"Orpbal/ent"
	"Orpbal/internal/api"
	"Orpbal/internal/auth"
	"Orpbal/internal/security/crypto"
	"Orpbal/internal/target"
)

// Handler implements the generated OpenAPI server interface.
type Handler struct {
	client   *ent.Client
	sessions *auth.Service
	cipher   *crypto.Cipher
	secure   bool
	logger   zerolog.Logger
	viewer   *target.Viewer
}

var _ api.ServerInterface = (*Handler)(nil)

// New builds the HTTP handler.
func New(client *ent.Client, sessions *auth.Service, cipher *crypto.Cipher, secure bool, logger zerolog.Logger) *Handler {
	return &Handler{
		client:   client,
		sessions: sessions,
		cipher:   cipher,
		secure:   secure,
		logger:   logger,
		viewer:   target.NewViewer(nil),
	}
}

// ParameterError writes generated-parameter failures in the contract error shape.
func (h *Handler) ParameterError(c *gin.Context, err error, statusCode int) {
	writeError(c, statusCode, "invalid_request", err.Error())
}

func (h *Handler) fail(c *gin.Context, err error, action string) {
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(c, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
	case errors.Is(err, auth.ErrInvalidSession):
		writeError(c, http.StatusUnauthorized, "unauthorized", "authentication required")
	case errors.Is(err, auth.ErrLastAdmin):
		writeError(c, http.StatusConflict, "last_admin", "the last active platform admin cannot be removed")
	case ent.IsNotFound(err):
		writeError(c, http.StatusNotFound, "not_found", "resource not found")
	case ent.IsConstraintError(err):
		writeError(c, http.StatusConflict, "conflict", "resource already exists")
	default:
		h.logger.Error().Err(err).Str("action", action).Msg("Request failed")
		writeError(c, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, api.ErrorResponse{
		Error: api.ErrorDetail{Code: code, Message: message},
	})
}

func writeFields(c *gin.Context, fields map[string]string) {
	c.JSON(http.StatusBadRequest, api.ErrorResponse{
		Error: api.ErrorDetail{
			Code:    "validation_failed",
			Message: "request validation failed",
			Fields:  &fields,
		},
	})
}

func bindJSON(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "request body is not valid JSON")
		return false
	}
	return true
}

func toAPIUser(user *ent.PlatformUser) api.User {
	return api.User{
		Id:              user.ID,
		Username:        user.Username,
		IsPlatformAdmin: user.IsPlatformAdmin,
		IsActive:        user.IsActive,
		CreatedAt:       user.CreatedAt,
		UpdatedAt:       user.UpdatedAt,
	}
}

func toAPIGrant(grant *ent.DatabaseGrant) api.Grant {
	return api.Grant{
		Id:           grant.ID,
		GroupId:      grant.GroupID,
		ConnectionId: grant.ConnectionID,
		AccessLevel:  api.AccessLevel(grant.AccessLevel),
		CreatedAt:    grant.CreatedAt,
		UpdatedAt:    grant.UpdatedAt,
	}
}

func (h *Handler) currentUser(c *gin.Context) *ent.PlatformUser {
	return auth.CurrentUser(c)
}

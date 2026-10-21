package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"Orpbal/internal/api"
	"Orpbal/internal/auth"
	"Orpbal/internal/security/password"
)

// Login verifies credentials and creates a session.
func (h *Handler) Login(c *gin.Context) {
	var req api.LoginRequest
	if !bindJSON(c, &req) {
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" || req.Password == "" {
		writeFields(c, map[string]string{"username": "required", "password": "required"})
		return
	}

	oldToken, _ := c.Cookie(auth.CookieName)
	session, err := h.sessions.Login(c.Request.Context(), username, req.Password, oldToken)
	if err != nil {
		h.fail(c, err, "login")
		return
	}

	auth.SetSessionCookie(c, session.RawToken, session.Row.AbsoluteExpiresAt, h.secure)
	c.JSON(http.StatusOK, api.Session{
		User:      toAPIUser(session.User),
		CsrfToken: session.CSRFToken,
	})
}

// Logout revokes the current session.
func (h *Handler) Logout(c *gin.Context, _ api.LogoutParams) {
	session := auth.CurrentSession(c)
	if session == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if err := h.sessions.Logout(c.Request.Context(), session.ID); err != nil {
		h.fail(c, err, "logout")
		return
	}
	auth.ClearSessionCookie(c, h.secure)
	c.Status(http.StatusNoContent)
}

// GetSession returns the current user and CSRF token.
func (h *Handler) GetSession(c *gin.Context) {
	user := h.currentUser(c)
	if user == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	c.JSON(http.StatusOK, api.Session{
		User:      toAPIUser(user),
		CsrfToken: auth.CurrentCSRF(c),
	})
}

// ChangePassword updates the current user's password.
func (h *Handler) ChangePassword(c *gin.Context, _ api.ChangePasswordParams) {
	var req api.ChangePasswordRequest
	if !bindJSON(c, &req) {
		return
	}
	user := h.currentUser(c)
	session := auth.CurrentSession(c)
	if user == nil || session == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if err := password.ValidatePassword(req.NewPassword, user.Username); err != nil {
		writeFields(c, map[string]string{"new_password": err.Error()})
		return
	}

	rawToken, err := h.sessions.ChangePassword(c.Request.Context(), session, req.CurrentPassword, req.NewPassword)
	if err != nil {
		if err == auth.ErrInvalidCredentials {
			writeFields(c, map[string]string{"current_password": "incorrect password"})
			return
		}
		h.fail(c, err, "change password")
		return
	}

	auth.SetSessionCookie(c, rawToken, session.AbsoluteExpiresAt, h.secure)
	c.Status(http.StatusNoContent)
}

// GetHealth reports service health.
func (h *Handler) GetHealth(c *gin.Context) {
	c.JSON(http.StatusOK, api.Health{Status: "ok"})
}

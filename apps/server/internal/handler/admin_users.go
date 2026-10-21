package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"Orpbal/ent"
	"Orpbal/ent/platformuser"
	"Orpbal/internal/api"
	"Orpbal/internal/auth"
	"Orpbal/internal/security/password"
)

// ListUsers returns every platform user.
func (h *Handler) ListUsers(c *gin.Context) {
	users, err := h.client.PlatformUser.Query().
		Order(ent.Asc(platformuser.FieldID)).
		All(c.Request.Context())
	if err != nil {
		h.fail(c, err, "list users")
		return
	}
	result := make([]api.User, 0, len(users))
	for _, user := range users {
		result = append(result, toAPIUser(user))
	}
	c.JSON(http.StatusOK, result)
}

// CreateUser creates an admin-managed platform user.
func (h *Handler) CreateUser(c *gin.Context, _ api.CreateUserParams) {
	var req api.CreateUserRequest
	if !bindJSON(c, &req) {
		return
	}
	username := strings.TrimSpace(req.Username)
	if err := password.ValidateUsername(username); err != nil {
		writeFields(c, map[string]string{"username": err.Error()})
		return
	}
	if err := password.ValidatePassword(req.Password, username); err != nil {
		writeFields(c, map[string]string{"password": err.Error()})
		return
	}
	hash, err := password.Hash(req.Password)
	if err != nil {
		h.fail(c, err, "hash user password")
		return
	}

	isAdmin := req.IsPlatformAdmin != nil && *req.IsPlatformAdmin
	user, err := h.client.PlatformUser.Create().
		SetUsername(username).
		SetPasswordHash(hash).
		SetIsPlatformAdmin(isAdmin).
		SetIsActive(true).
		Save(c.Request.Context())
	if err != nil {
		h.fail(c, err, "create user")
		return
	}
	c.JSON(http.StatusCreated, toAPIUser(user))
}

// GetUser returns one platform user.
func (h *Handler) GetUser(c *gin.Context, id api.Id) {
	user, err := h.client.PlatformUser.Get(c.Request.Context(), id)
	if err != nil {
		h.fail(c, err, "get user")
		return
	}
	c.JSON(http.StatusOK, toAPIUser(user))
}

// UpdateUser changes activation or admin flags.
func (h *Handler) UpdateUser(c *gin.Context, id api.Id, _ api.UpdateUserParams) {
	var req api.UpdateUserRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.IsActive == nil && req.IsPlatformAdmin == nil {
		writeFields(c, map[string]string{"body": "at least one field is required"})
		return
	}

	user, err := h.client.PlatformUser.Get(c.Request.Context(), id)
	if err != nil {
		h.fail(c, err, "get user for update")
		return
	}

	deactivating := req.IsActive != nil && !*req.IsActive
	demoting := req.IsPlatformAdmin != nil && !*req.IsPlatformAdmin
	if user.IsPlatformAdmin && user.IsActive && (deactivating || demoting) {
		count, err := h.client.PlatformUser.Query().
			Where(platformuser.IsPlatformAdmin(true), platformuser.IsActive(true)).
			Count(c.Request.Context())
		if err != nil {
			h.fail(c, err, "count platform admins")
			return
		}
		if count <= 1 {
			h.fail(c, auth.ErrLastAdmin, "update user")
			return
		}
	}

	update := h.client.PlatformUser.UpdateOneID(user.ID)
	if req.IsActive != nil {
		update.SetIsActive(*req.IsActive)
	}
	if req.IsPlatformAdmin != nil {
		update.SetIsPlatformAdmin(*req.IsPlatformAdmin)
	}
	updated, err := update.Save(c.Request.Context())
	if err != nil {
		h.fail(c, err, "update user")
		return
	}

	adminChanged := req.IsPlatformAdmin != nil && *req.IsPlatformAdmin != user.IsPlatformAdmin
	if deactivating || adminChanged {
		if err := h.sessions.RevokeUserSessions(c.Request.Context(), user.ID); err != nil {
			h.fail(c, err, "revoke user sessions")
			return
		}
	}
	c.JSON(http.StatusOK, toAPIUser(updated))
}

// ResetUserPassword sets a user's password and revokes their sessions.
func (h *Handler) ResetUserPassword(c *gin.Context, id api.Id, _ api.ResetUserPasswordParams) {
	var req api.ResetPasswordRequest
	if !bindJSON(c, &req) {
		return
	}
	user, err := h.client.PlatformUser.Get(c.Request.Context(), id)
	if err != nil {
		h.fail(c, err, "get user for password reset")
		return
	}
	if err := password.ValidatePassword(req.NewPassword, user.Username); err != nil {
		writeFields(c, map[string]string{"new_password": err.Error()})
		return
	}
	hash, err := password.Hash(req.NewPassword)
	if err != nil {
		h.fail(c, err, "hash reset password")
		return
	}
	if err := h.client.PlatformUser.UpdateOneID(user.ID).SetPasswordHash(hash).Exec(c.Request.Context()); err != nil {
		h.fail(c, err, "reset user password")
		return
	}
	if err := h.sessions.RevokeUserSessions(c.Request.Context(), user.ID); err != nil {
		h.fail(c, err, "revoke reset user sessions")
		return
	}
	c.Status(http.StatusNoContent)
}

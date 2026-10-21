package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"Orpbal/ent"
	"Orpbal/ent/databasegrant"
	"Orpbal/internal/api"
)

// ListGrants returns grants, optionally filtered by group or connection.
func (h *Handler) ListGrants(c *gin.Context, params api.ListGrantsParams) {
	query := h.client.DatabaseGrant.Query().Order(ent.Asc(databasegrant.FieldID))
	if params.GroupId != nil {
		query = query.Where(databasegrant.GroupID(*params.GroupId))
	}
	if params.ConnectionId != nil {
		query = query.Where(databasegrant.ConnectionID(*params.ConnectionId))
	}
	grants, err := query.All(c.Request.Context())
	if err != nil {
		h.fail(c, err, "list grants")
		return
	}
	result := make([]api.Grant, 0, len(grants))
	for _, grant := range grants {
		result = append(result, toAPIGrant(grant))
	}
	c.JSON(http.StatusOK, result)
}

// SetGrant creates or updates a group's access to a connection.
func (h *Handler) SetGrant(c *gin.Context, id api.Id, connectionId api.ConnectionId, _ api.SetGrantParams) {
	var req api.SetGrantRequest
	if !bindJSON(c, &req) {
		return
	}
	if !req.AccessLevel.Valid() {
		writeFields(c, map[string]string{"access_level": "must be view_only or view_edit"})
		return
	}

	ctx := c.Request.Context()
	if _, err := h.client.Group.Get(ctx, id); err != nil {
		h.fail(c, err, "get group for grant")
		return
	}
	if _, err := h.client.DatabaseConnection.Get(ctx, connectionId); err != nil {
		h.fail(c, err, "get connection for grant")
		return
	}

	existing, err := h.client.DatabaseGrant.Query().
		Where(
			databasegrant.GroupID(id),
			databasegrant.ConnectionID(connectionId),
		).
		Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		h.fail(c, err, "query grant")
		return
	}
	if ent.IsNotFound(err) {
		_, err = h.client.DatabaseGrant.Create().
			SetGroupID(id).
			SetConnectionID(connectionId).
			SetAccessLevel(databasegrant.AccessLevel(req.AccessLevel)).
			Save(ctx)
		if err != nil && !ent.IsConstraintError(err) {
			h.fail(c, err, "create grant")
			return
		}
		if ent.IsConstraintError(err) {
			// A concurrent request created it; fall through to update.
			existing, err = h.client.DatabaseGrant.Query().
				Where(databasegrant.GroupID(id), databasegrant.ConnectionID(connectionId)).
				Only(ctx)
			if err != nil {
				h.fail(c, err, "reload grant")
				return
			}
		}
	}
	if existing != nil {
		if err := h.client.DatabaseGrant.UpdateOneID(existing.ID).
			SetAccessLevel(databasegrant.AccessLevel(req.AccessLevel)).
			Exec(ctx); err != nil {
			h.fail(c, err, "update grant")
			return
		}
	}
	c.Status(http.StatusNoContent)
}

// DeleteGrant removes a group's access to a connection.
func (h *Handler) DeleteGrant(c *gin.Context, id api.Id, connectionId api.ConnectionId, _ api.DeleteGrantParams) {
	count, err := h.client.DatabaseGrant.Delete().
		Where(
			databasegrant.GroupID(id),
			databasegrant.ConnectionID(connectionId),
		).
		Exec(c.Request.Context())
	if err != nil {
		h.fail(c, err, "delete grant")
		return
	}
	if count == 0 {
		writeError(c, http.StatusNotFound, "not_found", "grant not found")
		return
	}
	c.Status(http.StatusNoContent)
}

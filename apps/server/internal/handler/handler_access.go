package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"Orpbal/ent"
	"Orpbal/ent/databasegrant"
	"Orpbal/internal/access"
	"Orpbal/internal/api"
	"Orpbal/internal/target"
)

// authorizedTarget authenticates, checks effective access (require == ""
// means any effective access, otherwise the level must equal require), then
// opens the target for the connection.
func (h *Handler) authorizedTarget(c *gin.Context, id api.Id, require databasegrant.AccessLevel) (*target.Target, bool) {
	user := h.currentUser(c)
	if user == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return nil, false
	}
	level, err := access.ForConnection(c.Request.Context(), h.client, user.ID, int(id))
	if errors.Is(err, access.ErrNoAccess) {
		writeError(c, http.StatusForbidden, "forbidden", "no access to this database")
		return nil, false
	}
	if err != nil {
		h.fail(c, err, "check database access")
		return nil, false
	}
	if require != "" && level != require {
		writeError(c, http.StatusForbidden, "forbidden", "view_edit access required")
		return nil, false
	}
	t, err := h.manager.Open(c.Request.Context(), h.client, int(id))
	if ent.IsNotFound(err) {
		writeError(c, http.StatusNotFound, "not_found", "database connection not found")
		return nil, false
	}
	if err != nil {
		h.fail(c, err, "open target")
		return nil, false
	}
	return t, true
}

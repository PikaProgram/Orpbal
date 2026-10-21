package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"Orpbal/internal/access"
	"Orpbal/internal/api"
)

// ListMyDatabases returns enabled connections with EffectiveAccess.
func (h *Handler) ListMyDatabases(c *gin.Context) {
	user := h.currentUser(c)
	if user == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	connections, err := access.ForUser(c.Request.Context(), h.client, user.ID)
	if err != nil {
		h.fail(c, err, "list my databases")
		return
	}

	result := make([]api.MyDatabase, 0, len(connections))
	for _, connection := range connections {
		result = append(result, api.MyDatabase{
			ConnectionId: connection.ConnectionID,
			Name:         connection.Name,
			Host:         connection.Host,
			DatabaseName: connection.DatabaseName,
			AccessLevel:  api.AccessLevel(connection.Level),
		})
	}
	c.JSON(http.StatusOK, result)
}

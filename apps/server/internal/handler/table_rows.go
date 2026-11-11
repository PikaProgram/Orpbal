package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"Orpbal/ent/databasegrant"
	"Orpbal/internal/api"
	"Orpbal/internal/target"
)

// ListTableCounts returns exact and estimated row counts for every table.
func (h *Handler) ListTableCounts(c *gin.Context, id api.Id) {
	t, ok := h.authorizedTarget(c, id, "")
	if !ok {
		return
	}
	counts, err := t.TableCounts(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusBadGateway, "target_unreachable", err.Error())
		return
	}
	c.JSON(http.StatusOK, counts)
}

// MutateRows applies one insert, update, or delete to a target table.
// Writes are gated to view_edit; the table must have a primary key for
// update and delete so every affected row is unambiguous.
func (h *Handler) MutateRows(c *gin.Context, id api.Id, _ api.MutateRowsParams) {
	body := api.MutateRowsRequest{}
	if !bindJSON(c, &body) {
		return
	}
	t, ok := h.authorizedTarget(c, id, databasegrant.AccessLevelViewEdit)
	if !ok {
		return
	}
	affected, err := t.Mutate(c.Request.Context(), body)
	if err != nil {
		if errors.Is(err, target.ErrInvalidMutation) {
			writeError(c, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		writeError(c, http.StatusBadGateway, "target_unreachable", err.Error())
		return
	}
	c.JSON(http.StatusOK, api.MutationResult{Affected: int(affected)})
}

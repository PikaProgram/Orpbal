package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"Orpbal/ent"
	"Orpbal/ent/databasegrant"
	"Orpbal/ent/migration"
	"Orpbal/internal/api"
	"Orpbal/internal/sqllog"
)

// DeleteDatabaseConnection removes a managed target together with its grants.
// Connections that already carry migration history are refused: the linear
// migration history must stay addressable, so the admin disables the
// connection instead (PATCH is_enabled=false).
func (h *Handler) DeleteDatabaseConnection(c *gin.Context, id api.Id, _ api.DeleteDatabaseConnectionParams) {
	ctx := c.Request.Context()
	if _, err := h.client.DatabaseConnection.Get(ctx, id); err != nil {
		h.fail(c, err, "get database connection for delete")
		return
	}
	if exists, err := h.client.Migration.Query().
		Where(migration.ConnectionID(int(id))).
		Exist(ctx); err != nil {
		h.fail(c, err, "check migration history for delete")
		return
	} else if exists {
		writeError(c, http.StatusConflict, "conflict", "connection still has migration history; disable it instead of deleting")
		return
	}
	// Drop the pooled connection before the row disappears so the stored
	// credentials remain resolvable for the invalidation.
	h.manager.InvalidateConnection(ctx, h.client, int(id))
	tx, err := h.client.Tx(ctx)
	if err != nil {
		h.fail(c, err, "delete database connection")
		return
	}
	if _, err := tx.DatabaseGrant.Delete().
		Where(databasegrant.ConnectionID(int(id))).Exec(ctx); err != nil {
		_ = tx.Rollback()
		h.fail(c, err, "delete database grants")
		return
	}
	if err := tx.DatabaseConnection.DeleteOneID(id).Exec(ctx); err != nil {
		_ = tx.Rollback()
		h.fail(c, err, "delete database connection")
		return
	}
	if err := tx.Commit(); err != nil {
		h.fail(c, err, "commit delete database connection")
		return
	}
	c.Status(http.StatusNoContent)
}

// TestDatabaseConnection pings an existing managed connection using its
// stored credentials, without changing anything. Failure detail is redacted
// before it reaches the response or logs.
func (h *Handler) TestDatabaseConnection(c *gin.Context, id api.Id, _ api.TestDatabaseConnectionParams) {
	ctx := c.Request.Context()
	// Disabled connections are testable too: an admin verifies before
	// re-enabling, so OpenByID skips the enabled filter here.
	t, err := h.manager.OpenByID(ctx, h.client, int(id), false)
	if err != nil {
		if ent.IsNotFound(err) {
			writeError(c, http.StatusNotFound, "not_found", "resource not found")
			return
		}
		// Open fails on decrypt/DNS-assembly problems; connection failures
		// themselves surface at ping time below.
		writeError(c, http.StatusBadGateway, "target_unreachable", "could not prepare the target connection: "+sqllog.Redact(err.Error()))
		return
	}
	if err := t.Ping(ctx); err != nil {
		h.logger.Warn().Err(err).Str("connection", t.Name).Msg("Target connection test failed")
		writeError(c, http.StatusBadGateway, "target_unreachable", "could not connect to the target database: "+sqllog.Redact(err.Error()))
		return
	}
	c.JSON(http.StatusOK, api.ConnectionTestResult{Reachable: true})
}

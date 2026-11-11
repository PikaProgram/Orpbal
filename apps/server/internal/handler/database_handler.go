package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"Orpbal/internal/api"
	"Orpbal/internal/target"
)

const cellLimit = 4096

// GetDatabaseSchema lists every table with columns, constraints, and indexes
// in a managed target. Requires effective access (view_only or view_edit).
func (h *Handler) GetDatabaseSchema(c *gin.Context, id api.Id) {
	t, ok := h.authorizedTarget(c, id, "")
	if !ok {
		return
	}
	schema, err := t.IntrospectSchema(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusBadGateway, "target_unreachable", err.Error())
		return
	}
	c.JSON(http.StatusOK, schema)
}

// ExecuteDatabaseQuery runs one read-only statement against a managed target.
func (h *Handler) ExecuteDatabaseQuery(c *gin.Context, id api.Id, params api.ExecuteDatabaseQueryParams) {
	body := api.ExecuteQueryRequest{}
	if !bindJSON(c, &body) {
		return
	}
	if err := target.ReadableStatement(body.Sql); err != nil {
		writeFields(c, map[string]string{"sql": err.Error()})
		return
	}
	t, ok := h.authorizedTarget(c, id, "")
	if !ok {
		return
	}
	limit := 100
	if body.Limit != nil {
		limit = *body.Limit
	}
	// Zero/negative would loop uncapped in the scan, bypassing the ceiling.
	if limit < 1 {
		limit = 1
	}
	// Hard server ceiling: an uncapped SELECT against a big target would
	// serialize the entire result into one JSON response.
	const hardCeiling = 10000
	if limit > hardCeiling {
		limit = hardCeiling
	}
	// Offset is applied in Go while scanning, so any SQL statement paginates
	// without the handler having to wrap user SQL.
	offset := 0
	if body.Offset != nil && *body.Offset > 0 {
		offset = *body.Offset
	}
	columns, rows, err := t.Query(c.Request.Context(), body.Sql, limit+offset)
	if err == nil && offset > 0 {
		if offset >= len(rows) {
			rows = nil
		} else {
			rows = rows[offset:]
		}
	}
	if err != nil {
		writeError(c, http.StatusBadGateway, "target_unreachable", err.Error())
		return
	}
	result := api.QueryResult{Columns: columns, Rows: make([]*[]any, 0, len(rows))}
	for _, row := range rows {
		values := make([]any, 0, len(row))
		for _, value := range row {
			values = append(values, target.TruncateCell(target.FormatValue(value), cellLimit))
		}
		result.Rows = append(result.Rows, &values)
	}
	c.JSON(http.StatusOK, result)
}

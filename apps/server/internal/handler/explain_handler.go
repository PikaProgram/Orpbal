package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"Orpbal/internal/api"
	"Orpbal/internal/target"
)

// ExplainDatabaseQuery returns the PostgreSQL execution plan for a statement.
// ANALYZE runs the statement inside the same read-only transaction used by the
// viewer so runtime metrics can be collected without mutating the target.
func (h *Handler) ExplainDatabaseQuery(c *gin.Context, id api.Id, _ api.ExplainDatabaseQueryParams) {
	body := api.ExplainQueryRequest{}
	if !bindJSON(c, &body) {
		return
	}
	if err := target.ReadableStatement(body.Sql); err != nil {
		writeFields(c, map[string]string{"sql": err.Error()})
		return
	}
	user := h.currentUser(c)
	if user == nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if _, ok := h.requireAccess(c, user, id); !ok {
		return
	}
	_, dsnStr, ok := h.loadTargetDSN(c, id)
	if !ok {
		return
	}
	analyze := false
	if body.Analyze != nil {
		analyze = *body.Analyze
	}
	result, err := h.viewer.Explain(c.Request.Context(), dsnStr, body.Sql, analyze)
	if err != nil {
		writeError(c, http.StatusBadGateway, "target_unreachable", err.Error())
		return
	}
	c.JSON(http.StatusOK, toAPIExplainResult(result))
}

func toAPIExplainResult(result target.ExplainResult) api.ExplainResult {
	nodes := make([]api.ExplainNode, 0, len(result.Nodes))
	for _, node := range result.Nodes {
		nodes = append(nodes, toAPIExplainNode(node))
	}
	out := api.ExplainResult{
		Engine:  api.Engine(result.Engine),
		Analyze: result.Analyze,
		Nodes:   nodes,
	}
	if result.Columns != nil {
		columns := result.Columns
		out.Columns = &columns
	}
	if result.Rows != nil {
		rows := make([]*[]any, 0, len(result.Rows))
		for _, row := range result.Rows {
			values := row
			rows = append(rows, &values)
		}
		out.Rows = &rows
	}
	return out
}

func toAPIExplainNode(node target.ExplainNode) api.ExplainNode {
	out := api.ExplainNode{
		NodeType:          node.NodeType,
		StartupCost:       node.StartupCost,
		TotalCost:         node.TotalCost,
		PlanRows:          node.PlanRows,
		PlanWidth:         node.PlanWidth,
		ActualStartupTime: node.ActualStartupTime,
		ActualTotalTime:   node.ActualTotalTime,
		ActualRows:        node.ActualRows,
		ActualLoops:       node.ActualLoops,
	}
	if node.Relation != "" {
		out.Relation = &node.Relation
	}
	if node.Alias != "" {
		out.Alias = &node.Alias
	}
	if node.Index != "" {
		out.Index = &node.Index
	}
	if node.JoinType != "" {
		out.JoinType = &node.JoinType
	}
	if node.ScanDirection != "" {
		out.ScanDirection = &node.ScanDirection
	}
	if node.Filter != "" {
		out.Filter = &node.Filter
	}
	if node.IndexCond != "" {
		out.IndexCond = &node.IndexCond
	}
	if node.SortKey != nil {
		out.SortKey = &node.SortKey
	}
	if len(node.Children) > 0 {
		children := make([]api.ExplainNode, 0, len(node.Children))
		for _, child := range node.Children {
			children = append(children, toAPIExplainNode(child))
		}
		out.Children = &children
	}
	return out
}

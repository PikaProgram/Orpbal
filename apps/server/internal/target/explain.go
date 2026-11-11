package target

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ExplainNode is one node of a PostgreSQL execution plan. Optional fields are
// omitted when the planner does not provide them (for example actual runtime
// metrics without ANALYZE).
type ExplainNode struct {
	NodeType          string        `json:"node_type"`
	Relation          string        `json:"relation,omitempty"`
	Alias             string        `json:"alias,omitempty"`
	Index             string        `json:"index,omitempty"`
	JoinType          string        `json:"join_type,omitempty"`
	ScanDirection     string        `json:"scan_direction,omitempty"`
	Filter            string        `json:"filter,omitempty"`
	IndexCond         string        `json:"index_cond,omitempty"`
	SortKey           []string      `json:"sort_key,omitempty"`
	StartupCost       float64       `json:"startup_cost"`
	TotalCost         float64       `json:"total_cost"`
	PlanRows          int           `json:"plan_rows"`
	PlanWidth         int           `json:"plan_width"`
	ActualStartupTime *float64      `json:"actual_startup_time,omitempty"`
	ActualTotalTime   *float64      `json:"actual_total_time,omitempty"`
	ActualRows        *int          `json:"actual_rows,omitempty"`
	ActualLoops       *int          `json:"actual_loops,omitempty"`
	Children          []ExplainNode `json:"children,omitempty"`
}

// ExplainResult carries either a parsed PostgreSQL plan tree or, for MySQL,
// the tabular rows EXPLAIN returns.
type ExplainResult struct {
	Engine  string        `json:"engine"`
	Analyze bool          `json:"analyze"`
	Nodes   []ExplainNode `json:"nodes"`
	Columns []string      `json:"columns,omitempty"`
	Rows    [][]any       `json:"rows,omitempty"`
}

// pgPlanNode mirrors the JSON shape of EXPLAIN (FORMAT JSON). The planner
// uses spaced keys, so tags carry the exact names.
type pgPlanNode struct {
	NodeType          string       `json:"Node Type"`
	RelationName      string       `json:"Relation Name"`
	Alias             string       `json:"Alias"`
	IndexName         string       `json:"Index Name"`
	JoinType          string       `json:"Join Type"`
	ScanDirection     string       `json:"Scan Direction"`
	Filter            string       `json:"Filter"`
	IndexCond         string       `json:"Index Cond"`
	SortKey           []string     `json:"Sort Key"`
	StartupCost       float64      `json:"Startup Cost"`
	TotalCost         float64      `json:"Total Cost"`
	PlanRows          float64      `json:"Plan Rows"`
	PlanWidth         int          `json:"Plan Width"`
	ActualStartupTime *float64     `json:"Actual Startup Time"`
	ActualTotalTime   *float64     `json:"Actual Total Time"`
	ActualRows        *float64     `json:"Actual Rows"`
	ActualLoops       *int         `json:"Actual Loops"`
	Plans             []pgPlanNode `json:"Plans"`
}

// parsePostgresExplain converts the JSON array returned by EXPLAIN into the
// API tree shape.
func parsePostgresExplain(raw []byte) ([]ExplainNode, error) {
	var plans []struct {
		Plan pgPlanNode `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil {
		return nil, fmt.Errorf("parse explain plan: %w", err)
	}
	if len(plans) == 0 {
		return nil, fmt.Errorf("explain returned no plan")
	}
	nodes := make([]ExplainNode, 0, len(plans))
	for _, plan := range plans {
		nodes = append(nodes, toExplainNode(plan.Plan))
	}
	return nodes, nil
}

func toExplainNode(node pgPlanNode) ExplainNode {
	out := ExplainNode{
		NodeType:          node.NodeType,
		Relation:          node.RelationName,
		Alias:             node.Alias,
		Index:             node.IndexName,
		JoinType:          node.JoinType,
		ScanDirection:     node.ScanDirection,
		Filter:            node.Filter,
		IndexCond:         node.IndexCond,
		SortKey:           node.SortKey,
		StartupCost:       node.StartupCost,
		TotalCost:         node.TotalCost,
		PlanRows:          int(node.PlanRows),
		PlanWidth:         node.PlanWidth,
		ActualStartupTime: node.ActualStartupTime,
		ActualTotalTime:   node.ActualTotalTime,
	}
	if node.ActualRows != nil {
		actualRows := int(*node.ActualRows)
		out.ActualRows = &actualRows
	}
	out.ActualLoops = node.ActualLoops
	if len(node.Plans) > 0 {
		out.Children = make([]ExplainNode, 0, len(node.Plans))
		for _, child := range node.Plans {
			out.Children = append(out.Children, toExplainNode(child))
		}
	}
	return out
}

func toBytes(value any) []byte {
	switch typed := value.(type) {
	case []byte:
		return typed
	case string:
		return []byte(typed)
	default:
		return nil
	}
}

func firstKeyword(statement string) string {
	fields := strings.Fields(statement)
	if len(fields) == 0 {
		return ""
	}
	return strings.ToUpper(fields[0])
}

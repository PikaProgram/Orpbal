package target

import (
	"context"
	"database/sql"
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

// Explain runs EXPLAIN (optionally ANALYZE) for one statement against a target.
// PostgreSQL plans are parsed into a tree so the UI can show scan/index/join
// details plus estimates and actual measurements. MySQL is returned tabular.
func (v *Viewer) Explain(ctx context.Context, stored, statement string, analyze bool) (ExplainResult, error) {
	if err := ReadableStatement(statement); err != nil {
		return ExplainResult{}, err
	}
	if firstKeyword(statement) == "EXPLAIN" {
		return ExplainResult{}, fmt.Errorf("remove the leading EXPLAIN; the planner adds it")
	}
	db, err := v.pool(stored)
	if err != nil {
		return ExplainResult{}, fmt.Errorf("open target database: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()

	if isMySQL(stored) {
		return explainMySQL(ctx, db, statement, analyze)
	}
	return explainPostgres(ctx, db, statement, analyze)
}

// explainPostgres wraps the statement in EXPLAIN (FORMAT JSON) inside a
// read-only transaction. ANALYZE executes the statement, but the read-only
// transaction prevents it from mutating the target; the plan is still measured.
func explainPostgres(ctx context.Context, db *sql.DB, statement string, analyze bool) (ExplainResult, error) {
	options := "FORMAT JSON"
	if analyze {
		options += ", ANALYZE, BUFFERS, TIMING true"
	}
	query := "EXPLAIN (" + options + ") " + statement

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ExplainResult{}, fmt.Errorf("begin read-only transaction: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return ExplainResult{}, fmt.Errorf("explain query: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return ExplainResult{}, fmt.Errorf("read explain result: %w", err)
		}
		return ExplainResult{}, fmt.Errorf("explain returned no plan")
	}
	var value any
	if err := rows.Scan(&value); err != nil {
		return ExplainResult{}, fmt.Errorf("scan explain result: %w", err)
	}
	if err := rows.Err(); err != nil {
		return ExplainResult{}, fmt.Errorf("iterate explain result: %w", err)
	}

	nodes, err := parsePostgresExplain(toBytes(value))
	if err != nil {
		return ExplainResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return ExplainResult{}, fmt.Errorf("commit explain transaction: %w", err)
	}
	return ExplainResult{Engine: "postgres", Analyze: analyze, Nodes: nodes}, nil
}

// explainMySQL uses the engine's textual EXPLAIN output. MySQL has no
// read-only transaction here, so the same allowlist as the viewer applies.
func explainMySQL(ctx context.Context, db *sql.DB, statement string, analyze bool) (ExplainResult, error) {
	if err := MySQLReadableStatement(statement); err != nil {
		return ExplainResult{}, err
	}
	prefix := "EXPLAIN "
	if analyze {
		prefix = "EXPLAIN ANALYZE "
	}
	rows, err := db.QueryContext(ctx, prefix+statement)
	if err != nil {
		return ExplainResult{}, fmt.Errorf("explain query: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return ExplainResult{}, fmt.Errorf("read explain columns: %w", err)
	}
	result := [][]any{}
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return ExplainResult{}, fmt.Errorf("scan explain row: %w", err)
		}
		for i := range values {
			values[i] = FormatValue(values[i])
		}
		result = append(result, values)
	}
	if err := rows.Err(); err != nil {
		return ExplainResult{}, fmt.Errorf("iterate explain rows: %w", err)
	}
	return ExplainResult{
		Engine:  "mysql",
		Analyze: analyze,
		Nodes:   []ExplainNode{},
		Columns: columns,
		Rows:    result,
	}, nil
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

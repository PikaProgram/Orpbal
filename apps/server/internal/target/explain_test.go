package target

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

const postgresPlanJSON = `[
  {
    "Plan": {
      "Node Type": "Nested Loop",
      "Join Type": "Inner",
      "Startup Cost": 0.29,
      "Total Cost": 16.62,
      "Plan Rows": 3,
      "Plan Width": 40,
      "Actual Startup Time": 0.015,
      "Actual Total Time": 0.030,
      "Actual Rows": 2,
      "Actual Loops": 1,
      "Plans": [
        {
          "Node Type": "Seq Scan",
          "Relation Name": "notes",
          "Alias": "n",
          "Startup Cost": 0.00,
          "Total Cost": 1.03,
          "Plan Rows": 3,
          "Plan Width": 36,
          "Actual Startup Time": 0.004,
          "Actual Total Time": 0.005,
          "Actual Rows": 3,
          "Actual Loops": 1,
          "Filter": "(n.body ~~ '%x%'::text)"
        },
        {
          "Node Type": "Index Scan",
          "Relation Name": "tags",
          "Alias": "t",
          "Index Name": "tags_note_id_idx",
          "Startup Cost": 0.29,
          "Total Cost": 2.10,
          "Plan Rows": 1,
          "Plan Width": 4,
          "Actual Startup Time": 0.003,
          "Actual Total Time": 0.004,
          "Actual Rows": 1,
          "Actual Loops": 3,
          "Index Cond": "(note_id = n.id)"
        }
      ]
    }
  }
]`

func TestParsePostgresExplainTree(t *testing.T) {
	nodes, err := parsePostgresExplain([]byte(postgresPlanJSON))
	if err != nil {
		t.Fatalf("parsePostgresExplain() error = %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(nodes))
	}
	root := nodes[0]
	if root.NodeType != "Nested Loop" || root.JoinType != "Inner" {
		t.Fatalf("root = %+v, want Nested Loop/Inner", root)
	}
	if root.ActualRows == nil || *root.ActualRows != 2 {
		t.Fatalf("root actual rows = %v, want 2", root.ActualRows)
	}
	if root.ActualTotalTime == nil || *root.ActualTotalTime != 0.030 {
		t.Fatalf("root actual total time = %v, want 0.030", root.ActualTotalTime)
	}
	if root.PlanRows != 3 || root.TotalCost != 16.62 {
		t.Fatalf("root estimates = rows %d cost %v", root.PlanRows, root.TotalCost)
	}
	if len(root.Children) != 2 {
		t.Fatalf("root children = %d, want 2", len(root.Children))
	}
	scan := root.Children[1]
	if scan.NodeType != "Index Scan" || scan.Index != "tags_note_id_idx" {
		t.Fatalf("index child = %+v", scan)
	}
	if !strings.Contains(scan.IndexCond, "note_id") {
		t.Fatalf("index cond = %v", scan.IndexCond)
	}
}

func TestParsePostgresExplainRejectsInvalidJSON(t *testing.T) {
	if _, err := parsePostgresExplain([]byte("not json")); err == nil {
		t.Fatal("invalid JSON should fail")
	}
	if _, err := parsePostgresExplain([]byte("[]")); err == nil {
		t.Fatal("empty plan should fail")
	}
}

func TestExplainRejectsInvalidStatements(t *testing.T) {
	v := NewViewer(func(string) (*sql.DB, error) { return nil, nil })
	if _, err := v.Explain(context.Background(), "postgres://x", "  ", false); err == nil {
		t.Fatal("empty statement should be rejected")
	}
	if _, err := v.Explain(context.Background(), "postgres://x", "EXPLAIN SELECT 1", false); err == nil {
		t.Fatal("prefixed EXPLAIN should be rejected")
	}
}

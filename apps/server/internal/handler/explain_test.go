package handler

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"Orpbal/internal/api"
)

func TestExplainQueryPlanAnalyzeAndAccess(t *testing.T) {
	env := newTestEnv(t)
	connectionID, userCookie, userCSRF, adminCookie, adminCSRF := env.newGrantedTarget("view_edit")

	table := strings.ReplaceAll(unique("explaintable"), "-", "_")
	if _, err := env.adminDB.Exec(fmt.Sprintf(
		"CREATE TABLE %s (id serial PRIMARY KEY, label text NOT NULL)", table)); err != nil {
		t.Fatalf("create explain fixture table: %v", err)
	}
	t.Cleanup(func() {
		_, _ = env.adminDB.Exec("DROP TABLE IF EXISTS " + table)
	})
	if _, err := env.adminDB.Exec(fmt.Sprintf(
		"INSERT INTO %s (label) VALUES ('one'), ('two'), ('three')", table)); err != nil {
		t.Fatalf("seed explain fixture rows: %v", err)
	}

	statement := fmt.Sprintf("SELECT * FROM %s WHERE id = 1", table)

	rec := env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/explain", connectionID),
		map[string]any{"sql": statement}, userCookie, userCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("explain status = %d, body = %s", rec.Code, rec.Body.String())
	}
	plan := decode[api.ExplainResult](t, rec)
	if plan.Engine != api.Postgres || plan.Analyze {
		t.Fatalf("plan engine/analyze = %s/%v, want postgres/false", plan.Engine, plan.Analyze)
	}
	if len(plan.Nodes) == 0 || plan.Nodes[0].NodeType == "" {
		t.Fatalf("plan nodes = %+v, want a parsed tree", plan.Nodes)
	}
	if plan.Nodes[0].Relation == nil || *plan.Nodes[0].Relation != table {
		t.Fatalf("root relation = %v, want %s", plan.Nodes[0].Relation, table)
	}
	if plan.Nodes[0].ActualTotalTime != nil {
		t.Fatal("plain EXPLAIN should not include actual timings")
	}

	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/explain", connectionID),
		map[string]any{"sql": statement, "analyze": true}, userCookie, userCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("explain analyze status = %d, body = %s", rec.Code, rec.Body.String())
	}
	analyzed := decode[api.ExplainResult](t, rec)
	if !analyzed.Analyze {
		t.Fatal("analyze flag was not set")
	}
	actual := findActualNode(analyzed.Nodes)
	if actual == nil {
		t.Fatalf("analyze plan has no actual metrics: %+v", analyzed.Nodes)
	}
	if actual.ActualTotalTime == nil || actual.ActualRows == nil {
		t.Fatalf("analyze node actual metrics = %+v, want time and rows", actual)
	}

	// Empty SQL is rejected before reaching the target.
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/explain", connectionID),
		map[string]any{"sql": "  "}, userCookie, userCSRF)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty explain sql status = %d, want 400", rec.Code)
	}

	// Invalid SQL surfaces as a target error.
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/explain", connectionID),
		map[string]any{"sql": "SELECT * FROM does_not_exist_xyz"}, userCookie, userCSRF)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("invalid explain sql status = %d, want 502, body = %s", rec.Code, rec.Body.String())
	}

	// View-only users may inspect plans; they still cannot execute migrations.
	viewer, viewerPlain := env.createUserDirect("viewer", false, true)
	env.grantSetup(adminCookie, adminCSRF, connectionID, viewer.ID, "view_only")
	viewerCookie, viewerCSRF := env.login(viewer.Username, viewerPlain)
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/explain", connectionID),
		map[string]any{"sql": statement}, viewerCookie, viewerCSRF)
	if rec.Code != http.StatusOK {
		t.Fatalf("view_only explain status = %d, want 200", rec.Code)
	}

	// Users without a grant, platform admins without a grant, and
	// unauthenticated callers are denied.
	stranger, strangerPlain := env.createUserDirect("stranger", false, true)
	strangerCookie, strangerCSRF := env.login(stranger.Username, strangerPlain)
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/explain", connectionID),
		map[string]any{"sql": statement}, strangerCookie, strangerCSRF)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("ungranted explain status = %d, want 403", rec.Code)
	}
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/explain", connectionID),
		map[string]any{"sql": statement}, adminCookie, adminCSRF)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("admin without grant explain status = %d, want 403", rec.Code)
	}
	rec = env.do(http.MethodPost, fmt.Sprintf("/api/databases/%d/explain", connectionID),
		map[string]any{"sql": statement}, nil, "dummy-csrf")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated explain status = %d, want 401", rec.Code)
	}
}

func findActualNode(nodes []api.ExplainNode) *api.ExplainNode {
	for i := range nodes {
		node := &nodes[i]
		if node.ActualTotalTime != nil || node.ActualRows != nil {
			return node
		}
		if node.Children != nil {
			if found := findActualNode(*node.Children); found != nil {
				return found
			}
		}
	}
	return nil
}

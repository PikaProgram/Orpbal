package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"Orpbal/ent"
	"Orpbal/ent/databasegrant"
	"Orpbal/ent/groupmembership"
	"Orpbal/ent/migrationexecution"

	"github.com/lib/pq"
)

var svcSeq atomic.Int64

// newSvcEnv opens an isolated schema like the handler/queue harnesses and
// returns the ent client plus a connection id.
func newSvcEnv(t *testing.T) (*ent.Client, int) {
	t.Helper()
	rawDSN := os.Getenv("TEST_DATABASE_URL")
	if rawDSN == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	parsed, err := url.Parse(rawDSN)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	client, err := ent.Open("postgres", rawDSN)
	if err != nil {
		t.Fatalf("ent.Open() error = %v", err)
	}
	schema := fmt.Sprintf("stest_%d_%d", os.Getpid(), svcSeq.Add(1))
	adminDB, err := sql.Open("postgres", rawDSN)
	if err != nil {
		_ = client.Close()
		t.Fatalf("admin sql.Open() error = %v", err)
	}
	if _, err := adminDB.Exec("CREATE SCHEMA " + pq.QuoteIdentifier(schema)); err != nil {
		_ = adminDB.Close()
		_ = client.Close()
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminDB.Exec("DROP SCHEMA IF EXISTS "+pq.QuoteIdentifier(schema)+" CASCADE")
		_ = adminDB.Close()
	})
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	schemaDSN := parsed.String()

	schemaClient, err := ent.Open("postgres", schemaDSN)
	if err != nil {
		_ = client.Close()
		t.Fatalf("ent.Open() schema error = %v", err)
	}
	if err := schemaClient.Schema.Create(context.Background()); err != nil {
		_ = schemaClient.Close()
		_ = client.Close()
		t.Fatalf("Schema.Create() error = %v", err)
	}
	t.Cleanup(func() {
		_ = schemaClient.Close()
		_ = client.Close()
	})
	conn, err := schemaClient.DatabaseConnection.Create().
		SetName(fmt.Sprintf("sconn-%d", svcSeq.Add(1))).
		SetHost("localhost").SetPort(5432).
		SetDatabaseName("test").SetUsername("test").
		SetIsEnabled(true).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}
	return schemaClient, conn.ID
}

// svcUser inserts a bare user row and returns it.
func svcUser(t *testing.T, client *ent.Client) *ent.PlatformUser {
	t.Helper()
	user, err := client.PlatformUser.Create().
		SetUsername(fmt.Sprintf("suser-%d", svcSeq.Add(1))).
		SetPasswordHash("x").
		Save(context.Background())
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}

// svcGroup grants a group over the connection; leader controls membership role.
func svcGroup(t *testing.T, client *ent.Client, connectionID, userID int, leader bool) *ent.Group {
	t.Helper()
	group, err := client.Group.Create().
		SetName(fmt.Sprintf("sgroup-%d", svcSeq.Add(1))).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	role := groupmembership.RoleMember
	if leader {
		role = groupmembership.RoleLeader
	}
	if _, err := client.GroupMembership.Create().
		SetGroupID(group.ID).
		SetUserID(userID).
		SetRole(role).
		Save(context.Background()); err != nil {
		t.Fatalf("create membership: %v", err)
	}
	if _, err := client.DatabaseGrant.Create().
		SetGroupID(group.ID).
		SetConnectionID(connectionID).
		SetAccessLevel(databasegrant.AccessLevelViewEdit).
		Save(context.Background()); err != nil {
		t.Fatalf("create grant: %v", err)
	}
	return group
}

func TestSubmitRequiresLeaderEverywhere(t *testing.T) {
	client, connectionID := newSvcEnv(t)
	ctx := context.Background()
	author := svcUser(t, client)

	// Group 1 has a leader; group 2 does not.
	svcGroup(t, client, connectionID, author.ID, true)
	leaderless := svcGroup(t, client, connectionID, author.ID, false)
	_, err := client.GroupMembership.Delete().
		Where(groupmembership.GroupID(leaderless.ID)).
		Exec(ctx)
	if err != nil {
		t.Fatalf("clear leaderless group: %v", err)
	}

	mig, err := CreateMigration(ctx, client, author.ID, connectionID, &CreateBody{SqlUp: "SELECT 1"})
	if err != nil {
		t.Fatalf("CreateMigration: %v", err)
	}

	_, err = Submit(ctx, client, author.ID, connectionID, mig.ID)
	if !errors.Is(err, ErrNoLeaderGroup) {
		t.Fatalf("Submit error = %v, want ErrNoLeaderGroup", err)
	}

	// Migration stays draft and no execution was created.
	entry, err := LoadMigration(ctx, client, connectionID, mig.ID)
	if err != nil {
		t.Fatalf("LoadMigration: %v", err)
	}
	if entry.Status != "draft" {
		t.Fatalf("migration status = %s, want draft", entry.Status)
	}
	n, err := client.MigrationExecution.Query().
		Where(migrationexecution.MigrationID(mig.ID)).
		Count(ctx)
	if err != nil {
		t.Fatalf("count executions: %v", err)
	}
	if n != 0 {
		t.Fatalf("executions after failed submit = %d, want 0", n)
	}
}

func TestApproveRequiresCurrentLeader(t *testing.T) {
	client, connectionID := newSvcEnv(t)
	ctx := context.Background()
	author := svcUser(t, client)

	// One group, led at submit time by the author.
	svcGroup(t, client, connectionID, author.ID, true)
	mig, err := CreateMigration(ctx, client, author.ID, connectionID, &CreateBody{SqlUp: "SELECT 1"})
	if err != nil {
		t.Fatalf("CreateMigration: %v", err)
	}
	if _, err := Submit(ctx, client, author.ID, connectionID, mig.ID); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	approvals, err := client.MigrationApproval.Query().All(ctx)
	if err != nil {
		t.Fatalf("query approvals: %v", err)
	}
	if len(approvals) != 1 {
		t.Fatalf("approvals = %d, want 1", len(approvals))
	}
	approvalID := approvals[0].ID

	// Rotate leadership to another member AFTER submit: the approval row
	// still points at the old leader, but decisions follow the CURRENT one.
	newLeader := svcUser(t, client)
	if _, err := client.GroupMembership.Create().
		SetGroupID(approvals[0].GroupID).
		SetUserID(newLeader.ID).
		SetRole(groupmembership.RoleLeader).
		Save(ctx); err != nil {
		t.Fatalf("add new leader: %v", err)
	}
	if _, err := client.GroupMembership.Update().
		Where(
			groupmembership.GroupID(approvals[0].GroupID),
			groupmembership.UserID(author.ID),
		).
		SetRole(groupmembership.RoleMember).
		Save(ctx); err != nil {
		t.Fatalf("demote old leader: %v", err)
	}

	if _, _, _, err := Approve(ctx, client, author.ID, approvalID); !errors.Is(err, ErrNotLeader) {
		t.Fatalf("old leader approve error = %v, want ErrNotLeader", err)
	}
	entry, _, _, err := Approve(ctx, client, newLeader.ID, approvalID)
	if err != nil {
		t.Fatalf("new leader approve: %v", err)
	}
	if entry.Status != "queued" {
		t.Fatalf("after approval status = %s, want queued (single group)", entry.Status)
	}
}

func TestValidateWriteBatch(t *testing.T) {
	cases := []struct {
		name string
		body CreateBody
	}{
		{
			name: "batch without batch_table",
			body: CreateBody{SqlUp: "SELECT 1", Batch: &BatchBody{
				BatchSize: 10, CursorColumn: "id", Template: "UPDATE t SET x=1 WHERE id > {{last_id}}",
			}},
		},
		{
			name: "irreversible without reason",
			body: CreateBody{SqlUp: "SELECT 1", Irreversible: true},
		},
		{
			name: "invalid cursor column",
			body: CreateBody{SqlUp: "SELECT 1", Batch: &BatchBody{
				BatchSize: 10, CursorColumn: "id; DROP", BatchTable: "t", Template: "UPDATE t SET x=1 WHERE id > {{last_id}}",
			}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateWrite(&tc.body); !errors.Is(err, ErrValidation) {
				t.Fatalf("ValidateWrite error = %v, want ErrValidation", err)
			}
		})
	}

	// The valid counterpart passes.
	valid := CreateBody{SqlUp: "SELECT 1", Batch: &BatchBody{
		BatchSize: 10, CursorColumn: "id", BatchTable: "t", Template: "UPDATE t SET x=1 WHERE id > {{last_id}}",
		MinId: func() *int64 { v := int64(1); return &v }(),
	}}
	if err := ValidateWrite(&valid); err != nil {
		t.Fatalf("ValidateWrite(valid) = %v, want nil (time %s)", err, time.Now())
	}
}

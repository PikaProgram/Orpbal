package access

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"Orpbal/ent"
	"Orpbal/ent/databaseconnection"
	"Orpbal/ent/databasegrant"
	"Orpbal/ent/group"
	"Orpbal/ent/groupmembership"
	"Orpbal/ent/platformuser"
	"Orpbal/ent/session"

	_ "github.com/lib/pq"
)

func TestMax(t *testing.T) {
	tests := []struct {
		name   string
		levels []databasegrant.AccessLevel
		want   databasegrant.AccessLevel
		found  bool
	}{
		{name: "empty"},
		{name: "view only", levels: []databasegrant.AccessLevel{databasegrant.AccessLevelViewOnly}, want: databasegrant.AccessLevelViewOnly, found: true},
		{name: "view edit", levels: []databasegrant.AccessLevel{databasegrant.AccessLevelViewEdit}, want: databasegrant.AccessLevelViewEdit, found: true},
		{name: "highest wins", levels: []databasegrant.AccessLevel{databasegrant.AccessLevelViewOnly, databasegrant.AccessLevelViewEdit}, want: databasegrant.AccessLevelViewEdit, found: true},
		{name: "reversed", levels: []databasegrant.AccessLevel{databasegrant.AccessLevelViewEdit, databasegrant.AccessLevelViewOnly}, want: databasegrant.AccessLevelViewEdit, found: true},
		{name: "unknown ignored", levels: []databasegrant.AccessLevel{"bogus", databasegrant.AccessLevelViewOnly}, want: databasegrant.AccessLevelViewOnly, found: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := Max(tt.levels...)
			if found != tt.found || got != tt.want {
				t.Fatalf("Max() = (%q, %v), want (%q, %v)", got, found, tt.want, tt.found)
			}
		})
	}
}

func TestForConnectionHighestPrivilegeWins(t *testing.T) {
	f := newFixture(t)
	user := f.user("alice")
	viewers := f.group("viewers")
	editors := f.group("editors")
	conn := f.connection("primary", true)

	f.membership(viewers, user)
	f.membership(editors, user)
	f.grant(viewers, conn, databasegrant.AccessLevelViewOnly)
	f.grant(editors, conn, databasegrant.AccessLevelViewEdit)

	level, err := ForConnection(context.Background(), f.client, user.ID, conn.ID)
	if err != nil {
		t.Fatalf("ForConnection() error = %v", err)
	}
	if level != databasegrant.AccessLevelViewEdit {
		t.Fatalf("ForConnection() = %q, want view_edit", level)
	}

	list, err := ForUser(context.Background(), f.client, user.ID)
	if err != nil {
		t.Fatalf("ForUser() error = %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ForUser() returned %d connections, want 1", len(list))
	}
	if list[0].ConnectionID != conn.ID || list[0].Level != databasegrant.AccessLevelViewEdit {
		t.Fatalf("ForUser() = %+v, want connection %d with view_edit", list[0], conn.ID)
	}
}

func TestForConnectionWithoutGrant(t *testing.T) {
	f := newFixture(t)
	user := f.user("bob")
	conn := f.connection("ungranted", true)

	if _, err := ForConnection(context.Background(), f.client, user.ID, conn.ID); !errors.Is(err, ErrNoAccess) {
		t.Fatalf("ForConnection() error = %v, want ErrNoAccess", err)
	}
	list, err := ForUser(context.Background(), f.client, user.ID)
	if err != nil {
		t.Fatalf("ForUser() error = %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("ForUser() returned %d connections, want 0", len(list))
	}
}

func TestDisabledConnectionIsInaccessible(t *testing.T) {
	f := newFixture(t)
	user := f.user("carol")
	members := f.group("members")
	conn := f.connection("disabled", false)

	f.membership(members, user)
	f.grant(members, conn, databasegrant.AccessLevelViewEdit)

	if _, err := ForConnection(context.Background(), f.client, user.ID, conn.ID); !errors.Is(err, ErrNoAccess) {
		t.Fatalf("ForConnection() error = %v, want ErrNoAccess", err)
	}
	list, err := ForUser(context.Background(), f.client, user.ID)
	if err != nil {
		t.Fatalf("ForUser() error = %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("ForUser() returned %d connections, want 0", len(list))
	}
}

func TestForUserListsSortedGrantedConnections(t *testing.T) {
	f := newFixture(t)
	user := f.user("dave")
	members := f.group("members")
	zulu := f.connection("zulu", true)
	alpha := f.connection("alpha", true)
	f.connection("ungranted", true)

	f.membership(members, user)
	f.grant(members, zulu, databasegrant.AccessLevelViewOnly)
	f.grant(members, alpha, databasegrant.AccessLevelViewEdit)

	list, err := ForUser(context.Background(), f.client, user.ID)
	if err != nil {
		t.Fatalf("ForUser() error = %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ForUser() returned %d connections, want 2", len(list))
	}
	if !strings.HasPrefix(list[0].Name, "alpha-") || !strings.HasPrefix(list[1].Name, "zulu-") {
		t.Fatalf("ForUser() order = %q, %q; want alpha then zulu", list[0].Name, list[1].Name)
	}
}

type fixture struct {
	t             *testing.T
	client        *ent.Client
	seq           int
	userIDs       []int
	groupIDs      []int
	connectionIDs []int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	client, err := ent.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("ent.Open() error = %v", err)
	}
	if err := client.Schema.Create(context.Background()); err != nil {
		_ = client.Close()
		t.Fatalf("Schema.Create() error = %v", err)
	}
	f := &fixture{t: t, client: client}
	t.Cleanup(f.cleanup)
	return f
}

func (f *fixture) cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if len(f.groupIDs) > 0 {
		_, _ = f.client.DatabaseGrant.Delete().Where(databasegrant.GroupIDIn(f.groupIDs...)).Exec(ctx)
	}
	if len(f.userIDs) > 0 {
		_, _ = f.client.GroupMembership.Delete().Where(groupmembership.UserIDIn(f.userIDs...)).Exec(ctx)
		_, _ = f.client.Session.Delete().Where(session.UserIDIn(f.userIDs...)).Exec(ctx)
		_, _ = f.client.PlatformUser.Delete().Where(platformuser.IDIn(f.userIDs...)).Exec(ctx)
	}
	if len(f.groupIDs) > 0 {
		_, _ = f.client.Group.Delete().Where(group.IDIn(f.groupIDs...)).Exec(ctx)
	}
	if len(f.connectionIDs) > 0 {
		_, _ = f.client.DatabaseConnection.Delete().Where(databaseconnection.IDIn(f.connectionIDs...)).Exec(ctx)
	}
	_ = f.client.Close()
}

func (f *fixture) suffix(name string) string {
	f.seq++
	return name + "-" + strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.Itoa(f.seq)
}

func (f *fixture) user(name string) *ent.PlatformUser {
	u, err := f.client.PlatformUser.Create().
		SetUsername(f.suffix(name)).
		SetPasswordHash("test-hash").
		Save(context.Background())
	if err != nil {
		f.t.Fatalf("create user: %v", err)
	}
	f.userIDs = append(f.userIDs, u.ID)
	return u
}

func (f *fixture) group(name string) *ent.Group {
	g, err := f.client.Group.Create().
		SetName(f.suffix(name)).
		Save(context.Background())
	if err != nil {
		f.t.Fatalf("create group: %v", err)
	}
	f.groupIDs = append(f.groupIDs, g.ID)
	return g
}

func (f *fixture) connection(name string, enabled bool) *ent.DatabaseConnection {
	c, err := f.client.DatabaseConnection.Create().
		SetName(f.suffix(name)).
		SetHost("localhost").
		SetPort(5432).
		SetDatabaseName("app").
		SetUsername("postgres").
		SetIsEnabled(enabled).
		Save(context.Background())
	if err != nil {
		f.t.Fatalf("create connection: %v", err)
	}
	f.connectionIDs = append(f.connectionIDs, c.ID)
	return c
}

func (f *fixture) membership(g *ent.Group, u *ent.PlatformUser) {
	_, err := f.client.GroupMembership.Create().
		SetGroupID(g.ID).
		SetUserID(u.ID).
		Save(context.Background())
	if err != nil {
		f.t.Fatalf("create membership: %v", err)
	}
}

func (f *fixture) grant(g *ent.Group, c *ent.DatabaseConnection, level databasegrant.AccessLevel) {
	_, err := f.client.DatabaseGrant.Create().
		SetGroupID(g.ID).
		SetConnectionID(c.ID).
		SetAccessLevel(level).
		Save(context.Background())
	if err != nil {
		f.t.Fatalf("create grant: %v", err)
	}
}

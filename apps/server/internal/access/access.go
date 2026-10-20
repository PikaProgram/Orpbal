package access

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"Orpbal/ent"
	"Orpbal/ent/databaseconnection"
	"Orpbal/ent/databasegrant"
	"Orpbal/ent/group"
	"Orpbal/ent/groupmembership"
)

// ErrNoAccess is returned when a user has no grant on a usable connection.
var ErrNoAccess = errors.New("no access to database")

// ConnectionAccess is the effective access a user has to one connection.
type ConnectionAccess struct {
	ConnectionID int
	Name         string
	Host         string
	DatabaseName string
	Level        databasegrant.AccessLevel
}

// Max returns the strongest valid level, or false when no level is valid.
func Max(levels ...databasegrant.AccessLevel) (databasegrant.AccessLevel, bool) {
	var best databasegrant.AccessLevel
	found := false
	for _, level := range levels {
		if rank(level) == 0 {
			continue
		}
		if !found || rank(level) > rank(best) {
			best, found = level, true
		}
	}
	return best, found
}

// ForConnection computes a user's EffectiveAccess on one enabled connection.
func ForConnection(ctx context.Context, client *ent.Client, userID, connectionID int) (databasegrant.AccessLevel, error) {
	grants, err := client.DatabaseGrant.Query().
		Where(
			databasegrant.HasGroupWith(group.HasMembershipsWith(groupmembership.UserID(userID))),
			databasegrant.HasConnectionWith(
				databaseconnection.ID(connectionID),
				databaseconnection.IsEnabled(true),
			),
		).
		All(ctx)
	if err != nil {
		return "", fmt.Errorf("query database grants: %w", err)
	}

	levels := make([]databasegrant.AccessLevel, 0, len(grants))
	for _, grant := range grants {
		levels = append(levels, grant.AccessLevel)
	}
	level, ok := Max(levels...)
	if !ok {
		return "", ErrNoAccess
	}
	return level, nil
}

// ForUser lists every enabled connection the user can reach and its
// EffectiveAccess, sorted by connection name.
func ForUser(ctx context.Context, client *ent.Client, userID int) ([]ConnectionAccess, error) {
	grants, err := client.DatabaseGrant.Query().
		Where(
			databasegrant.HasGroupWith(group.HasMembershipsWith(groupmembership.UserID(userID))),
			databasegrant.HasConnectionWith(databaseconnection.IsEnabled(true)),
		).
		WithConnection().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query database grants: %w", err)
	}

	byConnection := make(map[int]*ConnectionAccess, len(grants))
	for _, grant := range grants {
		conn := grant.Edges.Connection
		if conn == nil {
			continue
		}
		if current, ok := byConnection[conn.ID]; ok {
			if rank(grant.AccessLevel) > rank(current.Level) {
				current.Level = grant.AccessLevel
			}
			continue
		}
		byConnection[conn.ID] = &ConnectionAccess{
			ConnectionID: conn.ID,
			Name:         conn.Name,
			Host:         conn.Host,
			DatabaseName: conn.DatabaseName,
			Level:        grant.AccessLevel,
		}
	}

	result := make([]ConnectionAccess, 0, len(byConnection))
	for _, item := range byConnection {
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name == result[j].Name {
			return result[i].ConnectionID < result[j].ConnectionID
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func rank(level databasegrant.AccessLevel) int {
	switch level {
	case databasegrant.AccessLevelViewOnly:
		return 1
	case databasegrant.AccessLevelViewEdit:
		return 2
	default:
		return 0
	}
}

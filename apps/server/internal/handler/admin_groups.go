package handler

import (
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"Orpbal/ent"
	"Orpbal/ent/databasegrant"
	"Orpbal/ent/group"
	"Orpbal/ent/groupmembership"
	"Orpbal/internal/api"
)

// ListGroups returns every group.
func (h *Handler) ListGroups(c *gin.Context) {
	groups, err := h.client.Group.Query().
		Order(ent.Asc(group.FieldID)).
		All(c.Request.Context())
	if err != nil {
		h.fail(c, err, "list groups")
		return
	}
	result := make([]api.Group, 0, len(groups))
	for _, item := range groups {
		result = append(result, toAPIGroup(item))
	}
	c.JSON(http.StatusOK, result)
}

// CreateGroup creates a group.
func (h *Handler) CreateGroup(c *gin.Context, _ api.CreateGroupParams) {
	var req api.CreateGroupRequest
	if !bindJSON(c, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if err := validateGroupName(name); err != nil {
		writeFields(c, map[string]string{"name": err.Error()})
		return
	}
	description := ""
	if req.Description != nil {
		description = strings.TrimSpace(*req.Description)
	}
	created, err := h.client.Group.Create().
		SetName(name).
		SetDescription(description).
		Save(c.Request.Context())
	if err != nil {
		h.fail(c, err, "create group")
		return
	}
	c.JSON(http.StatusCreated, toAPIGroup(created))
}

// GetGroup returns a group with its members and grants.
func (h *Handler) GetGroup(c *gin.Context, id api.Id) {
	ctx := c.Request.Context()
	item, err := h.client.Group.Get(ctx, id)
	if err != nil {
		h.fail(c, err, "get group")
		return
	}
	memberships, err := h.client.GroupMembership.Query().
		Where(groupmembership.GroupID(id)).
		WithUser().
		Order(ent.Asc(groupmembership.FieldID)).
		All(ctx)
	if err != nil {
		h.fail(c, err, "get group members")
		return
	}
	grants, err := h.client.DatabaseGrant.Query().
		Where(databasegrant.GroupID(id)).
		Order(ent.Asc(databasegrant.FieldID)).
		All(ctx)
	if err != nil {
		h.fail(c, err, "get group grants")
		return
	}

	members := make([]api.User, 0, len(memberships))
	for _, membership := range memberships {
		if membership.Edges.User != nil {
			members = append(members, toAPIUser(membership.Edges.User))
		}
	}
	grantList := make([]api.Grant, 0, len(grants))
	for _, grant := range grants {
		grantList = append(grantList, toAPIGrant(grant))
	}

	c.JSON(http.StatusOK, api.GroupDetail{
		Id:          item.ID,
		Name:        item.Name,
		Description: item.Description,
		CreatedAt:   item.CreatedAt,
		UpdatedAt:   item.UpdatedAt,
		Members:     members,
		Grants:      grantList,
	})
}

// UpdateGroup changes a group's name or description.
func (h *Handler) UpdateGroup(c *gin.Context, id api.Id, _ api.UpdateGroupParams) {
	var req api.UpdateGroupRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.Name == nil && req.Description == nil {
		writeFields(c, map[string]string{"body": "at least one field is required"})
		return
	}
	update := h.client.Group.UpdateOneID(id)
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if err := validateGroupName(name); err != nil {
			writeFields(c, map[string]string{"name": err.Error()})
			return
		}
		update.SetName(name)
	}
	if req.Description != nil {
		update.SetDescription(strings.TrimSpace(*req.Description))
	}
	updated, err := update.Save(c.Request.Context())
	if err != nil {
		h.fail(c, err, "update group")
		return
	}
	c.JSON(http.StatusOK, toAPIGroup(updated))
}

// DeleteGroup deletes a group, its memberships and its grants.
func (h *Handler) DeleteGroup(c *gin.Context, id api.Id, _ api.DeleteGroupParams) {
	ctx := c.Request.Context()
	if _, err := h.client.Group.Get(ctx, id); err != nil {
		h.fail(c, err, "get group for delete")
		return
	}

	tx, err := h.client.Tx(ctx)
	if err != nil {
		h.fail(c, err, "begin group delete")
		return
	}
	if _, err := tx.DatabaseGrant.Delete().Where(databasegrant.GroupID(id)).Exec(ctx); err != nil {
		_ = tx.Rollback()
		h.fail(c, err, "delete group grants")
		return
	}
	if _, err := tx.GroupMembership.Delete().Where(groupmembership.GroupID(id)).Exec(ctx); err != nil {
		_ = tx.Rollback()
		h.fail(c, err, "delete group memberships")
		return
	}
	if err := tx.Group.DeleteOneID(id).Exec(ctx); err != nil {
		_ = tx.Rollback()
		h.fail(c, err, "delete group")
		return
	}
	if err := tx.Commit(); err != nil {
		h.fail(c, err, "commit group delete")
		return
	}
	c.Status(http.StatusNoContent)
}

// AddGroupMember adds a user to a group idempotently.
func (h *Handler) AddGroupMember(c *gin.Context, id api.Id, userId api.UserId, _ api.AddGroupMemberParams) {
	ctx := c.Request.Context()
	if _, err := h.client.Group.Get(ctx, id); err != nil {
		h.fail(c, err, "get group for membership")
		return
	}
	if _, err := h.client.PlatformUser.Get(ctx, userId); err != nil {
		h.fail(c, err, "get user for membership")
		return
	}
	exists, err := h.client.GroupMembership.Query().
		Where(groupmembership.GroupID(id), groupmembership.UserID(userId)).
		Exist(ctx)
	if err != nil {
		h.fail(c, err, "check membership")
		return
	}
	if !exists {
		_, err = h.client.GroupMembership.Create().
			SetGroupID(id).
			SetUserID(userId).
			Save(ctx)
		if err != nil && !ent.IsConstraintError(err) {
			h.fail(c, err, "create membership")
			return
		}
	}
	c.Status(http.StatusNoContent)
}

// RemoveGroupMember removes a user from a group idempotently.
func (h *Handler) RemoveGroupMember(c *gin.Context, id api.Id, userId api.UserId, _ api.RemoveGroupMemberParams) {
	ctx := c.Request.Context()
	if _, err := h.client.Group.Get(ctx, id); err != nil {
		h.fail(c, err, "get group for membership removal")
		return
	}
	if _, err := h.client.PlatformUser.Get(ctx, userId); err != nil {
		h.fail(c, err, "get user for membership removal")
		return
	}
	if _, err := h.client.GroupMembership.Delete().
		Where(groupmembership.GroupID(id), groupmembership.UserID(userId)).
		Exec(ctx); err != nil {
		h.fail(c, err, "delete membership")
		return
	}
	c.Status(http.StatusNoContent)
}

func toAPIGroup(item *ent.Group) api.Group {
	return api.Group{
		Id:          item.ID,
		Name:        item.Name,
		Description: item.Description,
		CreatedAt:   item.CreatedAt,
		UpdatedAt:   item.UpdatedAt,
	}
}

func validateGroupName(name string) error {
	length := utf8.RuneCountInString(name)
	if length < 2 || length > 64 {
		return fmt.Errorf("name must be 2-64 characters")
	}
	return nil
}

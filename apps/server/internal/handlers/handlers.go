package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"Orpbal/internal/database"
	"Orpbal/internal/models"
)

// GetUsers returns all users
func GetUsers(c *gin.Context) {
	rows, err := database.GetDB().Query("SELECT id, name, email, created_at, updated_at FROM users ORDER BY id")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var users []models.UserCreate
	for rows.Next() {
		var id int
		var name, email string
		var createdAt, updatedAt string
		if err := rows.Scan(&id, &name, &email, &createdAt, &updatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		users = append(users, models.UserCreate{Name: name, Email: email})
	}

	c.JSON(http.StatusOK, users)
}

// GetUser returns a user by ID
func GetUser(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	var name, email string
	err = database.GetDB().QueryRow("SELECT name, email FROM users WHERE id = ?", id).Scan(&name, &email)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"id": id, "name": name, "email": email})
}

// CreateUser creates a new user
func CreateUser(c *gin.Context) {
	var input models.UserCreate
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := database.GetDB().Exec("INSERT INTO users (name, email) VALUES (?, ?)", input.Name, input.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	id, _ := result.LastInsertId()
	c.JSON(http.StatusCreated, gin.H{"id": id, "name": input.Name, "email": input.Email})
}

// UpdateUser updates a user
func UpdateUser(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	var input models.UserUpdate
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if input.Name != nil {
		database.GetDB().Exec("UPDATE users SET name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", *input.Name, id)
	}
	if input.Email != nil {
		database.GetDB().Exec("UPDATE users SET email = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", *input.Email, id)
	}

	c.JSON(http.StatusOK, gin.H{"id": id, "message": "User updated"})
}

// DeleteUser deletes a user
func DeleteUser(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	_, err = database.GetDB().Exec("DELETE FROM users WHERE id = ?", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}

// GetPosts returns all posts
func GetPosts(c *gin.Context) {
	rows, err := database.GetDB().Query("SELECT id, title, content, published, author_id, created_at, updated_at FROM posts ORDER BY id")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var posts []gin.H
	for rows.Next() {
		var id, authorID int
		var title, content string
		var published bool
		var createdAt, updatedAt string
		if err := rows.Scan(&id, &title, &content, &published, &authorID, &createdAt, &updatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		posts = append(posts, gin.H{"id": id, "title": title, "content": content, "published": published, "author_id": authorID})
	}

	if posts == nil {
		posts = []gin.H{}
	}
	c.JSON(http.StatusOK, posts)
}

// CreatePost creates a new post
func CreatePost(c *gin.Context) {
	var input models.PostCreate
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := database.GetDB().Exec("INSERT INTO posts (title, content, author_id) VALUES (?, ?, ?)", input.Title, input.Content, input.AuthorID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	id, _ := result.LastInsertId()
	c.JSON(http.StatusCreated, gin.H{"id": id, "title": input.Title, "content": input.Content, "author_id": input.AuthorID})
}

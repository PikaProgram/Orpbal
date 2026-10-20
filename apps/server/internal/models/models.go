package models

// UserCreate is used for creating a new user
type UserCreate struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// UserUpdate is used for updating a user
type UserUpdate struct {
	Name  *string `json:"name,omitempty"`
	Email *string `json:"email,omitempty"`
}

// PostCreate is used for creating a new post
type PostCreate struct {
	Title    string `json:"title"`
	Content  string `json:"content"`
	AuthorID int    `json:"author_id"`
}

// PostUpdate is used for updating a post
type PostUpdate struct {
	Title     *string `json:"title,omitempty"`
	Content   *string `json:"content,omitempty"`
	Published *bool   `json:"published,omitempty"`
}

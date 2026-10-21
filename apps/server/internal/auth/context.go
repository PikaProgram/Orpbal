package auth

import (
	"Orpbal/ent"

	"github.com/gin-gonic/gin"
)

const (
	userKey    = "orpbal.auth.user"
	sessionKey = "orpbal.auth.session"
	csrfKey    = "orpbal.auth.csrf"
)

// CurrentUser returns the authenticated user, if any.
func CurrentUser(c *gin.Context) *ent.PlatformUser {
	value, ok := c.Get(userKey)
	if !ok {
		return nil
	}
	user, _ := value.(*ent.PlatformUser)
	return user
}

// CurrentSession returns the authenticated session row, if any.
func CurrentSession(c *gin.Context) *ent.Session {
	value, ok := c.Get(sessionKey)
	if !ok {
		return nil
	}
	session, _ := value.(*ent.Session)
	return session
}

// CurrentCSRF returns the session's CSRF token.
func CurrentCSRF(c *gin.Context) string {
	value, ok := c.Get(csrfKey)
	if !ok {
		return ""
	}
	token, _ := value.(string)
	return token
}

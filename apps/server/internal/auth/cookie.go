package auth

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// SetSessionCookie writes the opaque session cookie.
func SetSessionCookie(c *gin.Context, rawToken string, absoluteExpires time.Time, secure bool) {
	maxAge := int(time.Until(absoluteExpires).Seconds())
	if maxAge <= 0 {
		ClearSessionCookie(c, secure)
		return
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(CookieName, rawToken, maxAge, "/", "", secure, true)
}

// ClearSessionCookie expires the session cookie.
func ClearSessionCookie(c *gin.Context, secure bool) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(CookieName, "", -1, "/", "", secure, true)
}

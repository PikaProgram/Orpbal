package auth

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"Orpbal/internal/api"
)

// Authenticate loads the session cookie, enforces idle/absolute expiry,
// rotates when due, and validates CSRF on unsafe methods.
func Authenticate(service *Service, secure bool, appOrigin string, log zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if isUnsafe(c.Request.Method) && !originAllowed(c, appOrigin) {
			abortWithError(c, http.StatusForbidden, "forbidden", "origin is not allowed")
			return
		}

		path := c.Request.URL.Path
		if path == "/api/auth/login" || path == "/api/health" {
			c.Next()
			return
		}

		rawToken, err := c.Cookie(CookieName)
		if err != nil || rawToken == "" {
			ClearSessionCookie(c, secure)
			abortWithError(c, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}

		session, err := service.Authenticate(
			c.Request.Context(),
			rawToken,
			path != "/api/auth/password" && path != "/api/auth/logout",
		)
		if err != nil {
			if !errors.Is(err, ErrInvalidSession) {
				log.Error().Err(err).Str("path", path).Msg("Session authentication failed")
			}
			ClearSessionCookie(c, secure)
			abortWithError(c, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}

		if session.RawToken != "" {
			SetSessionCookie(c, session.RawToken, session.Row.AbsoluteExpiresAt, secure)
		}

		c.Set(userKey, session.User)
		c.Set(sessionKey, session.Row)
		c.Set(csrfKey, session.CSRFToken)

		if isUnsafe(c.Request.Method) && !csrfValid(c, session.CSRFToken) {
			abortWithError(c, http.StatusForbidden, "forbidden", "invalid CSRF token")
			return
		}

		c.Next()
	}
}

// RequirePlatformAdmin rejects non-admin access to /api/admin/*.
func RequirePlatformAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/admin/") {
			user := CurrentUser(c)
			if user == nil || !user.IsPlatformAdmin {
				abortWithError(c, http.StatusForbidden, "forbidden", "platform admin required")
				return
			}
		}
		c.Next()
	}
}

func isUnsafe(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

func originAllowed(c *gin.Context, appOrigin string) bool {
	if appOrigin == "" {
		return true
	}
	origin := c.GetHeader("Origin")
	if origin == "" {
		if referer := c.GetHeader("Referer"); referer != "" {
			if parsed, err := url.Parse(referer); err == nil {
				origin = parsed.Scheme + "://" + parsed.Host
			}
		}
	}
	if origin == "" {
		// Server-to-server calls (Astro actions) have no Origin header.
		return true
	}
	return strings.EqualFold(strings.TrimRight(origin, "/"), appOrigin)
}

func csrfValid(c *gin.Context, expected string) bool {
	provided := c.GetHeader("X-CSRF-Token")
	if provided == "" && isFormRequest(c) {
		provided = c.PostForm("csrf_token")
	}
	if provided == "" || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func isFormRequest(c *gin.Context) bool {
	contentType := c.ContentType()
	return contentType == "application/x-www-form-urlencoded" || contentType == "multipart/form-data"
}

func abortWithError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, api.ErrorResponse{
		Error: api.ErrorDetail{Code: code, Message: message},
	})
}

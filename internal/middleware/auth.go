// Package middleware provides HTTP middleware for Firebase auth and admin checks.
package middleware

import (
	"context"
	"net/http"
	"strings"

	"firebase.google.com/go/v4/auth"
	"github.com/andrewmartin/fantasy-league/internal/db"
	"github.com/andrewmartin/fantasy-league/internal/models"
)

type contextKey string

const (
	CtxUID         contextKey = "uid"
	CtxEmail       contextKey = "email"
	CtxDisplayName contextKey = "displayName"
	CtxPhotoURL    contextKey = "photoURL"
	CtxIsAdmin     contextKey = "isAdmin"
)

// EmailFromContext returns the authenticated user's email.
func EmailFromContext(ctx context.Context) string {
	v, _ := ctx.Value(CtxEmail).(string)
	return v
}

// DisplayNameFromContext returns the authenticated user's display name.
func DisplayNameFromContext(ctx context.Context) string {
	v, _ := ctx.Value(CtxDisplayName).(string)
	return v
}

// PhotoURLFromContext returns the authenticated user's photo URL.
func PhotoURLFromContext(ctx context.Context) string {
	v, _ := ctx.Value(CtxPhotoURL).(string)
	return v
}

// UIDFromContext returns the authenticated user's UID from the request context.
func UIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(CtxUID).(string)
	return v
}

// IsAdminFromContext returns whether the authenticated user is an admin.
func IsAdminFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(CtxIsAdmin).(bool)
	return v
}

// Authenticate verifies the Firebase ID token in the Authorization header.
// On success it sets CtxUID and CtxEmail in the request context.
// On failure it responds 401.
func Authenticate(authClient *auth.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				writeUnauthorized(w, "missing or malformed Authorization header")
				return
			}

			decoded, err := authClient.VerifyIDToken(r.Context(), token)
			if err != nil {
				writeUnauthorized(w, "invalid token")
				return
			}

			ctx := context.WithValue(r.Context(), CtxUID, decoded.UID)
			ctx = context.WithValue(ctx, CtxEmail, claimStr(decoded.Claims, "email"))
			ctx = context.WithValue(ctx, CtxDisplayName, claimStr(decoded.Claims, "name"))
			ctx = context.WithValue(ctx, CtxPhotoURL, claimStr(decoded.Claims, "picture"))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAdmin checks the Firestore users/{uid}.isAdmin flag.
// Must be used after Authenticate. Responds 403 if not admin.
func RequireAdmin(dbClient *db.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uid := UIDFromContext(r.Context())
			if uid == "" {
				writeUnauthorized(w, "not authenticated")
				return
			}

			var user models.User
			found, err := dbClient.GetDoc(r.Context(), "users", uid, &user)
			if err != nil || !found || !user.IsAdmin {
				writeForbidden(w, "admin access required")
				return
			}

			ctx := context.WithValue(r.Context(), CtxIsAdmin, true)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func claimStr(claims map[string]interface{}, key string) string {
	v, _ := claims[key].(string)
	return v
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return "", false
	}
	t := strings.TrimPrefix(h, "Bearer ")
	if t == "" {
		return "", false
	}
	return t, true
}

func writeUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"` + msg + `"}`))
}

func writeForbidden(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	w.Write([]byte(`{"error":"` + msg + `"}`))
}

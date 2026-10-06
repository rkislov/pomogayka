package middleware

import (
	"context"
	"net/http"

	"github.com/alexedwards/scs/v2"

	"github.com/rkislov/pomogayka/internal/db"
	"github.com/rkislov/pomogayka/internal/models"
)

type ctxKey string

const UserKey ctxKey = "user"

func UserFromContext(ctx context.Context) *models.User {
	u, _ := ctx.Value(UserKey).(*models.User)
	return u
}

func LoadUser(sm *scs.SessionManager, store *db.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := sm.GetString(r.Context(), "user_id")
			if id != "" {
				if u, err := store.GetUserByID(id); err == nil && u.IsActive {
					r = r.WithContext(context.WithValue(r.Context(), UserKey, u))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserFromContext(r.Context()) == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func RequireStaff(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r.Context())
		if u == nil || !u.Role.IsStaff() {
			http.Error(w, "Недостаточно прав", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r.Context())
		if u == nil || u.Role != models.RoleAdmin {
			http.Error(w, "Недостаточно прав", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

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
const TenantKey ctxKey = "tenant"

func UserFromContext(ctx context.Context) *models.User {
	u, _ := ctx.Value(UserKey).(*models.User)
	return u
}

func TenantFromContext(ctx context.Context) *models.Tenant {
	t, _ := ctx.Value(TenantKey).(*models.Tenant)
	return t
}

func LoadUser(sm *scs.SessionManager, store *db.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := sm.GetString(r.Context(), "user_id")
			if id != "" {
				if u, err := store.GetUserByID(id); err == nil && u.IsActive {
					ctx := context.WithValue(r.Context(), UserKey, u)
					if t, err := store.GetTenant(u.TenantID); err == nil {
						ctx = context.WithValue(ctx, TenantKey, t)
					}
					r = r.WithContext(ctx)
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

func RequireManager(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r.Context())
		if u == nil || !u.Role.IsManager() {
			http.Error(w, "Недостаточно прав", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r.Context())
		if u == nil || !u.Role.IsAdmin() {
			http.Error(w, "Недостаточно прав", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

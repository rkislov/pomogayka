package middleware

import (
	"context"
	"net/http"
	"strings"

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

// ResolveTenant maps Host to a tenant portal domain.
// If the host is not registered, falls back to the default tenant (local/dev).
func ResolveTenant(store *db.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := r.Host
			if xf := r.Header.Get("X-Forwarded-Host"); xf != "" {
				host = strings.TrimSpace(strings.Split(xf, ",")[0])
			}
			var tenant *models.Tenant
			if t, err := store.GetTenantByHost(host); err == nil && t.IsActive {
				tenant = t
			} else if t, err := store.GetTenantBySlug("default"); err == nil && t.IsActive {
				tenant = t
			}
			if tenant != nil {
				r = r.WithContext(context.WithValue(r.Context(), TenantKey, tenant))
			}
			next.ServeHTTP(w, r)
		})
	}
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

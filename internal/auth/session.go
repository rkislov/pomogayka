package auth

import (
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"

	"github.com/rkislov/pomogayka/internal/config"
)

func NewSessionManager(cfg config.Config) *scs.SessionManager {
	sm := scs.New()
	sm.Lifetime = time.Duration(cfg.SessionHours) * time.Hour
	sm.Cookie.Name = "pomogayka_session"
	sm.Cookie.HttpOnly = true
	sm.Cookie.SameSite = http.SameSiteLaxMode
	sm.Cookie.Secure = cfg.SecureCookies
	sm.Cookie.Path = "/"
	return sm
}

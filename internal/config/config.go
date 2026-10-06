package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	AppName            string
	Addr               string
	DatabaseURL        string
	SessionSecret      string
	AdminEmail         string
	AdminPassword      string
	AdminFullName      string
	SecureCookies      bool
	SessionHours       int
	TelegramBotToken   string
	TelegramBotEnabled bool
	JabberJID          string
	JabberPassword     string
	JabberHost         string
	JabberNoTLS        bool
	JabberStartTLS     bool
	JabberInsecureTLS  bool
	JabberEnabled      bool
}

func Load() Config {
	_ = godotenv.Load()

	token := env("TELEGRAM_BOT_TOKEN", "")
	jabberJID := env("JABBER_JID", "")
	jabberPassword := env("JABBER_PASSWORD", "")
	jabberConfigured := jabberJID != "" && jabberPassword != ""

	return Config{
		AppName:            env("APP_NAME", "Помогайка"),
		Addr:               env("ADDR", ":8080"),
		DatabaseURL:        env("DATABASE_URL", "file:data/pomogayka.db?_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"),
		SessionSecret:      env("SESSION_SECRET", "change-me-in-production"),
		AdminEmail:         env("ADMIN_EMAIL", "admin@example.com"),
		AdminPassword:      env("ADMIN_PASSWORD", "admin12345"),
		AdminFullName:      env("ADMIN_FULL_NAME", "Администратор"),
		SecureCookies:      envBool("SECURE_COOKIES", false),
		SessionHours:       envInt("SESSION_HOURS", 168),
		TelegramBotToken:   token,
		TelegramBotEnabled: envBool("TELEGRAM_BOT_ENABLED", token != ""),
		JabberJID:          jabberJID,
		JabberPassword:     jabberPassword,
		JabberHost:         env("JABBER_HOST", ""),
		JabberNoTLS:        envBool("JABBER_NO_TLS", false),
		JabberStartTLS:     envBool("JABBER_START_TLS", true),
		JabberInsecureTLS:  envBool("JABBER_INSECURE_TLS", false),
		JabberEnabled:      envBool("JABBER_ENABLED", jabberConfigured),
	}
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

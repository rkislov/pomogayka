package main

import (
	"io/fs"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	pomogayka "github.com/rkislov/pomogayka"
	"github.com/rkislov/pomogayka/internal/auth"
	"github.com/rkislov/pomogayka/internal/bot"
	"github.com/rkislov/pomogayka/internal/config"
	"github.com/rkislov/pomogayka/internal/db"
	"github.com/rkislov/pomogayka/internal/handlers"
	appmw "github.com/rkislov/pomogayka/internal/middleware"
)

var version = "dev"

func main() {
	cfg := config.Load()

	dialect, err := db.DetectDialect(cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	migrationPath := "migrations/sqlite/001_init.sql"
	if dialect == db.DialectPostgres {
		migrationPath = "migrations/postgres/001_init.sql"
	}
	migration, err := pomogayka.Content.ReadFile(migrationPath)
	if err != nil {
		log.Fatal(err)
	}
	db.MigrationSQL = string(migration)

	database, err := db.Open(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()
	log.Printf("database dialect: %s", dialect)

	store := db.NewStore(database)
	sessions := auth.NewSessionManager(cfg)

	templatesFS, err := fs.Sub(pomogayka.Content, "web/templates")
	if err != nil {
		log.Fatal(err)
	}
	renderer, err := handlers.NewRenderer(templatesFS, cfg.AppName)
	if err != nil {
		log.Fatal(err)
	}

	app := &handlers.App{Store: store, Sessions: sessions, Render: renderer}
	notifiers := &bot.MultiNotifier{}

	if cfg.TelegramBotEnabled {
		if cfg.TelegramBotToken == "" {
			log.Fatal("TELEGRAM_BOT_ENABLED=true, but TELEGRAM_BOT_TOKEN is empty")
		}
		tg, err := bot.NewTelegram(cfg.TelegramBotToken, store, cfg.AppName)
		if err != nil {
			log.Fatalf("telegram bot: %v", err)
		}
		notifiers.Add(tg)
		go tg.Start()
		log.Printf("Telegram bot enabled")
	} else {
		log.Printf("Telegram bot disabled (set TELEGRAM_BOT_TOKEN to enable)")
	}

	if cfg.JabberEnabled {
		if cfg.JabberJID == "" || cfg.JabberPassword == "" {
			log.Fatal("JABBER_ENABLED=true, but JABBER_JID/JABBER_PASSWORD are empty")
		}
		jb, err := bot.NewJabber(cfg, store, cfg.AppName)
		if err != nil {
			log.Fatalf("jabber bot: %v", err)
		}
		notifiers.Add(jb)
		go jb.Start()
		log.Printf("Jabber bot enabled as %s", cfg.JabberJID)
	} else {
		log.Printf("Jabber bot disabled (set JABBER_JID and JABBER_PASSWORD to enable)")
	}

	if len(notifiers.Notifiers) > 0 {
		app.Notifier = notifiers
	}

	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.RealIP, chimw.Logger, chimw.Recoverer)
	r.Use(sessions.LoadAndSave)
	r.Use(appmw.LoadUser(sessions, store))

	staticFS, err := fs.Sub(pomogayka.Content, "web/static")
	if err != nil {
		log.Fatal(err)
	}
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	r.Get("/", app.Home)
	r.Get("/login", app.LoginForm)
	r.Post("/login", app.Login)
	r.Get("/register", app.RegisterForm)
	r.Post("/register", app.Register)
	r.Post("/logout", app.Logout)

	r.Group(func(private chi.Router) {
		private.Use(appmw.RequireAuth)
		private.Get("/dashboard", app.Dashboard)
		private.Get("/tickets", app.TicketsList)
		private.Get("/tickets/new", app.TicketNewForm)
		private.Post("/tickets", app.TicketCreate)
		private.Get("/tickets/{id}", app.TicketView)
		private.Post("/tickets/{id}/comments", app.TicketComment)
		private.Post("/tickets/{id}/close", app.TicketClose)

		private.Group(func(staff chi.Router) {
			staff.Use(appmw.RequireStaff)
			staff.Post("/tickets/{id}/take", app.TicketTake)
			staff.Post("/tickets/{id}/await", app.TicketAwait)
		})

		private.Group(func(mgr chi.Router) {
			mgr.Use(appmw.RequireManager)
			mgr.Get("/team", app.Team)
			mgr.Post("/tickets/{id}/assign", app.TicketAssign)
		})

		private.Group(func(admin chi.Router) {
			admin.Use(appmw.RequireAdmin)
			admin.Get("/admin", app.Admin)
			admin.Post("/admin/users/{id}/role", app.AdminUserRole)
			admin.Post("/admin/users/{id}/manager", app.AdminUserManager)
			admin.Post("/admin/queues", app.AdminQueueCreate)
			admin.Post("/admin/templates/{id}", app.AdminTemplateUpdate)
			admin.Post("/admin/tenants", app.AdminTenantCreate)
		})
	})

	log.Printf("%s %s listening on %s", cfg.AppName, version, cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, r); err != nil {
		log.Fatal(err)
	}
}

package handlers

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/rkislov/pomogayka/internal/middleware"
	"github.com/rkislov/pomogayka/internal/models"
)

type Renderer struct {
	templates *template.Template
	appName   string
}

func NewRenderer(fsys fs.FS, appName string) (*Renderer, error) {
	funcMap := template.FuncMap{
		"formatTime": func(t time.Time) string {
			if t.IsZero() {
				return "—"
			}
			return t.Local().Format("02.01.2006 15:04")
		},
		"statusLabel":   func(s models.TicketStatus) string { return s.Label() },
		"priorityLabel": func(p models.Priority) string { return p.Label() },
		"roleLabel":     func(r models.Role) string { return r.Label() },
		"statusClass": func(s models.TicketStatus) string {
			switch s {
			case models.StatusNew:
				return "badge badge-blue"
			case models.StatusInProgress:
				return "badge badge-amber"
			case models.StatusAwaitingRequester:
				return "badge badge-indigo"
			case models.StatusClosed:
				return "badge badge-green"
			default:
				return "badge badge-slate"
			}
		},
		"priorityClass": func(p models.Priority) string {
			switch p {
			case models.PriorityCritical:
				return "badge badge-red"
			case models.PriorityHigh:
				return "badge badge-orange"
			case models.PriorityMedium:
				return "badge badge-amber"
			default:
				return "badge badge-slate"
			}
		},
		"nl2br": func(s string) template.HTML {
			escaped := template.HTMLEscapeString(s)
			return template.HTML(strings.ReplaceAll(escaped, "\n", "<br>"))
		},
		"hasPrefix": strings.HasPrefix,
		"eq":        func(a, b any) bool { return fmt.Sprint(a) == fmt.Sprint(b) },
	}
	tmpl := template.New("").Funcs(funcMap)
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".html") {
			return err
		}
		b, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(path)
		_, err = tmpl.New(name).Parse(string(b))
		return err
	})
	if err != nil {
		return nil, err
	}
	return &Renderer{templates: tmpl, appName: appName}, nil
}

type pageData map[string]any

func (r *Renderer) Render(w http.ResponseWriter, req *http.Request, name string, data pageData) {
	if data == nil {
		data = pageData{}
	}
	data["AppName"] = r.appName
	data["User"] = middleware.UserFromContext(req.Context())
	data["Path"] = req.URL.Path
	var buf bytes.Buffer
	if err := r.templates.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, "Ошибка шаблона: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

func (r *Renderer) RenderPartial(w http.ResponseWriter, name string, data pageData) {
	var buf bytes.Buffer
	if err := r.templates.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, "Ошибка шаблона: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

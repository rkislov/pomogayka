package handlers

import (
	"net/http"
	"strings"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/rkislov/pomogayka/internal/auth"
	"github.com/rkislov/pomogayka/internal/db"
	"github.com/rkislov/pomogayka/internal/middleware"
	"github.com/rkislov/pomogayka/internal/models"
)

type TicketNotifier interface {
	NotifyTicketAuthor(ticket *models.Ticket, text string)
}

type App struct {
	Store     *db.Store
	Sessions  *scs.SessionManager
	Render    *Renderer
	Notifier  TicketNotifier
}

func (a *App) Home(w http.ResponseWriter, r *http.Request) {
	if u := middleware.UserFromContext(r.Context()); u != nil {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *App) LoginForm(w http.ResponseWriter, r *http.Request) {
	a.Render.Render(w, r, "pages/login.html", pageData{"Title": "Вход", "Error": ""})
}

func (a *App) Login(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	u, err := a.Store.GetUserByEmail(email)
	if err != nil || !auth.CheckPassword(u.PasswordHash, password) || !u.IsActive {
		a.Render.Render(w, r, "pages/login.html", pageData{"Title": "Вход", "Error": "Неверный email или пароль", "Email": email})
		return
	}
	if err := a.Sessions.RenewToken(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.Sessions.Put(r.Context(), "user_id", u.ID)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (a *App) RegisterForm(w http.ResponseWriter, r *http.Request) {
	a.Render.Render(w, r, "pages/register.html", pageData{"Title": "Регистрация", "Error": ""})
}

func (a *App) Register(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	email := strings.TrimSpace(r.FormValue("email"))
	fullName := strings.TrimSpace(r.FormValue("full_name"))
	password := r.FormValue("password")
	if len(email) < 3 || len(fullName) < 2 || len(password) < 8 {
		a.Render.Render(w, r, "pages/register.html", pageData{
			"Title": "Регистрация", "Error": "Проверьте поля: имя, email и пароль от 8 символов",
			"Email": email, "FullName": fullName,
		})
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	u, err := a.Store.CreateUser(email, fullName, hash, models.RoleClient)
	if err != nil {
		a.Render.Render(w, r, "pages/register.html", pageData{
			"Title": "Регистрация", "Error": "Не удалось создать пользователя (возможно, email уже занят)",
			"Email": email, "FullName": fullName,
		})
		return
	}
	_ = a.Sessions.RenewToken(r.Context())
	a.Sessions.Put(r.Context(), "user_id", u.ID)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (a *App) Logout(w http.ResponseWriter, r *http.Request) {
	_ = a.Sessions.Destroy(r.Context())
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *App) Dashboard(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	stats, _ := a.Store.DashboardStats(user)
	tickets, _ := a.Store.ListTickets(db.TicketFilter{User: user})
	if len(tickets) > 8 {
		tickets = tickets[:8]
	}
	a.Render.Render(w, r, "pages/dashboard.html", pageData{
		"Title": "Дашборд", "Stats": stats, "Tickets": tickets,
	})
}

func (a *App) TicketsList(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	status := r.URL.Query().Get("status")
	q := r.URL.Query().Get("q")
	onlyMine := r.URL.Query().Get("mine") == "1"
	tickets, err := a.Store.ListTickets(db.TicketFilter{User: user, Status: status, Query: q, OnlyMine: onlyMine})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.Render.Render(w, r, "pages/tickets_list.html", pageData{
		"Title": "Заявки", "Tickets": tickets, "Status": status, "Query": q, "Mine": onlyMine,
	})
}

func (a *App) TicketNewForm(w http.ResponseWriter, r *http.Request) {
	queues, _ := a.Store.ListQueues(true)
	a.Render.Render(w, r, "pages/ticket_new.html", pageData{"Title": "Новая заявка", "Queues": queues, "Error": ""})
}

func (a *App) TicketCreate(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	_ = r.ParseForm()
	title := strings.TrimSpace(r.FormValue("title"))
	description := strings.TrimSpace(r.FormValue("description"))
	priority := models.Priority(r.FormValue("priority"))
	if priority == "" {
		priority = models.PriorityMedium
	}
	queueID := strings.TrimSpace(r.FormValue("queue_id"))
	var qid *string
	if queueID != "" {
		qid = &queueID
	}
	queues, _ := a.Store.ListQueues(true)
	if len(title) < 3 || len(description) < 3 {
		a.Render.Render(w, r, "pages/ticket_new.html", pageData{
			"Title": "Новая заявка", "Queues": queues, "Error": "Заполните тему и описание (минимум 3 символа)",
			"FormTitle": title, "FormDescription": description, "Priority": priority, "QueueID": queueID,
		})
		return
	}
	t, err := a.Store.CreateTicket(title, description, priority, user.ID, qid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/tickets/"+t.ID, http.StatusSeeOther)
}

func (a *App) TicketView(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	id := chi.URLParam(r, "id")
	ticket, err := a.Store.GetTicket(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !user.Role.IsStaff() && ticket.AuthorID != user.ID {
		http.Error(w, "Недостаточно прав", http.StatusForbidden)
		return
	}
	comments, _ := a.Store.ListComments(id, user.Role.IsStaff())
	a.Render.Render(w, r, "pages/ticket_view.html", pageData{
		"Title": "Заявка #" + ticket.Number, "Ticket": ticket, "Comments": comments,
	})
}

func (a *App) TicketComment(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	id := chi.URLParam(r, "id")
	ticket, err := a.Store.GetTicket(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !user.Role.IsStaff() && ticket.AuthorID != user.ID {
		http.Error(w, "Недостаточно прав", http.StatusForbidden)
		return
	}
	_ = r.ParseForm()
	body := strings.TrimSpace(r.FormValue("body"))
	internal := user.Role.IsStaff() && r.FormValue("is_internal") == "1"
	if body == "" {
		http.Error(w, "Пустой комментарий", http.StatusBadRequest)
		return
	}
	if _, err := a.Store.AddComment(id, user.ID, body, internal); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !internal && a.Notifier != nil && user.ID != ticket.AuthorID {
		a.Notifier.NotifyTicketAuthor(ticket, "Новый ответ по заявке #"+ticket.Number+"\n\n"+body)
	}
	comments, _ := a.Store.ListComments(id, user.Role.IsStaff())
	if r.Header.Get("HX-Request") == "true" {
		a.Render.RenderPartial(w, "partials/comments.html", pageData{"Comments": comments, "User": user, "Ticket": ticket})
		return
	}
	http.Redirect(w, r, "/tickets/"+id, http.StatusSeeOther)
}

func (a *App) TicketTake(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if err := a.Store.AssignTicket(id, user.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/tickets/"+id, http.StatusSeeOther)
}

func (a *App) TicketClose(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	id := chi.URLParam(r, "id")
	ticket, err := a.Store.GetTicket(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !user.Role.IsStaff() && ticket.AuthorID != user.ID {
		http.Error(w, "Недостаточно прав", http.StatusForbidden)
		return
	}
	assignee := user.ID
	if err := a.Store.UpdateTicketStatus(id, models.StatusClosed, &assignee); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/tickets/"+id, http.StatusSeeOther)
}

func (a *App) TicketAwait(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.UpdateTicketStatus(id, models.StatusAwaitingRequester, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/tickets/"+id, http.StatusSeeOther)
}

func (a *App) Admin(w http.ResponseWriter, r *http.Request) {
	users, _ := a.Store.ListUsers()
	queues, _ := a.Store.ListQueues(false)
	templates, _ := a.Store.ListTemplates()
	a.Render.Render(w, r, "pages/admin.html", pageData{
		"Title": "Админка", "Users": users, "Queues": queues, "Templates": templates,
		"Flash": a.Sessions.PopString(r.Context(), "flash"),
	})
}

func (a *App) AdminUserRole(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id := chi.URLParam(r, "id")
	role := models.Role(r.FormValue("role"))
	if role != models.RoleClient && role != models.RoleAgent && role != models.RoleAdmin {
		http.Error(w, "Некорректная роль", http.StatusBadRequest)
		return
	}
	if err := a.Store.UpdateUserRole(id, role); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.Sessions.Put(r.Context(), "flash", "Роль пользователя обновлена")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (a *App) AdminQueueCreate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	name := strings.TrimSpace(r.FormValue("name"))
	desc := strings.TrimSpace(r.FormValue("description"))
	if name == "" {
		http.Error(w, "Укажите название очереди", http.StatusBadRequest)
		return
	}
	if err := a.Store.CreateQueue(name, desc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.Sessions.Put(r.Context(), "flash", "Очередь создана")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (a *App) AdminTemplateUpdate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id := chi.URLParam(r, "id")
	subject := strings.TrimSpace(r.FormValue("subject"))
	body := strings.TrimSpace(r.FormValue("body"))
	active := r.FormValue("is_active") == "1"
	if err := a.Store.UpdateTemplate(id, subject, body, active); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.Sessions.Put(r.Context(), "flash", "Шаблон сохранён")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

package handlers

import (
	"net/http"
	"regexp"
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
	Store    *db.Store
	Sessions *scs.SessionManager
	Render   *Renderer
	Notifier TicketNotifier
}

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)

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
	a.Render.Render(w, r, "pages/register.html", pageData{"Title": "Регистрация", "Error": "", "TenantSlug": "default"})
}

func (a *App) Register(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	email := strings.TrimSpace(r.FormValue("email"))
	fullName := strings.TrimSpace(r.FormValue("full_name"))
	password := r.FormValue("password")
	slug := strings.ToLower(strings.TrimSpace(r.FormValue("tenant_slug")))
	if slug == "" {
		slug = "default"
	}
	tenant, err := a.Store.GetTenantBySlug(slug)
	if err != nil || !tenant.IsActive {
		a.Render.Render(w, r, "pages/register.html", pageData{
			"Title": "Регистрация", "Error": "Организация не найдена. Укажите корректный slug.",
			"Email": email, "FullName": fullName, "TenantSlug": slug,
		})
		return
	}
	if len(email) < 3 || len(fullName) < 2 || len(password) < 8 {
		a.Render.Render(w, r, "pages/register.html", pageData{
			"Title": "Регистрация", "Error": "Проверьте поля: имя, email и пароль от 8 символов",
			"Email": email, "FullName": fullName, "TenantSlug": slug,
		})
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	u, err := a.Store.CreateUser(tenant.ID, email, fullName, hash, models.RoleClient)
	if err != nil {
		a.Render.Render(w, r, "pages/register.html", pageData{
			"Title": "Регистрация", "Error": "Не удалось создать пользователя (возможно, email уже занят)",
			"Email": email, "FullName": fullName, "TenantSlug": slug,
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
	teamOnly := r.URL.Query().Get("team") == "1"
	tickets, err := a.Store.ListTickets(db.TicketFilter{User: user, Status: status, Query: q, OnlyMine: onlyMine, TeamOnly: teamOnly})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.Render.Render(w, r, "pages/tickets_list.html", pageData{
		"Title": "Заявки", "Tickets": tickets, "Status": status, "Query": q, "Mine": onlyMine, "Team": teamOnly,
	})
}

func (a *App) TicketNewForm(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	queues, _ := a.Store.ListQueues(user.TenantID, true)
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
	queues, _ := a.Store.ListQueues(user.TenantID, true)
	if len(title) < 3 || len(description) < 3 {
		a.Render.Render(w, r, "pages/ticket_new.html", pageData{
			"Title": "Новая заявка", "Queues": queues, "Error": "Заполните тему и описание (минимум 3 символа)",
			"FormTitle": title, "FormDescription": description, "Priority": priority, "QueueID": queueID,
		})
		return
	}
	t, err := a.Store.CreateTicket(user.TenantID, title, description, priority, user.ID, qid)
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
	if !models.CanAccessTicket(user, ticket) {
		http.Error(w, "Недостаточно прав", http.StatusForbidden)
		return
	}
	agents := []models.User{}
	if user.Role.IsManager() {
		if user.Role.IsAdmin() {
			agents, _ = a.Store.ListAgents(user.TenantID)
		} else {
			agents, _ = a.Store.ListManagedAgents(user.ID)
		}
	}
	comments, _ := a.Store.ListComments(id, user.Role.IsStaff())
	a.Render.Render(w, r, "pages/ticket_view.html", pageData{
		"Title": "Заявка #" + ticket.Number, "Ticket": ticket, "Comments": comments, "Agents": agents,
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
	if !models.CanAccessTicket(user, ticket) {
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
	ticket, err := a.Store.GetTicket(id)
	if err != nil || !models.CanAccessTicket(user, ticket) {
		http.Error(w, "Недостаточно прав", http.StatusForbidden)
		return
	}
	if err := a.Store.AssignTicket(id, user.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/tickets/"+id, http.StatusSeeOther)
}

func (a *App) TicketAssign(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	id := chi.URLParam(r, "id")
	ticket, err := a.Store.GetTicket(id)
	if err != nil || !models.CanAccessTicket(user, ticket) || !user.Role.IsManager() {
		http.Error(w, "Недостаточно прав", http.StatusForbidden)
		return
	}
	_ = r.ParseForm()
	agentID := strings.TrimSpace(r.FormValue("agent_id"))
	if agentID == "" {
		http.Error(w, "Выберите специалиста", http.StatusBadRequest)
		return
	}
	agent, err := a.Store.GetUserByID(agentID)
	if err != nil || agent.TenantID != user.TenantID || agent.Role != models.RoleAgent {
		http.Error(w, "Специалист не найден", http.StatusBadRequest)
		return
	}
	if user.Role == models.RoleManager && !user.Role.IsAdmin() && !a.Store.IsAgentManagedBy(agentID, user.ID) {
		http.Error(w, "Этот специалист не в вашей команде", http.StatusForbidden)
		return
	}
	if err := a.Store.AssignTicket(id, agentID); err != nil {
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
	if !models.CanAccessTicket(user, ticket) {
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
	user := middleware.UserFromContext(r.Context())
	id := chi.URLParam(r, "id")
	ticket, err := a.Store.GetTicket(id)
	if err != nil || !models.CanAccessTicket(user, ticket) {
		http.Error(w, "Недостаточно прав", http.StatusForbidden)
		return
	}
	if err := a.Store.UpdateTicketStatus(id, models.StatusAwaitingRequester, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/tickets/"+id, http.StatusSeeOther)
}

func (a *App) Team(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	var agents []models.User
	if user.Role.IsAdmin() {
		agents, _ = a.Store.ListAgents(user.TenantID)
	} else {
		agents, _ = a.Store.ListManagedAgents(user.ID)
	}
	tickets, _ := a.Store.ListTickets(db.TicketFilter{User: user, TeamOnly: user.Role == models.RoleManager})
	if user.Role.IsAdmin() {
		tickets, _ = a.Store.ListTickets(db.TicketFilter{User: user})
	}
	if len(tickets) > 20 {
		tickets = tickets[:20]
	}
	a.Render.Render(w, r, "pages/team.html", pageData{
		"Title": "Команда", "Agents": agents, "Tickets": tickets,
		"Flash": a.Sessions.PopString(r.Context(), "flash"),
	})
}

func (a *App) Admin(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	users, _ := a.Store.ListUsers(user.TenantID)
	queues, _ := a.Store.ListQueues(user.TenantID, false)
	templates, _ := a.Store.ListTemplates(user.TenantID)
	tenants, _ := a.Store.ListTenants()
	managers, _ := a.Store.ListManagers(user.TenantID)
	agents, _ := a.Store.ListAgents(user.TenantID)
	managerNames := map[string]string{}
	for _, m := range managers {
		managerNames[m.ID] = m.FullName
	}
	a.Render.Render(w, r, "pages/admin.html", pageData{
		"Title": "Админка", "Users": users, "Queues": queues, "Templates": templates,
		"Tenants": tenants, "Managers": managers, "Agents": agents, "ManagerNames": managerNames,
		"Flash": a.Sessions.PopString(r.Context(), "flash"),
	})
}

func (a *App) AdminUserRole(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	_ = r.ParseForm()
	id := chi.URLParam(r, "id")
	role := models.Role(r.FormValue("role"))
	if role != models.RoleClient && role != models.RoleAgent && role != models.RoleManager && role != models.RoleAdmin {
		http.Error(w, "Некорректная роль", http.StatusBadRequest)
		return
	}
	target, err := a.Store.GetUserByID(id)
	if err != nil || target.TenantID != user.TenantID {
		http.Error(w, "Пользователь не найден", http.StatusNotFound)
		return
	}
	if err := a.Store.UpdateUserRole(id, role); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if role != models.RoleAgent {
		_ = a.Store.UpdateUserManager(id, "")
	}
	a.Sessions.Put(r.Context(), "flash", "Роль пользователя обновлена")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (a *App) AdminUserManager(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	_ = r.ParseForm()
	agentID := chi.URLParam(r, "id")
	managerID := strings.TrimSpace(r.FormValue("manager_id"))
	agent, err := a.Store.GetUserByID(agentID)
	if err != nil || agent.TenantID != user.TenantID || agent.Role != models.RoleAgent {
		http.Error(w, "Специалист не найден", http.StatusBadRequest)
		return
	}
	if managerID != "" {
		m, err := a.Store.GetUserByID(managerID)
		if err != nil || m.TenantID != user.TenantID || !m.Role.IsManager() {
			http.Error(w, "Менеджер не найден", http.StatusBadRequest)
			return
		}
	}
	if err := a.Store.UpdateUserManager(agentID, managerID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.Sessions.Put(r.Context(), "flash", "Менеджер специалиста обновлён")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (a *App) AdminQueueCreate(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	_ = r.ParseForm()
	name := strings.TrimSpace(r.FormValue("name"))
	desc := strings.TrimSpace(r.FormValue("description"))
	if name == "" {
		http.Error(w, "Укажите название очереди", http.StatusBadRequest)
		return
	}
	if err := a.Store.CreateQueue(user.TenantID, name, desc); err != nil {
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

func (a *App) AdminTenantCreate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	name := strings.TrimSpace(r.FormValue("name"))
	slug := strings.ToLower(strings.TrimSpace(r.FormValue("slug")))
	if name == "" || !slugRe.MatchString(slug) {
		http.Error(w, "Укажите название и slug (латиница, цифры, дефис)", http.StatusBadRequest)
		return
	}
	if _, err := a.Store.CreateTenant(name, slug); err != nil {
		http.Error(w, "Не удалось создать организацию: "+err.Error(), http.StatusBadRequest)
		return
	}
	a.Sessions.Put(r.Context(), "flash", "Организация создана")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

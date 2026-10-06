package handlers

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/rkislov/pomogayka/internal/auth"
	"github.com/rkislov/pomogayka/internal/db"
	"github.com/rkislov/pomogayka/internal/middleware"
	"github.com/rkislov/pomogayka/internal/models"
	"github.com/rkislov/pomogayka/internal/services"
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
	tenant := middleware.TenantFromContext(r.Context())
	ldapEnabled := false
	if tenant != nil {
		if cfg, err := a.Store.GetLDAPSettings(tenant.ID); err == nil {
			ldapEnabled = cfg.IsConfigured()
		}
	}
	a.Render.Render(w, r, "pages/login.html", pageData{
		"Title": "Вход", "Error": "", "LDAPEnabled": ldapEnabled, "PortalTenant": tenant,
	})
}

func (a *App) Login(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	tenant := middleware.TenantFromContext(r.Context())
	ldapEnabled := false
	if tenant != nil {
		if cfg, err := a.Store.GetLDAPSettings(tenant.ID); err == nil {
			ldapEnabled = cfg.IsConfigured()
		}
	}
	fail := func(msg string) {
		a.Render.Render(w, r, "pages/login.html", pageData{
			"Title": "Вход", "Error": msg, "Email": email, "LDAPEnabled": ldapEnabled, "PortalTenant": tenant,
		})
	}
	if tenant == nil {
		fail("Портал организации не определён (проверьте доменное имя)")
		return
	}

	var u *models.User
	if local, err := a.Store.GetUserByEmailInTenant(tenant.ID, email); err == nil && local.IsActive {
		if local.Source != models.UserSourceLDAP && auth.CheckPassword(local.PasswordHash, password) {
			u = local
		}
	}
	if u == nil {
		cfg, err := a.Store.GetLDAPSettings(tenant.ID)
		if err == nil && cfg.IsConfigured() {
			lu, err := services.AuthenticateLDAP(cfg, email, password)
			if err == nil && lu.Email != "" {
				u, err = a.Store.UpsertLDAPUser(tenant.ID, lu.Email, lu.FullName, lu.ExternalID, lu.Role)
				if err != nil {
					fail("Не удалось синхронизировать пользователя LDAP")
					return
				}
				_ = a.Store.ReplaceLDAPPhones(tenant.ID, u.ID, lu.Phones)
			}
		}
	}
	if u == nil || !u.IsActive {
		fail("Неверный email или пароль")
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
	portal := middleware.TenantFromContext(r.Context())
	slug := "default"
	locked := false
	if portal != nil {
		slug = portal.Slug
		// If host maps to a real portal domain (not just fallback), lock slug.
		if _, err := a.Store.GetTenantByHost(r.Host); err == nil {
			locked = true
		}
	}
	a.Render.Render(w, r, "pages/register.html", pageData{
		"Title": "Регистрация", "Error": "", "TenantSlug": slug, "TenantLocked": locked, "PortalTenant": portal,
	})
}

func (a *App) Register(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	email := strings.TrimSpace(r.FormValue("email"))
	fullName := strings.TrimSpace(r.FormValue("full_name"))
	password := r.FormValue("password")
	slug := strings.ToLower(strings.TrimSpace(r.FormValue("tenant_slug")))
	portal := middleware.TenantFromContext(r.Context())
	locked := false
	if _, err := a.Store.GetTenantByHost(r.Host); err == nil && portal != nil {
		slug = portal.Slug
		locked = true
	}
	if slug == "" {
		slug = "default"
	}
	tenant, err := a.Store.GetTenantBySlug(slug)
	if err != nil || !tenant.IsActive {
		a.Render.Render(w, r, "pages/register.html", pageData{
			"Title": "Регистрация", "Error": "Организация не найдена. Укажите корректный slug.",
			"Email": email, "FullName": fullName, "TenantSlug": slug, "TenantLocked": locked, "PortalTenant": portal,
		})
		return
	}
	if len(email) < 3 || len(fullName) < 2 || len(password) < 8 {
		a.Render.Render(w, r, "pages/register.html", pageData{
			"Title": "Регистрация", "Error": "Проверьте поля: имя, email и пароль от 8 символов",
			"Email": email, "FullName": fullName, "TenantSlug": slug, "TenantLocked": locked, "PortalTenant": portal,
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
			"Title": "Регистрация", "Error": "Не удалось создать пользователя (возможно, email уже занят в этой организации)",
			"Email": email, "FullName": fullName, "TenantSlug": slug, "TenantLocked": locked, "PortalTenant": portal,
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
	clients := []models.User{}
	if user.Role.IsStaff() {
		clients, _ = a.Store.ListClientsWithPhones(user.TenantID)
	}
	phone := strings.TrimSpace(r.URL.Query().Get("phone"))
	requesterID := strings.TrimSpace(r.URL.Query().Get("requester_id"))
	fromCall := r.URL.Query().Get("from_call") == "1"
	title := strings.TrimSpace(r.URL.Query().Get("title"))
	desc := strings.TrimSpace(r.URL.Query().Get("description"))
	if fromCall && title == "" {
		if phone != "" {
			title = "Входящий звонок " + phone
		} else {
			title = "Входящий звонок"
		}
	}
	if fromCall && desc == "" {
		desc = "Заявка создана при принятии звонка."
		if phone != "" {
			desc += "\nТелефон: " + phone
		}
	}
	requesterName := ""
	if requesterID != "" {
		if ru, err := a.Store.GetUserByID(requesterID); err == nil && ru.TenantID == user.TenantID {
			requesterName = ru.FullName
		}
	}
	a.Render.Render(w, r, "pages/ticket_new.html", pageData{
		"Title": "Новая заявка", "Queues": queues, "Error": "", "Clients": clients,
		"FormTitle": title, "FormDescription": desc, "Phone": phone,
		"RequesterID": requesterID, "RequesterName": requesterName, "FromCall": fromCall,
	})
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
	authorID := user.ID
	requesterID := strings.TrimSpace(r.FormValue("requester_id"))
	phone := strings.TrimSpace(r.FormValue("phone"))
	if user.Role.IsStaff() && requesterID != "" {
		if ru, err := a.Store.GetUserByID(requesterID); err == nil && ru.TenantID == user.TenantID && ru.IsActive {
			authorID = ru.ID
		}
	}
	if phone != "" && !strings.Contains(description, phone) {
		description = strings.TrimSpace(description + "\n\nТелефон: " + phone)
	}
	queues, _ := a.Store.ListQueues(user.TenantID, true)
	clients := []models.User{}
	if user.Role.IsStaff() {
		clients, _ = a.Store.ListClientsWithPhones(user.TenantID)
	}
	if len(title) < 3 || len(description) < 3 {
		a.Render.Render(w, r, "pages/ticket_new.html", pageData{
			"Title": "Новая заявка", "Queues": queues, "Clients": clients, "Error": "Заполните тему и описание (минимум 3 символа)",
			"FormTitle": title, "FormDescription": description, "Priority": priority, "QueueID": queueID,
			"Phone": phone, "RequesterID": requesterID,
		})
		return
	}
	t, err := a.Store.CreateTicket(user.TenantID, title, description, priority, authorID, qid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if user.Role.IsStaff() && authorID != user.ID {
		_ = a.Store.AssignTicket(t.ID, user.ID)
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
	domains, _ := a.Store.ListTenantDomains(user.TenantID)
	ldap, _ := a.Store.GetLDAPSettings(user.TenantID)
	sip, _ := a.Store.GetSIPSettings(user.TenantID)
	mailboxes, _ := a.Store.ListMailboxes(user.TenantID)
	managerNames := map[string]string{}
	for _, m := range managers {
		managerNames[m.ID] = m.FullName
	}
	a.Render.Render(w, r, "pages/admin.html", pageData{
		"Title": "Админка", "Users": users, "Queues": queues, "Templates": templates,
		"Tenants": tenants, "Managers": managers, "Agents": agents, "ManagerNames": managerNames,
		"Domains": domains, "LDAP": ldap, "SIP": sip, "Mailboxes": mailboxes,
		"Flash": a.Sessions.PopString(r.Context(), "flash"),
		"Error": a.Sessions.PopString(r.Context(), "flash_error"),
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

func (a *App) AdminDomainAdd(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	_ = r.ParseForm()
	host := strings.TrimSpace(r.FormValue("host"))
	primary := r.FormValue("is_primary") == "1"
	if host == "" {
		a.Sessions.Put(r.Context(), "flash_error", "Укажите доменное имя портала")
		http.Redirect(w, r, "/admin#domains", http.StatusSeeOther)
		return
	}
	if _, err := a.Store.AddTenantDomain(user.TenantID, host, primary); err != nil {
		a.Sessions.Put(r.Context(), "flash_error", "Не удалось добавить домен (возможно, уже занят): "+err.Error())
		http.Redirect(w, r, "/admin#domains", http.StatusSeeOther)
		return
	}
	a.Sessions.Put(r.Context(), "flash", "Домен портала добавлен")
	http.Redirect(w, r, "/admin#domains", http.StatusSeeOther)
}

func (a *App) AdminDomainDelete(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if err := a.Store.DeleteTenantDomain(user.TenantID, id); err != nil {
		a.Sessions.Put(r.Context(), "flash_error", err.Error())
	} else {
		a.Sessions.Put(r.Context(), "flash", "Домен удалён")
	}
	http.Redirect(w, r, "/admin#domains", http.StatusSeeOther)
}

func (a *App) AdminLDAPSave(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	_ = r.ParseForm()
	existing, _ := a.Store.GetLDAPSettings(user.TenantID)
	bindPassword := r.FormValue("bind_password")
	if bindPassword == "" && existing != nil {
		bindPassword = existing.BindPassword
	}
	cfg := models.LDAPSettings{
		TenantID:       user.TenantID,
		Enabled:        r.FormValue("enabled") == "1",
		Provider:       r.FormValue("provider"),
		ServerURL:      strings.TrimSpace(r.FormValue("server_url")),
		BindDN:         strings.TrimSpace(r.FormValue("bind_dn")),
		BindPassword:   bindPassword,
		UserBaseDN:     strings.TrimSpace(r.FormValue("user_base_dn")),
		UserFilter:     strings.TrimSpace(r.FormValue("user_filter")),
		EmailAttr:      strings.TrimSpace(r.FormValue("email_attr")),
		NameAttr:       strings.TrimSpace(r.FormValue("name_attr")),
		UsernameAttr:   strings.TrimSpace(r.FormValue("username_attr")),
		PhoneAttr:      strings.TrimSpace(r.FormValue("phone_attr")),
		GroupAttr:      strings.TrimSpace(r.FormValue("group_attr")),
		AgentGroupDN:   strings.TrimSpace(r.FormValue("agent_group_dn")),
		ManagerGroupDN: strings.TrimSpace(r.FormValue("manager_group_dn")),
		AdminGroupDN:   strings.TrimSpace(r.FormValue("admin_group_dn")),
		UseTLS:         r.FormValue("use_tls") == "1",
		StartTLS:       r.FormValue("start_tls") == "1",
		InsecureTLS:    r.FormValue("insecure_tls") == "1",
	}
	if err := a.Store.SaveLDAPSettings(cfg); err != nil {
		a.Sessions.Put(r.Context(), "flash_error", "LDAP: "+err.Error())
	} else {
		a.Sessions.Put(r.Context(), "flash", "Настройки LDAP сохранены")
	}
	http.Redirect(w, r, "/admin#ldap", http.StatusSeeOther)
}

func (a *App) AdminLDAPTest(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	cfg, err := a.Store.GetLDAPSettings(user.TenantID)
	if err != nil {
		a.Sessions.Put(r.Context(), "flash_error", err.Error())
		http.Redirect(w, r, "/admin#ldap", http.StatusSeeOther)
		return
	}
	if err := services.TestLDAP(cfg); err != nil {
		a.Sessions.Put(r.Context(), "flash_error", "LDAP тест: "+err.Error())
	} else {
		a.Sessions.Put(r.Context(), "flash", "LDAP: подключение успешно")
	}
	http.Redirect(w, r, "/admin#ldap", http.StatusSeeOther)
}

func (a *App) AdminMailboxSave(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	_ = r.ParseForm()
	port, _ := strconv.Atoi(r.FormValue("smtp_port"))
	m := models.EmailMailbox{
		ID:           strings.TrimSpace(r.FormValue("id")),
		TenantID:     user.TenantID,
		QueueID:      strings.TrimSpace(r.FormValue("queue_id")),
		Name:         strings.TrimSpace(r.FormValue("name")),
		FromEmail:    strings.TrimSpace(r.FormValue("from_email")),
		SMTPHost:     strings.TrimSpace(r.FormValue("smtp_host")),
		SMTPPort:     port,
		SMTPUsername: strings.TrimSpace(r.FormValue("smtp_username")),
		SMTPPassword: r.FormValue("smtp_password"),
		SMTPUseTLS:   r.FormValue("smtp_use_tls") == "1",
		IsActive:     r.FormValue("is_active") == "1",
	}
	if m.Name == "" {
		a.Sessions.Put(r.Context(), "flash_error", "Укажите название ящика")
		http.Redirect(w, r, "/admin#mail", http.StatusSeeOther)
		return
	}
	if err := a.Store.UpsertMailbox(m); err != nil {
		a.Sessions.Put(r.Context(), "flash_error", "Почта: "+err.Error())
	} else {
		a.Sessions.Put(r.Context(), "flash", "Почтовый ящик сохранён")
	}
	http.Redirect(w, r, "/admin#mail", http.StatusSeeOther)
}

func (a *App) AdminMailboxDelete(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if err := a.Store.DeleteMailbox(user.TenantID, id); err != nil {
		a.Sessions.Put(r.Context(), "flash_error", err.Error())
	} else {
		a.Sessions.Put(r.Context(), "flash", "Почтовый ящик удалён")
	}
	http.Redirect(w, r, "/admin#mail", http.StatusSeeOther)
}

func (a *App) AdminMailboxTest(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	id := chi.URLParam(r, "id")
	list, _ := a.Store.ListMailboxes(user.TenantID)
	var mb *models.EmailMailbox
	for i := range list {
		if list[i].ID == id {
			mb = &list[i]
			break
		}
	}
	if mb == nil {
		a.Sessions.Put(r.Context(), "flash_error", "Ящик не найден")
		http.Redirect(w, r, "/admin#mail", http.StatusSeeOther)
		return
	}
	to := user.Email
	if err := services.SendSMTP(mb, []string{to}, "Помогайка: тест SMTP", "Тестовое письмо от портала "+user.TenantID); err != nil {
		a.Sessions.Put(r.Context(), "flash_error", "SMTP тест: "+err.Error())
	} else {
		a.Sessions.Put(r.Context(), "flash", "Тестовое письмо отправлено на "+to)
	}
	http.Redirect(w, r, "/admin#mail", http.StatusSeeOther)
}


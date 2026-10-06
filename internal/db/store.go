package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rkislov/pomogayka/internal/models"
)

type Store struct {
	DB *Conn
}

func NewStore(database *Conn) *Store {
	return &Store{DB: database}
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

const userSelect = `SELECT id, tenant_id, email, full_name, password_hash, role, COALESCE(manager_id, ''), is_active, telegram_id, COALESCE(jabber_jid, ''), created_at FROM users`

func (s *Store) GetUserByEmail(email string) (*models.User, error) {
	row := s.DB.QueryRow(userSelect+` WHERE email = ?`, strings.ToLower(email))
	return scanUser(row)
}

func (s *Store) GetUserByID(id string) (*models.User, error) {
	row := s.DB.QueryRow(userSelect+` WHERE id = ?`, id)
	return scanUser(row)
}

func (s *Store) GetUserByTelegramID(telegramID int64) (*models.User, error) {
	row := s.DB.QueryRow(userSelect+` WHERE telegram_id = ?`, telegramID)
	return scanUser(row)
}

func (s *Store) GetUserByJabberJID(jid string) (*models.User, error) {
	row := s.DB.QueryRow(userSelect+` WHERE lower(jabber_jid) = lower(?)`, jid)
	return scanUser(row)
}

func scanUser(row *sql.Row) (*models.User, error) {
	var u models.User
	var active int
	var created string
	var telegramID sql.NullInt64
	if err := row.Scan(&u.ID, &u.TenantID, &u.Email, &u.FullName, &u.PasswordHash, &u.Role, &u.ManagerID, &active, &telegramID, &u.JabberJID, &created); err != nil {
		return nil, err
	}
	u.IsActive = active == 1
	u.CreatedAt = parseTime(created)
	if telegramID.Valid {
		v := telegramID.Int64
		u.TelegramID = &v
	}
	return &u, nil
}

func (s *Store) LinkTelegram(userID string, telegramID int64) error {
	_, err := s.DB.Exec(`UPDATE users SET telegram_id = NULL WHERE telegram_id = ?`, telegramID)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`UPDATE users SET telegram_id = ? WHERE id = ?`, telegramID, userID)
	return err
}

func (s *Store) UnlinkTelegram(userID string) error {
	_, err := s.DB.Exec(`UPDATE users SET telegram_id = NULL WHERE id = ?`, userID)
	return err
}

func (s *Store) LinkJabber(userID, jid string) error {
	_, err := s.DB.Exec(`UPDATE users SET jabber_jid = NULL WHERE lower(jabber_jid) = lower(?)`, jid)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`UPDATE users SET jabber_jid = ? WHERE id = ?`, jid, userID)
	return err
}

func (s *Store) UnlinkJabber(userID string) error {
	_, err := s.DB.Exec(`UPDATE users SET jabber_jid = NULL WHERE id = ?`, userID)
	return err
}

func (s *Store) CreateUser(tenantID, email, fullName, passwordHash string, role models.Role) (*models.User, error) {
	u := &models.User{
		ID:           uuid.NewString(),
		TenantID:     tenantID,
		Email:        strings.ToLower(email),
		FullName:     fullName,
		PasswordHash: passwordHash,
		Role:         role,
		IsActive:     true,
		CreatedAt:    time.Now().UTC(),
	}
	_, err := s.DB.Exec(
		`INSERT INTO users (id, tenant_id, email, full_name, password_hash, role, manager_id, is_active, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, NULL, 1, ?)`,
		u.ID, u.TenantID, u.Email, u.FullName, u.PasswordHash, u.Role, u.CreatedAt.Format(time.RFC3339),
	)
	return u, err
}

func (s *Store) ListUsers(tenantID string) ([]models.User, error) {
	rows, err := s.DB.Query(userSelect+` WHERE tenant_id = ? ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.User
	for rows.Next() {
		var u models.User
		var active int
		var created string
		var telegramID sql.NullInt64
		if err := rows.Scan(&u.ID, &u.TenantID, &u.Email, &u.FullName, &u.PasswordHash, &u.Role, &u.ManagerID, &active, &telegramID, &u.JabberJID, &created); err != nil {
			return nil, err
		}
		u.IsActive = active == 1
		u.CreatedAt = parseTime(created)
		if telegramID.Valid {
			v := telegramID.Int64
			u.TelegramID = &v
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) ListAgents(tenantID string) ([]models.User, error) {
	rows, err := s.DB.Query(userSelect+` WHERE tenant_id = ? AND role = 'agent' AND is_active = 1 ORDER BY full_name`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.User
	for rows.Next() {
		u, err := scanUserRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

func (s *Store) ListManagedAgents(managerID string) ([]models.User, error) {
	rows, err := s.DB.Query(userSelect+` WHERE manager_id = ? AND role = 'agent' AND is_active = 1 ORDER BY full_name`, managerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.User
	for rows.Next() {
		u, err := scanUserRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

func (s *Store) ListManagers(tenantID string) ([]models.User, error) {
	rows, err := s.DB.Query(userSelect+` WHERE tenant_id = ? AND role IN ('manager','admin') AND is_active = 1 ORDER BY full_name`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.User
	for rows.Next() {
		u, err := scanUserRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUserRow(row rowScanner) (*models.User, error) {
	var u models.User
	var active int
	var created string
	var telegramID sql.NullInt64
	if err := row.Scan(&u.ID, &u.TenantID, &u.Email, &u.FullName, &u.PasswordHash, &u.Role, &u.ManagerID, &active, &telegramID, &u.JabberJID, &created); err != nil {
		return nil, err
	}
	u.IsActive = active == 1
	u.CreatedAt = parseTime(created)
	if telegramID.Valid {
		v := telegramID.Int64
		u.TelegramID = &v
	}
	return &u, nil
}

func (s *Store) UpdateUserRole(id string, role models.Role) error {
	_, err := s.DB.Exec(`UPDATE users SET role = ? WHERE id = ?`, role, id)
	return err
}

func (s *Store) UpdateUserManager(agentID, managerID string) error {
	var mid any
	if managerID != "" {
		mid = managerID
	}
	_, err := s.DB.Exec(`UPDATE users SET manager_id = ? WHERE id = ? AND role = 'agent'`, mid, agentID)
	return err
}

func (s *Store) GetTenant(id string) (*models.Tenant, error) {
	row := s.DB.QueryRow(`SELECT id, name, slug, is_active, created_at FROM tenants WHERE id = ?`, id)
	return scanTenant(row)
}

func (s *Store) GetTenantBySlug(slug string) (*models.Tenant, error) {
	row := s.DB.QueryRow(`SELECT id, name, slug, is_active, created_at FROM tenants WHERE lower(slug) = lower(?)`, slug)
	return scanTenant(row)
}

func scanTenant(row *sql.Row) (*models.Tenant, error) {
	var t models.Tenant
	var active int
	var created string
	if err := row.Scan(&t.ID, &t.Name, &t.Slug, &active, &created); err != nil {
		return nil, err
	}
	t.IsActive = active == 1
	t.CreatedAt = parseTime(created)
	return &t, nil
}

func (s *Store) ListTenants() ([]models.Tenant, error) {
	rows, err := s.DB.Query(`SELECT id, name, slug, is_active, created_at FROM tenants ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Tenant
	for rows.Next() {
		var t models.Tenant
		var active int
		var created string
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &active, &created); err != nil {
			return nil, err
		}
		t.IsActive = active == 1
		t.CreatedAt = parseTime(created)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) CreateTenant(name, slug string) (*models.Tenant, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	t := &models.Tenant{
		ID:        uuid.NewString(),
		Name:      strings.TrimSpace(name),
		Slug:      slug,
		IsActive:  true,
		CreatedAt: time.Now().UTC(),
	}
	_, err := s.DB.Exec(
		`INSERT INTO tenants (id, name, slug, is_active, created_at) VALUES (?, ?, ?, 1, ?)`,
		t.ID, t.Name, t.Slug, t.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return nil, err
	}
	_, _ = s.DB.InsertIgnore(`INSERT INTO ticket_counters (name, value) VALUES (?, 0)`, "tickets:"+t.ID)
	now := time.Now().UTC().Format(time.RFC3339)
	defaults := []struct{ event, subject, body string }{
		{"ticket_created", "Помогайка: заявка #{{ticket.number}} создана", "Заявка #{{ticket.number}} {{ticket.title}} создана.\n\n{{ticket.description}}"},
		{"status_changed", "Помогайка: статус #{{ticket.number}}", "Статус заявки #{{ticket.number}} изменён на {{ticket.status}}."},
		{"comment_public", "Помогайка: ответ по #{{ticket.number}}", "По заявке #{{ticket.number}} добавлен ответ.\n\n{{comment.text}}"},
		{"assigned", "Помогайка: назначена #{{ticket.number}}", "Заявка #{{ticket.number}} назначена на {{assignee.full_name}}."},
	}
	for _, d := range defaults {
		_, _ = s.DB.InsertIgnore(
			`INSERT INTO notification_templates (id, tenant_id, event, subject, body, is_active, updated_at) VALUES (?, ?, ?, ?, ?, 1, ?)`,
			uuid.NewString(), t.ID, d.event, d.subject, d.body, now,
		)
	}
	return t, nil
}

func (s *Store) ListQueues(tenantID string, activeOnly bool) ([]models.Queue, error) {
	q := `SELECT id, tenant_id, name, description, is_active FROM queues WHERE tenant_id = ?`
	args := []any{tenantID}
	if activeOnly {
		q += ` AND is_active = 1`
	}
	q += ` ORDER BY name`
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Queue
	for rows.Next() {
		var item models.Queue
		var active int
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Name, &item.Description, &active); err != nil {
			return nil, err
		}
		item.IsActive = active == 1
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) CreateQueue(tenantID, name, description string) error {
	_, err := s.DB.Exec(
		`INSERT INTO queues (id, tenant_id, name, description, is_active) VALUES (?, ?, ?, ?, 1)`,
		uuid.NewString(), tenantID, name, description,
	)
	return err
}

func (s *Store) nextTicketNumber(tenantID string) (string, error) {
	key := "tickets:" + tenantID
	tx, err := s.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var value int
	err = tx.QueryRow(`SELECT value FROM ticket_counters WHERE name = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		if _, err := tx.Exec(`INSERT INTO ticket_counters (name, value) VALUES (?, 0)`, key); err != nil {
			return "", err
		}
		value = 0
	} else if err != nil {
		return "", err
	}
	value++
	if _, err := tx.Exec(`UPDATE ticket_counters SET value = ? WHERE name = ?`, value, key); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return fmt.Sprintf("P-%05d", value), nil
}

func (s *Store) CreateTicket(tenantID, title, description string, priority models.Priority, authorID string, queueID *string) (*models.Ticket, error) {
	number, err := s.nextTicketNumber(tenantID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	t := &models.Ticket{
		ID:          uuid.NewString(),
		TenantID:    tenantID,
		Number:      number,
		Title:       title,
		Description: description,
		Status:      models.StatusNew,
		Priority:    priority,
		AuthorID:    authorID,
		QueueID:     queueID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	var qid any
	if queueID != nil && *queueID != "" {
		qid = *queueID
	}
	_, err = s.DB.Exec(
		`INSERT INTO tickets (id, tenant_id, number, title, description, status, priority, author_id, assignee_id, queue_id, created_at, updated_at, closed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, NULL)`,
		t.ID, t.TenantID, t.Number, t.Title, t.Description, t.Status, t.Priority, t.AuthorID, qid, t.CreatedAt.Format(time.RFC3339), t.UpdatedAt.Format(time.RFC3339),
	)
	return t, err
}

type TicketFilter struct {
	User     *models.User
	Status   string
	Query    string
	OnlyMine bool
	TeamOnly bool
}

func (s *Store) ListTickets(f TicketFilter) ([]models.Ticket, error) {
	query := `
SELECT t.id, t.tenant_id, t.number, t.title, t.description, t.status, t.priority, t.author_id, t.assignee_id, t.queue_id, t.created_at, t.updated_at, t.closed_at,
       a.id, a.full_name, a.email, a.role,
       s.id, s.full_name, s.email, s.role,
       q.id, q.name
FROM tickets t
JOIN users a ON a.id = t.author_id
LEFT JOIN users s ON s.id = t.assignee_id
LEFT JOIN queues q ON q.id = t.queue_id
WHERE 1=1`
	args := []any{}
	if f.User != nil {
		query += ` AND t.tenant_id = ?`
		args = append(args, f.User.TenantID)
		if !f.User.Role.IsStaff() {
			query += ` AND t.author_id = ?`
			args = append(args, f.User.ID)
		}
		if f.OnlyMine {
			query += ` AND t.assignee_id = ?`
			args = append(args, f.User.ID)
		}
		if f.TeamOnly && f.User.Role == models.RoleManager {
			query += ` AND (t.assignee_id = ? OR t.assignee_id IN (SELECT id FROM users WHERE manager_id = ?))`
			args = append(args, f.User.ID, f.User.ID)
		}
	}
	if f.Status != "" {
		query += ` AND t.status = ?`
		args = append(args, f.Status)
	}
	if strings.TrimSpace(f.Query) != "" {
		like := "%" + strings.TrimSpace(f.Query) + "%"
		query += ` AND (t.number LIKE ? OR t.title LIKE ? OR t.description LIKE ?)`
		args = append(args, like, like, like)
	}
	query += ` ORDER BY t.created_at DESC LIMIT 200`
	rows, err := s.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Ticket
	for rows.Next() {
		t, err := scanTicketRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

type scannable interface {
	Scan(dest ...any) error
}

func scanTicketRow(row scannable) (*models.Ticket, error) {
	var t models.Ticket
	var assigneeID, queueID, closedAt sql.NullString
	var authorID, authorName, authorEmail string
	var authorRole models.Role
	var assigneeUID, assigneeName, assigneeEmail sql.NullString
	var assigneeRole sql.NullString
	var queueUID, queueName sql.NullString
	var created, updated string
	if err := row.Scan(
		&t.ID, &t.TenantID, &t.Number, &t.Title, &t.Description, &t.Status, &t.Priority, &t.AuthorID, &assigneeID, &queueID, &created, &updated, &closedAt,
		&authorID, &authorName, &authorEmail, &authorRole,
		&assigneeUID, &assigneeName, &assigneeEmail, &assigneeRole,
		&queueUID, &queueName,
	); err != nil {
		return nil, err
	}
	t.CreatedAt = parseTime(created)
	t.UpdatedAt = parseTime(updated)
	if assigneeID.Valid {
		t.AssigneeID = &assigneeID.String
	}
	if queueID.Valid {
		t.QueueID = &queueID.String
	}
	if closedAt.Valid {
		ct := parseTime(closedAt.String)
		t.ClosedAt = &ct
	}
	t.Author = &models.User{ID: authorID, FullName: authorName, Email: authorEmail, Role: authorRole}
	if assigneeUID.Valid {
		t.Assignee = &models.User{ID: assigneeUID.String, FullName: assigneeName.String, Email: assigneeEmail.String, Role: models.Role(assigneeRole.String)}
	}
	if queueUID.Valid {
		t.Queue = &models.Queue{ID: queueUID.String, Name: queueName.String}
	}
	return &t, nil
}

func (s *Store) GetTicket(id string) (*models.Ticket, error) {
	row := s.DB.QueryRow(`
SELECT t.id, t.tenant_id, t.number, t.title, t.description, t.status, t.priority, t.author_id, t.assignee_id, t.queue_id, t.created_at, t.updated_at, t.closed_at,
       a.id, a.full_name, a.email, a.role,
       s.id, s.full_name, s.email, s.role,
       q.id, q.name
FROM tickets t
JOIN users a ON a.id = t.author_id
LEFT JOIN users s ON s.id = t.assignee_id
LEFT JOIN queues q ON q.id = t.queue_id
WHERE t.id = ?`, id)
	return scanTicketRow(row)
}

func (s *Store) GetTicketByNumber(number string) (*models.Ticket, error) {
	row := s.DB.QueryRow(`
SELECT t.id, t.tenant_id, t.number, t.title, t.description, t.status, t.priority, t.author_id, t.assignee_id, t.queue_id, t.created_at, t.updated_at, t.closed_at,
       a.id, a.full_name, a.email, a.role,
       s.id, s.full_name, s.email, s.role,
       q.id, q.name
FROM tickets t
JOIN users a ON a.id = t.author_id
LEFT JOIN users s ON s.id = t.assignee_id
LEFT JOIN queues q ON q.id = t.queue_id
WHERE t.number = ? OR t.id = ?`, number, number)
	return scanTicketRow(row)
}

func (s *Store) UpdateTicketStatus(id string, status models.TicketStatus, assigneeID *string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var closed any
	if status == models.StatusClosed || status == models.StatusCancelled {
		closed = now
	}
	var assignee any
	if assigneeID != nil {
		assignee = *assigneeID
	}
	_, err := s.DB.Exec(`UPDATE tickets SET status = ?, assignee_id = COALESCE(?, assignee_id), updated_at = ?, closed_at = ? WHERE id = ?`,
		status, assignee, now, closed, id)
	return err
}

func (s *Store) AssignTicket(id, assigneeID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.DB.Exec(`UPDATE tickets SET assignee_id = ?, status = ?, updated_at = ? WHERE id = ?`,
		assigneeID, models.StatusInProgress, now, id)
	return err
}

func (s *Store) ListComments(ticketID string, includeInternal bool) ([]models.Comment, error) {
	q := `
SELECT c.id, c.ticket_id, c.author_id, c.body, c.is_internal, c.created_at, u.id, u.full_name, u.email, u.role
FROM comments c JOIN users u ON u.id = c.author_id
WHERE c.ticket_id = ?`
	if !includeInternal {
		q += ` AND c.is_internal = 0`
	}
	q += ` ORDER BY c.created_at ASC`
	rows, err := s.DB.Query(q, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Comment
	for rows.Next() {
		var c models.Comment
		var internal int
		var created string
		var u models.User
		if err := rows.Scan(&c.ID, &c.TicketID, &c.AuthorID, &c.Body, &internal, &created, &u.ID, &u.FullName, &u.Email, &u.Role); err != nil {
			return nil, err
		}
		c.IsInternal = internal == 1
		c.CreatedAt = parseTime(created)
		c.Author = &u
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) AddComment(ticketID, authorID, body string, internal bool) (*models.Comment, error) {
	c := &models.Comment{
		ID:         uuid.NewString(),
		TicketID:   ticketID,
		AuthorID:   authorID,
		Body:       body,
		IsInternal: internal,
		CreatedAt:  time.Now().UTC(),
	}
	internalInt := 0
	if internal {
		internalInt = 1
	}
	_, err := s.DB.Exec(
		`INSERT INTO comments (id, ticket_id, author_id, body, is_internal, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		c.ID, c.TicketID, c.AuthorID, c.Body, internalInt, c.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return nil, err
	}
	_, _ = s.DB.Exec(`UPDATE tickets SET updated_at = ? WHERE id = ?`, time.Now().UTC().Format(time.RFC3339), ticketID)
	return c, nil
}

func (s *Store) DashboardStats(user *models.User) (models.DashboardStats, error) {
	var st models.DashboardStats
	if user == nil {
		return st, nil
	}
	weekAgo := time.Now().UTC().AddDate(0, 0, -7).Format(time.RFC3339)
	tenant := user.TenantID
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM tickets WHERE tenant_id = ? AND status = 'new'`, tenant).Scan(&st.OpenTickets)
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM tickets WHERE tenant_id = ? AND status = 'in_progress'`, tenant).Scan(&st.InProgress)
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM tickets WHERE tenant_id = ? AND status = 'awaiting_requester'`, tenant).Scan(&st.Awaiting)
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM tickets WHERE tenant_id = ? AND status = 'closed' AND closed_at >= ?`, tenant, weekAgo).Scan(&st.ClosedThisWeek)
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM tickets WHERE tenant_id = ? AND assignee_id = ? AND status NOT IN ('closed','cancelled')`, tenant, user.ID).Scan(&st.MyAssigned)
	if user.Role == models.RoleManager || user.Role.IsAdmin() {
		_ = s.DB.QueryRow(`
SELECT COUNT(*) FROM tickets
WHERE tenant_id = ? AND status NOT IN ('closed','cancelled')
  AND assignee_id IN (SELECT id FROM users WHERE manager_id = ?)`, tenant, user.ID).Scan(&st.TeamAssigned)
	}
	return st, nil
}

func (s *Store) ListTemplates(tenantID string) ([]models.NotificationTemplate, error) {
	rows, err := s.DB.Query(`SELECT id, tenant_id, event, subject, body, is_active, updated_at FROM notification_templates WHERE tenant_id = ? ORDER BY event`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.NotificationTemplate
	for rows.Next() {
		var t models.NotificationTemplate
		var active int
		var updated string
		if err := rows.Scan(&t.ID, &t.TenantID, &t.Event, &t.Subject, &t.Body, &active, &updated); err != nil {
			return nil, err
		}
		t.IsActive = active == 1
		t.UpdatedAt = parseTime(updated)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) UpdateTemplate(id, subject, body string, active bool) error {
	activeInt := 0
	if active {
		activeInt = 1
	}
	_, err := s.DB.Exec(
		`UPDATE notification_templates SET subject = ?, body = ?, is_active = ?, updated_at = ? WHERE id = ?`,
		subject, body, activeInt, time.Now().UTC().Format(time.RFC3339), id,
	)
	return err
}

func (s *Store) IsAgentManagedBy(agentID, managerID string) bool {
	var n int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE id = ? AND manager_id = ? AND role = 'agent'`, agentID, managerID).Scan(&n)
	return n > 0
}

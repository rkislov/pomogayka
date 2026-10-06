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
	DB *sql.DB
}

func NewStore(database *sql.DB) *Store {
	return &Store{DB: database}
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func (s *Store) GetUserByEmail(email string) (*models.User, error) {
	row := s.DB.QueryRow(`SELECT id, email, full_name, password_hash, role, is_active, telegram_id, created_at FROM users WHERE email = ?`, strings.ToLower(email))
	return scanUser(row)
}

func (s *Store) GetUserByID(id string) (*models.User, error) {
	row := s.DB.QueryRow(`SELECT id, email, full_name, password_hash, role, is_active, telegram_id, created_at FROM users WHERE id = ?`, id)
	return scanUser(row)
}

func (s *Store) GetUserByTelegramID(telegramID int64) (*models.User, error) {
	row := s.DB.QueryRow(`SELECT id, email, full_name, password_hash, role, is_active, telegram_id, created_at FROM users WHERE telegram_id = ?`, telegramID)
	return scanUser(row)
}

func scanUser(row *sql.Row) (*models.User, error) {
	var u models.User
	var active int
	var created string
	var telegramID sql.NullInt64
	if err := row.Scan(&u.ID, &u.Email, &u.FullName, &u.PasswordHash, &u.Role, &active, &telegramID, &created); err != nil {
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

func (s *Store) GetTicketByNumber(number string) (*models.Ticket, error) {
	row := s.DB.QueryRow(`
SELECT t.id, t.number, t.title, t.description, t.status, t.priority, t.author_id, t.assignee_id, t.queue_id, t.created_at, t.updated_at, t.closed_at,
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

func (s *Store) CreateUser(email, fullName, passwordHash string, role models.Role) (*models.User, error) {
	u := &models.User{
		ID:           uuid.NewString(),
		Email:        strings.ToLower(email),
		FullName:     fullName,
		PasswordHash: passwordHash,
		Role:         role,
		IsActive:     true,
		CreatedAt:    time.Now().UTC(),
	}
	_, err := s.DB.Exec(
		`INSERT INTO users (id, email, full_name, password_hash, role, is_active, created_at) VALUES (?, ?, ?, ?, ?, 1, ?)`,
		u.ID, u.Email, u.FullName, u.PasswordHash, u.Role, u.CreatedAt.Format(time.RFC3339),
	)
	return u, err
}

func (s *Store) ListUsers() ([]models.User, error) {
	rows, err := s.DB.Query(`SELECT id, email, full_name, password_hash, role, is_active, telegram_id, created_at FROM users ORDER BY created_at DESC`)
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
		if err := rows.Scan(&u.ID, &u.Email, &u.FullName, &u.PasswordHash, &u.Role, &active, &telegramID, &created); err != nil {
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

func (s *Store) UpdateUserRole(id string, role models.Role) error {
	_, err := s.DB.Exec(`UPDATE users SET role = ? WHERE id = ?`, role, id)
	return err
}

func (s *Store) ListQueues(activeOnly bool) ([]models.Queue, error) {
	q := `SELECT id, name, description, is_active FROM queues`
	if activeOnly {
		q += ` WHERE is_active = 1`
	}
	q += ` ORDER BY name`
	rows, err := s.DB.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Queue
	for rows.Next() {
		var item models.Queue
		var active int
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &active); err != nil {
			return nil, err
		}
		item.IsActive = active == 1
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) CreateQueue(name, description string) error {
	_, err := s.DB.Exec(
		`INSERT INTO queues (id, name, description, is_active) VALUES (?, ?, ?, 1)`,
		uuid.NewString(), name, description,
	)
	return err
}

func (s *Store) nextTicketNumber() (string, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var value int
	if err := tx.QueryRow(`SELECT value FROM ticket_counters WHERE name = 'tickets'`).Scan(&value); err != nil {
		return "", err
	}
	value++
	if _, err := tx.Exec(`UPDATE ticket_counters SET value = ? WHERE name = 'tickets'`, value); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return fmt.Sprintf("P-%05d", value), nil
}

func (s *Store) CreateTicket(title, description string, priority models.Priority, authorID string, queueID *string) (*models.Ticket, error) {
	number, err := s.nextTicketNumber()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	t := &models.Ticket{
		ID:          uuid.NewString(),
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
		`INSERT INTO tickets (id, number, title, description, status, priority, author_id, assignee_id, queue_id, created_at, updated_at, closed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, NULL)`,
		t.ID, t.Number, t.Title, t.Description, t.Status, t.Priority, t.AuthorID, qid, t.CreatedAt.Format(time.RFC3339), t.UpdatedAt.Format(time.RFC3339),
	)
	return t, err
}

type TicketFilter struct {
	User       *models.User
	Status     string
	Query      string
	OnlyMine   bool
}

func (s *Store) ListTickets(f TicketFilter) ([]models.Ticket, error) {
	query := `
SELECT t.id, t.number, t.title, t.description, t.status, t.priority, t.author_id, t.assignee_id, t.queue_id, t.created_at, t.updated_at, t.closed_at,
       a.id, a.full_name, a.email, a.role,
       s.id, s.full_name, s.email, s.role,
       q.id, q.name
FROM tickets t
JOIN users a ON a.id = t.author_id
LEFT JOIN users s ON s.id = t.assignee_id
LEFT JOIN queues q ON q.id = t.queue_id
WHERE 1=1`
	args := []any{}
	if f.User != nil && !f.User.Role.IsStaff() {
		query += ` AND t.author_id = ?`
		args = append(args, f.User.ID)
	}
	if f.OnlyMine && f.User != nil {
		query += ` AND t.assignee_id = ?`
		args = append(args, f.User.ID)
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
		&t.ID, &t.Number, &t.Title, &t.Description, &t.Status, &t.Priority, &t.AuthorID, &assigneeID, &queueID, &created, &updated, &closedAt,
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
SELECT t.id, t.number, t.title, t.description, t.status, t.priority, t.author_id, t.assignee_id, t.queue_id, t.created_at, t.updated_at, t.closed_at,
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
	weekAgo := time.Now().UTC().AddDate(0, 0, -7).Format(time.RFC3339)
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM tickets WHERE status = 'new'`).Scan(&st.OpenTickets)
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM tickets WHERE status = 'in_progress'`).Scan(&st.InProgress)
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM tickets WHERE status = 'awaiting_requester'`).Scan(&st.Awaiting)
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM tickets WHERE status = 'closed' AND closed_at >= ?`, weekAgo).Scan(&st.ClosedThisWeek)
	if user != nil {
		_ = s.DB.QueryRow(`SELECT COUNT(*) FROM tickets WHERE assignee_id = ? AND status NOT IN ('closed','cancelled')`, user.ID).Scan(&st.MyAssigned)
	}
	return st, nil
}

func (s *Store) ListTemplates() ([]models.NotificationTemplate, error) {
	rows, err := s.DB.Query(`SELECT id, event, subject, body, is_active, updated_at FROM notification_templates ORDER BY event`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.NotificationTemplate
	for rows.Next() {
		var t models.NotificationTemplate
		var active int
		var updated string
		if err := rows.Scan(&t.ID, &t.Event, &t.Subject, &t.Body, &active, &updated); err != nil {
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

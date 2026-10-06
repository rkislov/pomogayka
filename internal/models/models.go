package models

import "time"

type Role string

const (
	RoleClient  Role = "client"
	RoleAgent   Role = "agent"
	RoleManager Role = "manager"
	RoleAdmin   Role = "admin"
)

func (r Role) Label() string {
	switch r {
	case RoleAdmin:
		return "Администратор"
	case RoleManager:
		return "Менеджер"
	case RoleAgent:
		return "Специалист"
	default:
		return "Заявитель"
	}
}

func (r Role) IsStaff() bool {
	return r == RoleAgent || r == RoleManager || r == RoleAdmin
}

func (r Role) IsManager() bool {
	return r == RoleManager || r == RoleAdmin
}

func (r Role) IsAdmin() bool {
	return r == RoleAdmin
}

type TicketStatus string

const (
	StatusNew               TicketStatus = "new"
	StatusInProgress        TicketStatus = "in_progress"
	StatusAwaitingRequester TicketStatus = "awaiting_requester"
	StatusClosed            TicketStatus = "closed"
	StatusCancelled         TicketStatus = "cancelled"
)

func (s TicketStatus) Label() string {
	switch s {
	case StatusNew:
		return "Новый"
	case StatusInProgress:
		return "В работе"
	case StatusAwaitingRequester:
		return "Ожидание ответа"
	case StatusClosed:
		return "Закрыт"
	case StatusCancelled:
		return "Отменён"
	default:
		return string(s)
	}
}

type Priority string

const (
	PriorityLow      Priority = "low"
	PriorityMedium   Priority = "medium"
	PriorityHigh     Priority = "high"
	PriorityCritical Priority = "critical"
)

func (p Priority) Label() string {
	switch p {
	case PriorityLow:
		return "Низкий"
	case PriorityMedium:
		return "Средний"
	case PriorityHigh:
		return "Высокий"
	case PriorityCritical:
		return "Критический"
	default:
		return string(p)
	}
}

type Tenant struct {
	ID        string
	Name      string
	Slug      string
	IsActive  bool
	CreatedAt time.Time
}

type User struct {
	ID           string
	TenantID     string
	Email        string
	FullName     string
	PasswordHash string
	Role         Role
	ManagerID    string
	IsActive     bool
	TelegramID   *int64
	JabberJID    string
	CreatedAt    time.Time

	Tenant  *Tenant
	Manager *User
}

type Queue struct {
	ID          string
	TenantID    string
	Name        string
	Description string
	IsActive    bool
}

type Ticket struct {
	ID          string
	TenantID    string
	Number      string
	Title       string
	Description string
	Status      TicketStatus
	Priority    Priority
	AuthorID    string
	AssigneeID  *string
	QueueID     *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ClosedAt    *time.Time

	Author   *User
	Assignee *User
	Queue    *Queue
}

type Comment struct {
	ID         string
	TicketID   string
	AuthorID   string
	Body       string
	IsInternal bool
	CreatedAt  time.Time
	Author     *User
}

type NotificationTemplate struct {
	ID        string
	TenantID  string
	Event     string
	Subject   string
	Body      string
	IsActive  bool
	UpdatedAt time.Time
}

type DashboardStats struct {
	OpenTickets    int
	InProgress     int
	Awaiting       int
	ClosedThisWeek int
	MyAssigned     int
	TeamAssigned   int
}

func CanAccessTicket(user *User, ticket *Ticket) bool {
	if user == nil || ticket == nil {
		return false
	}
	if user.TenantID != "" && ticket.TenantID != "" && user.TenantID != ticket.TenantID {
		return false
	}
	if user.Role.IsAdmin() || user.Role == RoleManager || user.Role == RoleAgent {
		return true
	}
	return ticket.AuthorID == user.ID
}

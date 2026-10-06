package models

import "time"

type Role string

const (
	RoleClient Role = "client"
	RoleAgent  Role = "agent"
	RoleAdmin  Role = "admin"
)

func (r Role) Label() string {
	switch r {
	case RoleAdmin:
		return "Администратор"
	case RoleAgent:
		return "Специалист"
	default:
		return "Заявитель"
	}
}

func (r Role) IsStaff() bool {
	return r == RoleAgent || r == RoleAdmin
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

type User struct {
	ID           string
	Email        string
	FullName     string
	PasswordHash string
	Role         Role
	IsActive     bool
	TelegramID   *int64
	CreatedAt    time.Time
}

type Queue struct {
	ID          string
	Name        string
	Description string
	IsActive    bool
}

type Ticket struct {
	ID          string
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
}

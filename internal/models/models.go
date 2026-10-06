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

type UserSource string

const (
	UserSourceManual UserSource = "manual"
	UserSourceLDAP   UserSource = "ldap"
)

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
	Domains   []TenantDomain
}

type TenantDomain struct {
	ID        string
	TenantID  string
	Host      string
	IsPrimary bool
	CreatedAt time.Time
}

type LDAPSettings struct {
	TenantID       string
	Enabled        bool
	Provider       string // ad | freeipa
	ServerURL      string
	BindDN         string
	BindPassword   string
	UserBaseDN     string
	UserFilter     string
	EmailAttr      string
	NameAttr       string
	UsernameAttr   string
	PhoneAttr      string
	GroupAttr      string
	AgentGroupDN   string
	ManagerGroupDN string
	AdminGroupDN   string
	UseTLS         bool
	StartTLS       bool
	InsecureTLS    bool
	UpdatedAt      time.Time
}

func (s LDAPSettings) IsConfigured() bool {
	return s.Enabled && s.ServerURL != "" && s.UserBaseDN != ""
}

type EmailMailbox struct {
	ID           string
	TenantID     string
	QueueID      string
	Name         string
	FromEmail    string
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPUseTLS   bool
	IsActive     bool
	CreatedAt    time.Time
	QueueName    string
}

func (m EmailMailbox) IsReady() bool {
	return m.IsActive && m.SMTPHost != "" && m.FromEmail != ""
}

type User struct {
	ID           string
	TenantID     string
	Email        string
	FullName     string
	PasswordHash string
	Role         Role
	ManagerID    string
	Source       UserSource
	ExternalID   string
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


type SIPSettings struct {
	TenantID      string
	Enabled       bool
	WebsocketURL  string
	SIPDomain     string
	OutboundProxy string
	STUNURLs      string
	TURNURLs      string
	TURNUsername  string
	TURNPassword  string
	UpdatedAt     time.Time
}

func (s SIPSettings) IsConfigured() bool {
	return s.Enabled && s.WebsocketURL != "" && s.SIPDomain != ""
}

type UserSIPCredential struct {
	UserID        string
	TenantID      string
	Extension     string
	AuthUsername  string
	Password      string
	DisplayName   string
	AutoRegister  bool
	UpdatedAt     time.Time
}

type PhoneSource string

const (
	PhoneSourceManual PhoneSource = "manual"
	PhoneSourceLDAP   PhoneSource = "ldap"
)

type UserPhone struct {
	ID              string
	TenantID        string
	UserID          string
	Phone           string
	PhoneNormalized string
	Label           string
	Source          PhoneSource
	IsPrimary       bool
	CreatedAt       time.Time
	UserName        string
	UserEmail       string
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

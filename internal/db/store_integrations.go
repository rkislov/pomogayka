package db

import (
	"database/sql"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rkislov/pomogayka/internal/models"
)

func NormalizeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(host, ".")
	return host
}

func (s *Store) GetTenantByHost(host string) (*models.Tenant, error) {
	host = NormalizeHost(host)
	if host == "" {
		return nil, sql.ErrNoRows
	}
	row := s.DB.QueryRow(`
SELECT t.id, t.name, t.slug, t.is_active, t.created_at
FROM tenant_domains d
JOIN tenants t ON t.id = d.tenant_id
WHERE d.host = ? AND t.is_active = 1
LIMIT 1`, host)
	return scanTenant(row)
}

func (s *Store) ListTenantDomains(tenantID string) ([]models.TenantDomain, error) {
	rows, err := s.DB.Query(`
SELECT id, tenant_id, host, is_primary, created_at
FROM tenant_domains WHERE tenant_id = ? ORDER BY is_primary DESC, host`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.TenantDomain
	for rows.Next() {
		var d models.TenantDomain
		var primary int
		var created string
		if err := rows.Scan(&d.ID, &d.TenantID, &d.Host, &primary, &created); err != nil {
			return nil, err
		}
		d.IsPrimary = primary == 1
		d.CreatedAt = parseTime(created)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) AddTenantDomain(tenantID, host string, isPrimary bool) (*models.TenantDomain, error) {
	host = NormalizeHost(host)
	if host == "" {
		return nil, sql.ErrNoRows
	}
	if isPrimary {
		_, _ = s.DB.Exec(`UPDATE tenant_domains SET is_primary = 0 WHERE tenant_id = ?`, tenantID)
	}
	d := &models.TenantDomain{
		ID:        uuid.NewString(),
		TenantID:  tenantID,
		Host:      host,
		IsPrimary: isPrimary,
		CreatedAt: time.Now().UTC(),
	}
	primary := 0
	if isPrimary {
		primary = 1
	}
	_, err := s.DB.Exec(
		`INSERT INTO tenant_domains (id, tenant_id, host, is_primary, created_at) VALUES (?, ?, ?, ?, ?)`,
		d.ID, d.TenantID, d.Host, primary, d.CreatedAt.Format(time.RFC3339),
	)
	return d, err
}

func (s *Store) DeleteTenantDomain(tenantID, domainID string) error {
	_, err := s.DB.Exec(`DELETE FROM tenant_domains WHERE id = ? AND tenant_id = ?`, domainID, tenantID)
	return err
}

func (s *Store) GetLDAPSettings(tenantID string) (*models.LDAPSettings, error) {
	row := s.DB.QueryRow(`
SELECT tenant_id, enabled, provider, server_url, bind_dn, bind_password, user_base_dn, user_filter,
       email_attr, name_attr, username_attr, group_attr, agent_group_dn, manager_group_dn, admin_group_dn,
       use_tls, start_tls, insecure_tls, updated_at
FROM tenant_ldap_settings WHERE tenant_id = ?`, tenantID)
	var sset models.LDAPSettings
	var enabled, useTLS, startTLS, insecure int
	var updated string
	err := row.Scan(
		&sset.TenantID, &enabled, &sset.Provider, &sset.ServerURL, &sset.BindDN, &sset.BindPassword,
		&sset.UserBaseDN, &sset.UserFilter, &sset.EmailAttr, &sset.NameAttr, &sset.UsernameAttr, &sset.GroupAttr,
		&sset.AgentGroupDN, &sset.ManagerGroupDN, &sset.AdminGroupDN, &useTLS, &startTLS, &insecure, &updated,
	)
	if err == sql.ErrNoRows {
		return &models.LDAPSettings{
			TenantID:     tenantID,
			Provider:     "ad",
			UserFilter:   "(&(objectClass=user)(!(userAccountControl:1.2.840.113556.1.4.803:=2)))",
			EmailAttr:    "mail",
			NameAttr:     "displayName",
			UsernameAttr: "sAMAccountName",
			GroupAttr:    "memberOf",
			UseTLS:       true,
		}, nil
	}
	if err != nil {
		return nil, err
	}
	sset.Enabled = enabled == 1
	sset.UseTLS = useTLS == 1
	sset.StartTLS = startTLS == 1
	sset.InsecureTLS = insecure == 1
	sset.UpdatedAt = parseTime(updated)
	return &sset, nil
}

func (s *Store) SaveLDAPSettings(cfg models.LDAPSettings) error {
	now := time.Now().UTC().Format(time.RFC3339)
	b := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	if cfg.Provider == "" {
		cfg.Provider = "ad"
	}
	if cfg.UserFilter == "" {
		if cfg.Provider == "freeipa" {
			cfg.UserFilter = "(objectClass=person)"
		} else {
			cfg.UserFilter = "(&(objectClass=user)(!(userAccountControl:1.2.840.113556.1.4.803:=2)))"
		}
	}
	if cfg.EmailAttr == "" {
		cfg.EmailAttr = "mail"
	}
	if cfg.NameAttr == "" {
		cfg.NameAttr = "displayName"
	}
	if cfg.UsernameAttr == "" {
		if cfg.Provider == "freeipa" {
			cfg.UsernameAttr = "uid"
		} else {
			cfg.UsernameAttr = "sAMAccountName"
		}
	}
	if cfg.GroupAttr == "" {
		cfg.GroupAttr = "memberOf"
	}
	_, err := s.DB.Exec(`
INSERT INTO tenant_ldap_settings (
  tenant_id, enabled, provider, server_url, bind_dn, bind_password, user_base_dn, user_filter,
  email_attr, name_attr, username_attr, group_attr, agent_group_dn, manager_group_dn, admin_group_dn,
  use_tls, start_tls, insecure_tls, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(tenant_id) DO UPDATE SET
  enabled=excluded.enabled, provider=excluded.provider, server_url=excluded.server_url,
  bind_dn=excluded.bind_dn, bind_password=excluded.bind_password, user_base_dn=excluded.user_base_dn,
  user_filter=excluded.user_filter, email_attr=excluded.email_attr, name_attr=excluded.name_attr,
  username_attr=excluded.username_attr, group_attr=excluded.group_attr,
  agent_group_dn=excluded.agent_group_dn, manager_group_dn=excluded.manager_group_dn,
  admin_group_dn=excluded.admin_group_dn, use_tls=excluded.use_tls, start_tls=excluded.start_tls,
  insecure_tls=excluded.insecure_tls, updated_at=excluded.updated_at
`, cfg.TenantID, b(cfg.Enabled), cfg.Provider, cfg.ServerURL, cfg.BindDN, cfg.BindPassword, cfg.UserBaseDN, cfg.UserFilter,
		cfg.EmailAttr, cfg.NameAttr, cfg.UsernameAttr, cfg.GroupAttr, cfg.AgentGroupDN, cfg.ManagerGroupDN, cfg.AdminGroupDN,
		b(cfg.UseTLS), b(cfg.StartTLS), b(cfg.InsecureTLS), now)
	return err
}

func (s *Store) ListMailboxes(tenantID string) ([]models.EmailMailbox, error) {
	rows, err := s.DB.Query(`
SELECT m.id, m.tenant_id, COALESCE(m.queue_id,''), m.name, m.from_email, m.smtp_host, m.smtp_port,
       m.smtp_username, m.smtp_password, m.smtp_use_tls, m.is_active, m.created_at, COALESCE(q.name,'')
FROM email_mailboxes m
LEFT JOIN queues q ON q.id = m.queue_id
WHERE m.tenant_id = ?
ORDER BY m.queue_id IS NOT NULL, m.name`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.EmailMailbox
	for rows.Next() {
		m, err := scanMailbox(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func scanMailbox(row rowScanner) (*models.EmailMailbox, error) {
	var m models.EmailMailbox
	var useTLS, active int
	var created string
	if err := row.Scan(
		&m.ID, &m.TenantID, &m.QueueID, &m.Name, &m.FromEmail, &m.SMTPHost, &m.SMTPPort,
		&m.SMTPUsername, &m.SMTPPassword, &useTLS, &active, &created, &m.QueueName,
	); err != nil {
		return nil, err
	}
	m.SMTPUseTLS = useTLS == 1
	m.IsActive = active == 1
	m.CreatedAt = parseTime(created)
	return &m, nil
}

func (s *Store) GetMailboxForTicket(tenantID string, queueID *string) (*models.EmailMailbox, error) {
	if queueID != nil && *queueID != "" {
		row := s.DB.QueryRow(`
SELECT m.id, m.tenant_id, COALESCE(m.queue_id,''), m.name, m.from_email, m.smtp_host, m.smtp_port,
       m.smtp_username, m.smtp_password, m.smtp_use_tls, m.is_active, m.created_at, COALESCE(q.name,'')
FROM email_mailboxes m
LEFT JOIN queues q ON q.id = m.queue_id
WHERE m.tenant_id = ? AND m.queue_id = ? AND m.is_active = 1
LIMIT 1`, tenantID, *queueID)
		if m, err := scanMailbox(row); err == nil {
			return m, nil
		}
	}
	row := s.DB.QueryRow(`
SELECT m.id, m.tenant_id, COALESCE(m.queue_id,''), m.name, m.from_email, m.smtp_host, m.smtp_port,
       m.smtp_username, m.smtp_password, m.smtp_use_tls, m.is_active, m.created_at, ''
FROM email_mailboxes m
WHERE m.tenant_id = ? AND m.queue_id IS NULL AND m.is_active = 1
LIMIT 1`, tenantID)
	return scanMailbox(row)
}

func (s *Store) UpsertMailbox(m models.EmailMailbox) error {
	b := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	if m.SMTPPort == 0 {
		m.SMTPPort = 587
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var queue any
	if m.QueueID != "" {
		queue = m.QueueID
	}
	if m.ID == "" {
		m.ID = uuid.NewString()
		_, err := s.DB.Exec(`
INSERT INTO email_mailboxes (id, tenant_id, queue_id, name, from_email, smtp_host, smtp_port, smtp_username, smtp_password, smtp_use_tls, is_active, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.ID, m.TenantID, queue, m.Name, m.FromEmail, m.SMTPHost, m.SMTPPort, m.SMTPUsername, m.SMTPPassword, b(m.SMTPUseTLS), b(m.IsActive), now)
		return err
	}
	_, err := s.DB.Exec(`
UPDATE email_mailboxes SET queue_id=?, name=?, from_email=?, smtp_host=?, smtp_port=?, smtp_username=?,
  smtp_password=CASE WHEN ? = '' THEN smtp_password ELSE ? END,
  smtp_use_tls=?, is_active=?
WHERE id=? AND tenant_id=?`,
		queue, m.Name, m.FromEmail, m.SMTPHost, m.SMTPPort, m.SMTPUsername,
		m.SMTPPassword, m.SMTPPassword,
		b(m.SMTPUseTLS), b(m.IsActive), m.ID, m.TenantID)
	return err
}

func (s *Store) DeleteMailbox(tenantID, id string) error {
	_, err := s.DB.Exec(`DELETE FROM email_mailboxes WHERE id = ? AND tenant_id = ?`, id, tenantID)
	return err
}

func (s *Store) GetUserByEmailInTenant(tenantID, email string) (*models.User, error) {
	row := s.DB.QueryRow(userSelect+` WHERE tenant_id = ? AND email = ?`, tenantID, strings.ToLower(email))
	return scanUser(row)
}

func (s *Store) GetUserByExternalID(tenantID, externalID string) (*models.User, error) {
	row := s.DB.QueryRow(userSelect+` WHERE tenant_id = ? AND external_id = ? AND external_id != ''`, tenantID, externalID)
	return scanUser(row)
}

func (s *Store) UpsertLDAPUser(tenantID, email, fullName, externalID string, role models.Role) (*models.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil, sql.ErrNoRows
	}
	if u, err := s.GetUserByExternalID(tenantID, externalID); err == nil && externalID != "" {
		_, _ = s.DB.Exec(`UPDATE users SET email=?, full_name=?, role=?, source='ldap', is_active=1 WHERE id=?`,
			email, fullName, role, u.ID)
		return s.GetUserByID(u.ID)
	}
	if u, err := s.GetUserByEmailInTenant(tenantID, email); err == nil {
		_, _ = s.DB.Exec(`UPDATE users SET full_name=?, role=?, source='ldap', external_id=?, is_active=1 WHERE id=?`,
			fullName, role, externalID, u.ID)
		return s.GetUserByID(u.ID)
	}
	u := &models.User{
		ID:         uuid.NewString(),
		TenantID:   tenantID,
		Email:      email,
		FullName:   fullName,
		Role:       role,
		Source:     models.UserSourceLDAP,
		ExternalID: externalID,
		IsActive:   true,
		CreatedAt:  time.Now().UTC(),
	}
	_, err := s.DB.Exec(
		`INSERT INTO users (id, tenant_id, email, full_name, password_hash, role, manager_id, source, external_id, is_active, created_at)
		 VALUES (?, ?, ?, ?, '', ?, NULL, 'ldap', ?, 1, ?)`,
		u.ID, u.TenantID, u.Email, u.FullName, u.Role, u.ExternalID, u.CreatedAt.Format(time.RFC3339),
	)
	return u, err
}

func (s *Store) GetTemplateByEvent(tenantID, event string) (*models.NotificationTemplate, error) {
	row := s.DB.QueryRow(`
SELECT id, tenant_id, event, subject, body, is_active, updated_at
FROM notification_templates WHERE tenant_id = ? AND event = ? AND is_active = 1`, tenantID, event)
	var t models.NotificationTemplate
	var active int
	var updated string
	if err := row.Scan(&t.ID, &t.TenantID, &t.Event, &t.Subject, &t.Body, &active, &updated); err != nil {
		return nil, err
	}
	t.IsActive = active == 1
	t.UpdatedAt = parseTime(updated)
	return &t, nil
}

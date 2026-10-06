package db

import (
	"database/sql"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rkislov/pomogayka/internal/models"
)

var nonDigitRe = regexp.MustCompile(`\D+`)

func NormalizePhone(phone string) string {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return ""
	}
	plus := strings.HasPrefix(phone, "+")
	digits := nonDigitRe.ReplaceAllString(phone, "")
	if digits == "" {
		return ""
	}
	// RU local 8xxxxxxxxxx → 7xxxxxxxxxx
	if len(digits) == 11 && digits[0] == '8' {
		digits = "7" + digits[1:]
	}
	if plus || len(digits) >= 11 {
		return digits
	}
	return digits
}

func (s *Store) GetSIPSettings(tenantID string) (*models.SIPSettings, error) {
	row := s.DB.QueryRow(`
SELECT tenant_id, enabled, websocket_url, sip_domain, outbound_proxy, stun_urls, turn_urls, turn_username, turn_password, updated_at
FROM tenant_sip_settings WHERE tenant_id = ?`, tenantID)
	var cfg models.SIPSettings
	var enabled int
	var updated string
	err := row.Scan(&cfg.TenantID, &enabled, &cfg.WebsocketURL, &cfg.SIPDomain, &cfg.OutboundProxy,
		&cfg.STUNURLs, &cfg.TURNURLs, &cfg.TURNUsername, &cfg.TURNPassword, &updated)
	if err == sql.ErrNoRows {
		return &models.SIPSettings{
			TenantID: tenantID,
			STUNURLs: "stun:stun.l.google.com:19302",
		}, nil
	}
	if err != nil {
		return nil, err
	}
	cfg.Enabled = enabled == 1
	cfg.UpdatedAt = parseTime(updated)
	return &cfg, nil
}

func (s *Store) SaveSIPSettings(cfg models.SIPSettings) error {
	b := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	if cfg.STUNURLs == "" {
		cfg.STUNURLs = "stun:stun.l.google.com:19302"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.DB.Exec(`
INSERT INTO tenant_sip_settings (
  tenant_id, enabled, websocket_url, sip_domain, outbound_proxy, stun_urls, turn_urls, turn_username, turn_password, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(tenant_id) DO UPDATE SET
  enabled=excluded.enabled, websocket_url=excluded.websocket_url, sip_domain=excluded.sip_domain,
  outbound_proxy=excluded.outbound_proxy, stun_urls=excluded.stun_urls, turn_urls=excluded.turn_urls,
  turn_username=excluded.turn_username, turn_password=excluded.turn_password, updated_at=excluded.updated_at
`, cfg.TenantID, b(cfg.Enabled), cfg.WebsocketURL, cfg.SIPDomain, cfg.OutboundProxy,
		cfg.STUNURLs, cfg.TURNURLs, cfg.TURNUsername, cfg.TURNPassword, now)
	return err
}

func (s *Store) GetUserSIPCredential(userID string) (*models.UserSIPCredential, error) {
	row := s.DB.QueryRow(`
SELECT user_id, tenant_id, extension, auth_username, password, display_name, auto_register, updated_at
FROM user_sip_credentials WHERE user_id = ?`, userID)
	var c models.UserSIPCredential
	var auto int
	var updated string
	err := row.Scan(&c.UserID, &c.TenantID, &c.Extension, &c.AuthUsername, &c.Password, &c.DisplayName, &auto, &updated)
	if err == sql.ErrNoRows {
		return &models.UserSIPCredential{UserID: userID}, nil
	}
	if err != nil {
		return nil, err
	}
	c.AutoRegister = auto == 1
	c.UpdatedAt = parseTime(updated)
	return &c, nil
}

func (s *Store) SaveUserSIPCredential(c models.UserSIPCredential) error {
	b := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	if c.AuthUsername == "" {
		c.AuthUsername = c.Extension
	}
	now := time.Now().UTC().Format(time.RFC3339)
	existing, _ := s.GetUserSIPCredential(c.UserID)
	if c.Password == "" && existing != nil {
		c.Password = existing.Password
	}
	_, err := s.DB.Exec(`
INSERT INTO user_sip_credentials (user_id, tenant_id, extension, auth_username, password, display_name, auto_register, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(user_id) DO UPDATE SET
  tenant_id=excluded.tenant_id, extension=excluded.extension, auth_username=excluded.auth_username,
  password=excluded.password, display_name=excluded.display_name, auto_register=excluded.auto_register,
  updated_at=excluded.updated_at
`, c.UserID, c.TenantID, c.Extension, c.AuthUsername, c.Password, c.DisplayName, b(c.AutoRegister), now)
	return err
}

func (s *Store) ListUserPhones(userID string) ([]models.UserPhone, error) {
	rows, err := s.DB.Query(`
SELECT id, tenant_id, user_id, phone, phone_normalized, label, source, is_primary, created_at, '', ''
FROM user_phones WHERE user_id = ? ORDER BY is_primary DESC, created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPhones(rows)
}

func (s *Store) ListClientsWithPhones(tenantID string) ([]models.User, error) {
	rows, err := s.DB.Query(userSelect+` WHERE tenant_id = ? AND role = 'client' AND is_active = 1 ORDER BY full_name`, tenantID)
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

func scanPhones(rows *sql.Rows) ([]models.UserPhone, error) {
	var out []models.UserPhone
	for rows.Next() {
		var p models.UserPhone
		var primary int
		var created, source string
		if err := rows.Scan(&p.ID, &p.TenantID, &p.UserID, &p.Phone, &p.PhoneNormalized, &p.Label, &source, &primary, &created, &p.UserName, &p.UserEmail); err != nil {
			return nil, err
		}
		p.Source = models.PhoneSource(source)
		p.IsPrimary = primary == 1
		p.CreatedAt = parseTime(created)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) FindUserByPhone(tenantID, phone string) (*models.User, error) {
	norm := NormalizePhone(phone)
	if norm == "" {
		return nil, sql.ErrNoRows
	}
	suffix := norm
	if len(suffix) > 10 {
		suffix = suffix[len(suffix)-10:]
	}
	rows, err := s.DB.Query(`
SELECT u.id, p.phone_normalized FROM user_phones p
JOIN users u ON u.id = p.user_id
WHERE p.tenant_id = ? AND u.is_active = 1`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var exactID, softID string
	for rows.Next() {
		var id, pn string
		if err := rows.Scan(&id, &pn); err != nil {
			return nil, err
		}
		if pn == norm {
			exactID = id
			break
		}
		if len(pn) >= 10 && pn[len(pn)-10:] == suffix {
			softID = id
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if exactID != "" {
		return s.GetUserByID(exactID)
	}
	if softID != "" {
		return s.GetUserByID(softID)
	}
	return nil, sql.ErrNoRows
}

func (s *Store) AddUserPhone(tenantID, userID, phone, label string, source models.PhoneSource, primary bool) (*models.UserPhone, error) {
	phone = strings.TrimSpace(phone)
	norm := NormalizePhone(phone)
	if norm == "" {
		return nil, sql.ErrNoRows
	}
	if source == "" {
		source = models.PhoneSourceManual
	}
	if primary {
		_, _ = s.DB.Exec(`UPDATE user_phones SET is_primary = 0 WHERE user_id = ?`, userID)
	}
	p := &models.UserPhone{
		ID:              uuid.NewString(),
		TenantID:        tenantID,
		UserID:          userID,
		Phone:           phone,
		PhoneNormalized: norm,
		Label:           label,
		Source:          source,
		IsPrimary:       primary,
		CreatedAt:       time.Now().UTC(),
	}
	pi := 0
	if primary {
		pi = 1
	}
	_, err := s.DB.Exec(`
INSERT INTO user_phones (id, tenant_id, user_id, phone, phone_normalized, label, source, is_primary, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.TenantID, p.UserID, p.Phone, p.PhoneNormalized, p.Label, p.Source, pi, p.CreatedAt.Format(time.RFC3339))
	return p, err
}

func (s *Store) DeleteUserPhone(tenantID, phoneID string) error {
	_, err := s.DB.Exec(`DELETE FROM user_phones WHERE id = ? AND tenant_id = ?`, phoneID, tenantID)
	return err
}

func (s *Store) ReplaceLDAPPhones(tenantID, userID string, phones []string) error {
	_, err := s.DB.Exec(`DELETE FROM user_phones WHERE user_id = ? AND source = 'ldap'`, userID)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for i, ph := range phones {
		norm := NormalizePhone(ph)
		if norm == "" || seen[norm] {
			continue
		}
		seen[norm] = true
		// skip if manual already owns this number
		var n int
		_ = s.DB.QueryRow(`SELECT COUNT(*) FROM user_phones WHERE tenant_id = ? AND phone_normalized = ?`, tenantID, norm).Scan(&n)
		if n > 0 {
			continue
		}
		_, err := s.AddUserPhone(tenantID, userID, strings.TrimSpace(ph), "LDAP", models.PhoneSourceLDAP, i == 0)
		if err != nil {
			return err
		}
	}
	return nil
}

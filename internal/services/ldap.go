package services

import (
	"crypto/tls"
	"fmt"
	"strings"

	ldap "github.com/go-ldap/ldap/v3"

	"github.com/rkislov/pomogayka/internal/models"
)

type LDAPUser struct {
	Email      string
	FullName   string
	ExternalID string
	Groups     []string
	Role       models.Role
}

func AuthenticateLDAP(cfg *models.LDAPSettings, login, password string) (*LDAPUser, error) {
	if cfg == nil || !cfg.IsConfigured() {
		return nil, fmt.Errorf("ldap disabled")
	}
	if strings.TrimSpace(login) == "" || password == "" {
		return nil, fmt.Errorf("empty credentials")
	}

	conn, err := dialLDAP(cfg)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
		return nil, fmt.Errorf("ldap service bind: %w", err)
	}

	filter := buildUserFilter(cfg, login)
	attrs := uniqueNonEmpty([]string{
		cfg.EmailAttr, cfg.NameAttr, cfg.UsernameAttr, cfg.GroupAttr, "dn", "entryUUID", "objectGUID", "uid",
	})
	req := ldap.NewSearchRequest(
		cfg.UserBaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, 0, false,
		filter,
		attrs,
		nil,
	)
	res, err := conn.Search(req)
	if err != nil {
		return nil, fmt.Errorf("ldap search: %w", err)
	}
	if len(res.Entries) == 0 {
		return nil, fmt.Errorf("user not found in ldap")
	}
	entry := res.Entries[0]

	userConn, err := dialLDAP(cfg)
	if err != nil {
		return nil, err
	}
	defer userConn.Close()
	if err := userConn.Bind(entry.DN, password); err != nil {
		return nil, fmt.Errorf("invalid ldap credentials")
	}

	email := firstAttr(entry, cfg.EmailAttr)
	if email == "" && strings.Contains(login, "@") {
		email = strings.ToLower(login)
	}
	name := firstAttr(entry, cfg.NameAttr)
	if name == "" {
		name = firstAttr(entry, cfg.UsernameAttr)
	}
	if name == "" {
		name = email
	}
	groups := entry.GetAttributeValues(cfg.GroupAttr)
	ext := firstAttr(entry, "entryUUID")
	if ext == "" {
		ext = entry.DN
	}
	role := roleFromGroups(groups, cfg)
	return &LDAPUser{
		Email:      strings.ToLower(strings.TrimSpace(email)),
		FullName:   strings.TrimSpace(name),
		ExternalID: ext,
		Groups:     groups,
		Role:       role,
	}, nil
}

func TestLDAP(cfg *models.LDAPSettings) error {
	if cfg == nil || cfg.ServerURL == "" {
		return fmt.Errorf("server url required")
	}
	conn, err := dialLDAP(cfg)
	if err != nil {
		return err
	}
	defer conn.Close()
	if cfg.BindDN != "" {
		if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
			return fmt.Errorf("bind failed: %w", err)
		}
	}
	return nil
}

func dialLDAP(cfg *models.LDAPSettings) (*ldap.Conn, error) {
	url := strings.TrimSpace(cfg.ServerURL)
	tlsConfig := &tls.Config{InsecureSkipVerify: cfg.InsecureTLS} //nolint:gosec
	var conn *ldap.Conn
	var err error
	if strings.HasPrefix(strings.ToLower(url), "ldaps://") || cfg.UseTLS {
		conn, err = ldap.DialURL(url, ldap.DialWithTLSConfig(tlsConfig))
	} else {
		conn, err = ldap.DialURL(url)
	}
	if err != nil {
		return nil, fmt.Errorf("ldap dial: %w", err)
	}
	if cfg.StartTLS && !strings.HasPrefix(strings.ToLower(url), "ldaps://") {
		if err := conn.StartTLS(tlsConfig); err != nil {
			conn.Close()
			return nil, fmt.Errorf("starttls: %w", err)
		}
	}
	return conn, nil
}

func buildUserFilter(cfg *models.LDAPSettings, login string) string {
	login = ldap.EscapeFilter(strings.TrimSpace(login))
	base := strings.TrimSpace(cfg.UserFilter)
	if base == "" {
		base = "(objectClass=*)"
	}
	attr := cfg.UsernameAttr
	if attr == "" {
		attr = "uid"
	}
	orParts := []string{fmt.Sprintf("(%s=%s)", attr, login)}
	if strings.Contains(login, "@") {
		emailAttr := cfg.EmailAttr
		if emailAttr == "" {
			emailAttr = "mail"
		}
		orParts = append(orParts, fmt.Sprintf("(%s=%s)", emailAttr, login))
	} else if cfg.Provider == "ad" || cfg.Provider == "" {
		orParts = append(orParts, fmt.Sprintf("(userPrincipalName=%s)", login))
	}
	userClause := "(|" + strings.Join(orParts, "") + ")"
	if strings.HasPrefix(base, "(") {
		return "(&" + base + userClause + ")"
	}
	return "(&(" + base + ")" + userClause + ")"
}

func roleFromGroups(groups []string, cfg *models.LDAPSettings) models.Role {
	norm := map[string]struct{}{}
	for _, g := range groups {
		norm[strings.ToLower(g)] = struct{}{}
	}
	has := func(dn string) bool {
		dn = strings.ToLower(strings.TrimSpace(dn))
		if dn == "" {
			return false
		}
		_, ok := norm[dn]
		return ok
	}
	if has(cfg.AdminGroupDN) {
		return models.RoleAdmin
	}
	if has(cfg.ManagerGroupDN) {
		return models.RoleManager
	}
	if has(cfg.AgentGroupDN) {
		return models.RoleAgent
	}
	return models.RoleClient
}

func firstAttr(e *ldap.Entry, name string) string {
	if name == "" {
		return ""
	}
	return strings.TrimSpace(e.GetAttributeValue(name))
}

func uniqueNonEmpty(items []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, i := range items {
		i = strings.TrimSpace(i)
		if i == "" {
			continue
		}
		if _, ok := seen[i]; ok {
			continue
		}
		seen[i] = struct{}{}
		out = append(out, i)
	}
	return out
}

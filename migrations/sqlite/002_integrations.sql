CREATE TABLE IF NOT EXISTS tenant_domains (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    host TEXT NOT NULL UNIQUE,
    is_primary INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tenant_domains_tenant ON tenant_domains(tenant_id);

CREATE TABLE IF NOT EXISTS tenant_ldap_settings (
    tenant_id TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    enabled INTEGER NOT NULL DEFAULT 0,
    provider TEXT NOT NULL DEFAULT 'ad',
    server_url TEXT NOT NULL DEFAULT '',
    bind_dn TEXT NOT NULL DEFAULT '',
    bind_password TEXT NOT NULL DEFAULT '',
    user_base_dn TEXT NOT NULL DEFAULT '',
    user_filter TEXT NOT NULL DEFAULT '(&(objectClass=user)(!(userAccountControl:1.2.840.113556.1.4.803:=2)))',
    email_attr TEXT NOT NULL DEFAULT 'mail',
    name_attr TEXT NOT NULL DEFAULT 'displayName',
    username_attr TEXT NOT NULL DEFAULT 'sAMAccountName',
    phone_attr TEXT NOT NULL DEFAULT 'telephoneNumber',
    group_attr TEXT NOT NULL DEFAULT 'memberOf',
    agent_group_dn TEXT NOT NULL DEFAULT '',
    manager_group_dn TEXT NOT NULL DEFAULT '',
    admin_group_dn TEXT NOT NULL DEFAULT '',
    use_tls INTEGER NOT NULL DEFAULT 1,
    start_tls INTEGER NOT NULL DEFAULT 0,
    insecure_tls INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS email_mailboxes (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    queue_id TEXT REFERENCES queues(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    from_email TEXT NOT NULL DEFAULT '',
    smtp_host TEXT NOT NULL DEFAULT '',
    smtp_port INTEGER NOT NULL DEFAULT 587,
    smtp_username TEXT NOT NULL DEFAULT '',
    smtp_password TEXT NOT NULL DEFAULT '',
    smtp_use_tls INTEGER NOT NULL DEFAULT 1,
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_mailbox_tenant_default ON email_mailboxes(tenant_id) WHERE queue_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_mailbox_queue ON email_mailboxes(queue_id) WHERE queue_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_mailbox_tenant ON email_mailboxes(tenant_id);

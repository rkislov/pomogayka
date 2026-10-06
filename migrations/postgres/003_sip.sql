CREATE TABLE IF NOT EXISTS tenant_sip_settings (
    tenant_id TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    enabled INTEGER NOT NULL DEFAULT 0,
    websocket_url TEXT NOT NULL DEFAULT '',
    sip_domain TEXT NOT NULL DEFAULT '',
    outbound_proxy TEXT NOT NULL DEFAULT '',
    stun_urls TEXT NOT NULL DEFAULT 'stun:stun.l.google.com:19302',
    turn_urls TEXT NOT NULL DEFAULT '',
    turn_username TEXT NOT NULL DEFAULT '',
    turn_password TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS user_sip_credentials (
    user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    extension TEXT NOT NULL DEFAULT '',
    auth_username TEXT NOT NULL DEFAULT '',
    password TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    auto_register INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_user_sip_tenant ON user_sip_credentials(tenant_id);

CREATE TABLE IF NOT EXISTS user_phones (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    phone TEXT NOT NULL,
    phone_normalized TEXT NOT NULL,
    label TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT 'manual',
    is_primary INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    UNIQUE(tenant_id, phone_normalized)
);

CREATE INDEX IF NOT EXISTS idx_user_phones_user ON user_phones(user_id);
CREATE INDEX IF NOT EXISTS idx_user_phones_norm ON user_phones(tenant_id, phone_normalized);

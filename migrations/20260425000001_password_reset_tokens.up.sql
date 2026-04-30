-- TBL-54 FIX-228 — Single-use password reset tokens (DEV-323).
-- Platform-global (no tenant_id) — reset flow is per-user authentication, not tenant-scoped.

CREATE TABLE password_reset_tokens (
  id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash      BYTEA        NOT NULL UNIQUE,
  email_rate_key  TEXT         NOT NULL,
  expires_at      TIMESTAMPTZ  NOT NULL,
  created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_password_reset_tokens_email_rate ON password_reset_tokens (email_rate_key, created_at DESC);
CREATE INDEX idx_password_reset_tokens_expires_at ON password_reset_tokens (expires_at);
CREATE INDEX idx_password_reset_tokens_user_id    ON password_reset_tokens (user_id);

COMMENT ON TABLE password_reset_tokens IS 'TBL-54 FIX-228: single-use password reset tokens (DEV-323: opaque SHA-256-hashed; AC-5 of FIX-228).';

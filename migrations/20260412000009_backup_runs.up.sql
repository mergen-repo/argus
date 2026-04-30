CREATE TABLE IF NOT EXISTS backup_runs (
    id              BIGSERIAL PRIMARY KEY,
    kind            VARCHAR(20)  NOT NULL,
    state           VARCHAR(20)  NOT NULL,
    s3_bucket       VARCHAR(200) NOT NULL,
    s3_key          VARCHAR(500) NOT NULL,
    size_bytes      BIGINT       NOT NULL DEFAULT 0,
    sha256          VARCHAR(64),
    started_at      TIMESTAMPTZ  NOT NULL,
    finished_at     TIMESTAMPTZ,
    duration_seconds INTEGER,
    error_message   TEXT,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_backup_runs_kind_time ON backup_runs (kind, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_backup_runs_state     ON backup_runs (state);

CREATE TABLE IF NOT EXISTS backup_verifications (
    id            BIGSERIAL PRIMARY KEY,
    backup_run_id BIGINT      NOT NULL REFERENCES backup_runs(id) ON DELETE CASCADE,
    state         VARCHAR(20) NOT NULL,
    tenants_count BIGINT,
    sims_count    BIGINT,
    error_message TEXT,
    verified_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_backup_verifications_run ON backup_verifications (backup_run_id);

-- Idempotency gate for deployments that predate it. Creates an empty table and
-- touches nothing else: no lock on transactions, no change to existing rows.
-- Apply before deploying the service.
--
--   psql "$PG_URL" -v ON_ERROR_STOP=1 -f migrations/0002_requests.sql

CREATE TABLE IF NOT EXISTS requests (
    request_id UUID PRIMARY KEY,
    operation VARCHAR(20) NOT NULL,
    payload_fingerprint TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

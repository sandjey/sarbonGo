-- Revert to partial unique index shape from initial calls migration.
DROP INDEX IF EXISTS idx_calls_client_request_id_uq;

CREATE UNIQUE INDEX IF NOT EXISTS idx_calls_client_request_id
ON calls (caller_id, client_request_id)
WHERE client_request_id IS NOT NULL;


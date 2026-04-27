-- Fix calls idempotency index so ON CONFLICT (caller_id, client_request_id) works.
-- Old schema used a partial unique index; PostgreSQL can't infer it for this ON CONFLICT target.

-- 1) De-duplicate existing non-null client_request_id values per caller.
WITH ranked AS (
  SELECT id,
         ROW_NUMBER() OVER (
           PARTITION BY caller_id, client_request_id
           ORDER BY created_at ASC, id ASC
         ) AS rn
  FROM calls
  WHERE client_request_id IS NOT NULL
)
UPDATE calls c
SET client_request_id = NULL
FROM ranked r
WHERE c.id = r.id
  AND r.rn > 1;

-- 2) Replace partial index with full unique index.
DROP INDEX IF EXISTS idx_calls_client_request_id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_calls_client_request_id_uq
ON calls (caller_id, client_request_id);


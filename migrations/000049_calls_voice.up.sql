-- Voice calls (1:1) signaling state in DB.
-- Transport (audio) is WebRTC P2P/TURN; this is only session/state storage + audit.

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'call_status') THEN
    CREATE TYPE call_status AS ENUM ('RINGING', 'ACTIVE', 'ENDED', 'DECLINED', 'MISSED', 'CANCELLED', 'FAILED');
  END IF;
END$$;

CREATE TABLE IF NOT EXISTS calls (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  conversation_id UUID NULL REFERENCES chat_conversations(id) ON DELETE SET NULL,
  caller_id UUID NOT NULL,
  callee_id UUID NOT NULL,
  status call_status NOT NULL DEFAULT 'RINGING',
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  started_at TIMESTAMP NULL,
  ended_at TIMESTAMP NULL,
  ended_by UUID NULL,
  ended_reason VARCHAR(50) NULL,
  client_request_id VARCHAR(64) NULL,
  CONSTRAINT calls_not_same_user CHECK (caller_id <> callee_id)
);

CREATE INDEX IF NOT EXISTS idx_calls_caller_created ON calls (caller_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_calls_callee_created ON calls (callee_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_calls_status_created ON calls (status, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_calls_client_request_id ON calls (caller_id, client_request_id) WHERE client_request_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS call_events (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  call_id UUID NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
  actor_id UUID NULL,
  event_type VARCHAR(50) NOT NULL,
  payload JSONB NULL,
  created_at TIMESTAMP NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_call_events_call_created ON call_events (call_id, created_at DESC);


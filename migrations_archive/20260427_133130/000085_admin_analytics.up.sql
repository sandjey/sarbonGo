CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE IF NOT EXISTS analytics_events (
  event_id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  event_name VARCHAR(64) NOT NULL,
  event_time_utc TIMESTAMPTZ NOT NULL DEFAULT now(),
  session_id VARCHAR(128) NULL,
  user_id UUID NULL,
  role VARCHAR(32) NOT NULL,
  entity_type VARCHAR(64) NULL,
  entity_id UUID NULL,
  actor_id UUID NULL,
  device_type VARCHAR(32) NULL,
  platform TEXT NULL,
  ip_hash VARCHAR(128) NULL,
  geo_city VARCHAR(128) NULL,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_analytics_events_time ON analytics_events (event_time_utc DESC);
CREATE INDEX IF NOT EXISTS idx_analytics_events_name_time ON analytics_events (event_name, event_time_utc DESC);
CREATE INDEX IF NOT EXISTS idx_analytics_events_user_time ON analytics_events (user_id, event_time_utc DESC);
CREATE INDEX IF NOT EXISTS idx_analytics_events_role_time ON analytics_events (role, event_time_utc DESC);
CREATE INDEX IF NOT EXISTS idx_analytics_events_entity_time ON analytics_events (entity_type, entity_id, event_time_utc DESC);
CREATE INDEX IF NOT EXISTS idx_analytics_events_geo_time ON analytics_events (geo_city, event_time_utc DESC);

CREATE TABLE IF NOT EXISTS sessions (
  session_id VARCHAR(128) PRIMARY KEY,
  user_id UUID NOT NULL,
  role VARCHAR(32) NOT NULL,
  started_at_utc TIMESTAMPTZ NOT NULL,
  ended_at_utc TIMESTAMPTZ NULL,
  last_seen_at_utc TIMESTAMPTZ NULL,
  duration_seconds BIGINT NOT NULL DEFAULT 0,
  device_type VARCHAR(32) NULL,
  platform TEXT NULL,
  ip_hash VARCHAR(128) NULL,
  geo_city VARCHAR(128) NULL,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_started ON sessions (user_id, started_at_utc DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_role_started ON sessions (role, started_at_utc DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_geo_started ON sessions (geo_city, started_at_utc DESC);

CREATE TABLE IF NOT EXISTS user_login_stats (
  user_id UUID NOT NULL,
  role VARCHAR(32) NOT NULL,
  total_logins BIGINT NOT NULL DEFAULT 0,
  successful_logins BIGINT NOT NULL DEFAULT 0,
  failed_logins BIGINT NOT NULL DEFAULT 0,
  last_login_at_utc TIMESTAMPTZ NULL,
  total_session_duration_seconds BIGINT NOT NULL DEFAULT 0,
  completed_sessions_count BIGINT NOT NULL DEFAULT 0,
  avg_session_duration_seconds DOUBLE PRECISION NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, role)
);

CREATE TABLE IF NOT EXISTS cargo_stats (
  day_utc DATE NOT NULL,
  role VARCHAR(32) NOT NULL,
  user_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
  created_count BIGINT NOT NULL DEFAULT 0,
  completed_count BIGINT NOT NULL DEFAULT 0,
  cancelled_count BIGINT NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (day_utc, role, user_id)
);

CREATE TABLE IF NOT EXISTS offer_stats (
  day_utc DATE NOT NULL,
  role VARCHAR(32) NOT NULL,
  user_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
  created_count BIGINT NOT NULL DEFAULT 0,
  accepted_count BIGINT NOT NULL DEFAULT 0,
  rejected_count BIGINT NOT NULL DEFAULT 0,
  canceled_count BIGINT NOT NULL DEFAULT 0,
  waiting_driver_confirm_count BIGINT NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (day_utc, role, user_id)
);

CREATE TABLE IF NOT EXISTS trip_stats (
  day_utc DATE NOT NULL,
  role VARCHAR(32) NOT NULL,
  user_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
  started_count BIGINT NOT NULL DEFAULT 0,
  completed_count BIGINT NOT NULL DEFAULT 0,
  cancelled_count BIGINT NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (day_utc, role, user_id)
);

CREATE TABLE IF NOT EXISTS geo_stats (
  day_utc DATE NOT NULL,
  geo_city VARCHAR(128) NOT NULL,
  role VARCHAR(32) NOT NULL,
  user_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
  event_count BIGINT NOT NULL DEFAULT 0,
  login_count BIGINT NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (day_utc, geo_city, role, user_id)
);

CREATE INDEX IF NOT EXISTS idx_geo_stats_day_city ON geo_stats (day_utc DESC, geo_city);

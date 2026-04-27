-- Trip user notifications (driver / dispatcher) and per-trip ratings (1–10 stars).

CREATE TABLE IF NOT EXISTS trip_user_notifications (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  trip_id UUID NOT NULL,
  recipient_kind VARCHAR(16) NOT NULL CHECK (recipient_kind IN ('driver', 'dispatcher')),
  recipient_id UUID NOT NULL,
  event_kind VARCHAR(64) NOT NULL,
  from_status VARCHAR(32),
  to_status VARCHAR(32),
  read_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_tun_recipient_created ON trip_user_notifications (recipient_kind, recipient_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tun_trip ON trip_user_notifications (trip_id);
CREATE INDEX IF NOT EXISTS idx_tun_unread ON trip_user_notifications (recipient_kind, recipient_id) WHERE read_at IS NULL;

CREATE TABLE IF NOT EXISTS trip_ratings (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  trip_id UUID NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
  rater_kind VARCHAR(16) NOT NULL CHECK (rater_kind IN ('driver', 'dispatcher')),
  rater_id UUID NOT NULL,
  ratee_kind VARCHAR(16) NOT NULL CHECK (ratee_kind IN ('driver', 'dispatcher')),
  ratee_id UUID NOT NULL,
  stars SMALLINT NOT NULL CHECK (stars >= 1 AND stars <= 10),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT trip_ratings_trip_rater_unique UNIQUE (trip_id, rater_kind)
);

CREATE INDEX IF NOT EXISTS idx_trip_ratings_ratee ON trip_ratings (ratee_kind, ratee_id);

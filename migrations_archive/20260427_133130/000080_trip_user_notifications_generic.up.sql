-- Extend trip_user_notifications to store non-trip events as well
-- (cargo_offer, connection_offer, message, call, driver_profile_edit, ...).
-- trip_id becomes nullable; event_type classifies the notification for mobile clients;
-- payload captures the original SSE/WS JSON envelope for richer UI rendering.

ALTER TABLE trip_user_notifications
    ALTER COLUMN trip_id DROP NOT NULL;

ALTER TABLE trip_user_notifications
    ADD COLUMN IF NOT EXISTS event_type VARCHAR(32);

ALTER TABLE trip_user_notifications
    ADD COLUMN IF NOT EXISTS payload JSONB;

CREATE INDEX IF NOT EXISTS idx_tun_recipient_type_created
    ON trip_user_notifications (recipient_kind, recipient_id, event_type, created_at DESC);

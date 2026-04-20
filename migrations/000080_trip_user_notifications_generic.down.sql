DROP INDEX IF EXISTS idx_tun_recipient_type_created;

ALTER TABLE trip_user_notifications DROP COLUMN IF EXISTS payload;
ALTER TABLE trip_user_notifications DROP COLUMN IF EXISTS event_type;

DELETE FROM trip_user_notifications WHERE trip_id IS NULL;

ALTER TABLE trip_user_notifications
    ALTER COLUMN trip_id SET NOT NULL;

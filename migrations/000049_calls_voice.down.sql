DROP TABLE IF EXISTS call_events;
DROP TABLE IF EXISTS calls;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_type WHERE typname = 'call_status') THEN
    DROP TYPE call_status;
  END IF;
END$$;


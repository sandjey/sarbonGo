-- Freelance dispatcher business role: CARGO_MANAGER | DRIVER_MANAGER (set at registration).
ALTER TABLE freelance_dispatchers ADD COLUMN IF NOT EXISTS manager_role VARCHAR(32) NULL;
ALTER TABLE deleted_freelance_dispatchers ADD COLUMN IF NOT EXISTS manager_role VARCHAR(32) NULL;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_freelance_dispatchers_manager_role') THEN
    ALTER TABLE freelance_dispatchers
      ADD CONSTRAINT chk_freelance_dispatchers_manager_role
      CHECK (manager_role IS NULL OR manager_role IN ('CARGO_MANAGER', 'DRIVER_MANAGER'));
  END IF;
END $$;

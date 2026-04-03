ALTER TABLE freelance_dispatchers DROP CONSTRAINT IF EXISTS chk_freelance_dispatchers_manager_role;
ALTER TABLE freelance_dispatchers DROP COLUMN IF EXISTS manager_role;
ALTER TABLE deleted_freelance_dispatchers DROP COLUMN IF EXISTS manager_role;

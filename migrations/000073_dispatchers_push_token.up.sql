ALTER TABLE freelance_dispatchers
  ADD COLUMN IF NOT EXISTS push_token VARCHAR NULL;

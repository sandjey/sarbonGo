DO $$
BEGIN
  -- Revert companies.owner_dispatcher_id FK to original behavior (NO ACTION)
  IF EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conname = 'fk_companies_owner_dispatcher'
  ) THEN
    ALTER TABLE companies DROP CONSTRAINT fk_companies_owner_dispatcher;
  END IF;
  ALTER TABLE companies
    ADD CONSTRAINT fk_companies_owner_dispatcher
    FOREIGN KEY (owner_dispatcher_id)
    REFERENCES freelance_dispatchers(id);

  -- Revert driver_invitations.invited_by_dispatcher_id FK to original behavior (NO ACTION)
  IF EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conname = 'fk_driver_invitations_dispatcher'
  ) THEN
    ALTER TABLE driver_invitations DROP CONSTRAINT fk_driver_invitations_dispatcher;
  END IF;
  ALTER TABLE driver_invitations
    ADD CONSTRAINT fk_driver_invitations_dispatcher
    FOREIGN KEY (invited_by_dispatcher_id)
    REFERENCES freelance_dispatchers(id);
END$$;


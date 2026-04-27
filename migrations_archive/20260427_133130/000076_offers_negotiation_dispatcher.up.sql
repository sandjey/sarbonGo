-- Dispatcher counterpart during WAITING_DRIVER_CONFIRM (DM who proposed DRIVER_MANAGER offer, or DM who accepted CM DISPATCHER offer).
ALTER TABLE offers ADD COLUMN IF NOT EXISTS negotiation_dispatcher_id UUID NULL;

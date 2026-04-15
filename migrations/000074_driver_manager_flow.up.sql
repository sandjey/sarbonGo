-- Таблица связей водителей и Driver Managers (Многие-ко-Многим)
CREATE TABLE IF NOT EXISTS driver_manager_relations (
    driver_id UUID NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
    manager_id UUID NOT NULL REFERENCES freelance_dispatchers(id) ON DELETE CASCADE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    PRIMARY KEY (driver_id, manager_id)
);

CREATE INDEX IF NOT EXISTS idx_driver_manager_relations_manager ON driver_manager_relations(manager_id);
CREATE INDEX IF NOT EXISTS idx_driver_manager_relations_driver ON driver_manager_relations(driver_id);

-- Расширяем статусы офферов для трехстороннего согласования
ALTER TABLE offers DROP CONSTRAINT IF EXISTS offers_status_check;
ALTER TABLE offers ADD CONSTRAINT offers_status_check CHECK (status IN ('PENDING', 'ACCEPTED', 'REJECTED', 'WAITING_DRIVER_CONFIRM'));

-- Добавляем ID того, кто предложил (для DRIVER_MANAGER)
ALTER TABLE offers ADD COLUMN IF NOT EXISTS proposed_by_id UUID;

-- Переносим старые связи (один-к-одному) в новую таблицу
INSERT INTO driver_manager_relations (driver_id, manager_id)
SELECT id, freelancer_id FROM drivers WHERE freelancer_id IS NOT NULL
ON CONFLICT (driver_id, manager_id) DO NOTHING;

-- Добавление расширенной системы рейтингов для Driver Manager Flow
ALTER TABLE trips 
ADD COLUMN IF NOT EXISTS rating_driver_to_dm INTEGER CHECK (rating_driver_to_dm BETWEEN 1 AND 5),
ADD COLUMN IF NOT EXISTS rating_dm_to_driver INTEGER CHECK (rating_dm_to_driver BETWEEN 1 AND 5),
ADD COLUMN IF NOT EXISTS rating_dm_to_cm INTEGER CHECK (rating_dm_to_cm BETWEEN 1 AND 5),
ADD COLUMN IF NOT EXISTS rating_cm_to_dm INTEGER CHECK (rating_cm_to_dm BETWEEN 1 AND 5);

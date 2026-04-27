-- Ранее при отмене в archived_trips копировался последний операционный status (например IN_PROGRESS).
UPDATE archived_trips
SET status = 'CANCELLED'
WHERE cancel_reason = 'CANCELLED'
  AND (status IS DISTINCT FROM 'CANCELLED');

ALTER TABLE transfers
DROP CONSTRAINT transfers_status_check,
ADD CONSTRAINT transfers_status_check
CHECK (status IN ('PENDING', 'COMPLETED', 'FAILED', 'REVERSED'));

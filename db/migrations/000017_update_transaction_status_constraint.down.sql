-- Revert transaction status check constraint to lowercase values
ALTER TABLE transactions DROP CONSTRAINT transactions_status_check;
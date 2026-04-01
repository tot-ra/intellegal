ALTER TABLE contracts
    DROP CONSTRAINT IF EXISTS contracts_language_check;

ALTER TABLE contracts
    DROP COLUMN IF EXISTS language;

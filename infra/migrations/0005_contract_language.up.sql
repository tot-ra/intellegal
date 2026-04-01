ALTER TABLE contracts
    ADD COLUMN IF NOT EXISTS language TEXT NOT NULL DEFAULT 'eng';

UPDATE contracts
SET language = 'eng'
WHERE COALESCE(BTRIM(language), '') = '';

ALTER TABLE contracts
    DROP CONSTRAINT IF EXISTS contracts_language_check;

ALTER TABLE contracts
    ADD CONSTRAINT contracts_language_check
    CHECK (language IN ('eng', 'est', 'rus'));

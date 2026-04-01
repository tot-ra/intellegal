CREATE TABLE IF NOT EXISTS document_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    version_label TEXT,
    checksum TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_document_versions_document_checksum
    ON document_versions (document_id, checksum);

CREATE INDEX IF NOT EXISTS idx_document_versions_document_id
    ON document_versions (document_id);

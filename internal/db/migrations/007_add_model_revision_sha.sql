ALTER TABLE models
    ADD COLUMN revision_sha TEXT;

CREATE UNIQUE INDEX models_revision_sha_unique ON models (revision_sha) WHERE revision_sha IS NOT NULL;

COMMENT ON COLUMN models.revision_sha IS 'Immutable upstream commit digest for this revision, used to deduplicate ingestion regardless of whether the mutable revision ref (e.g. "main") has since moved';

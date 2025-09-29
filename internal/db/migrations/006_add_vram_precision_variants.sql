ALTER TABLE models
    ADD COLUMN vram_fp8_bytes BIGINT,
    ADD COLUMN vram_int4_bytes BIGINT,
    ADD COLUMN kv_cache_bytes BIGINT;

COMMENT ON COLUMN models.vram_estimate_bytes IS 'FP16 total VRAM estimate (weights + KV cache)';
COMMENT ON COLUMN models.vram_fp8_bytes IS 'FP8 total VRAM estimate (weights + KV cache)';
COMMENT ON COLUMN models.vram_int4_bytes IS 'INT4 total VRAM estimate (weights + KV cache)';
COMMENT ON COLUMN models.kv_cache_bytes IS 'KV cache estimate shared across all precision variants';

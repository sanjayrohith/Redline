package inference

import (
	"errors"
	"fmt"

	"github.com/sanjayrohith/redline/internal/ingest"
)

// KVCacheBlockSize is vLLM's --block-size: the number of tokens each
// PagedAttention block holds. 16 is the well-tested default across model
// families - it partitions the sequence dimension, not the head
// dimension, so it does not need to vary with model geometry the way
// tensor-parallel degree or max-model-len do.
const KVCacheBlockSize = 16

// ErrKVCacheBudgetTooSmall means the KV cache memory budget cannot hold
// even a single PagedAttention block at this model's geometry.
var ErrKVCacheBudgetTooSmall = errors.New("inference: kv cache budget too small for a single block")

// KVCacheConfig is vLLM's PagedAttention KV cache sizing.
type KVCacheConfig struct {
	// BlockSize is --block-size.
	BlockSize int
	// NumGPUBlocks is --num-gpu-blocks-override: preallocating an exact
	// block count computed from the real memory budget, rather than
	// leaving it to vLLM's own profiling pass to guess, is what keeps
	// the cache from being either fragmented (undersized blocks driving
	// excessive block-table overhead) or over-reserved (vLLM's profiler
	// erring conservative and leaving VRAM idle that this deployment's
	// actual budget could have used for more concurrent sequences).
	NumGPUBlocks int
}

// RenderKVCacheConfig sizes the KV cache in whole PagedAttention blocks
// from budgetBytes - the VRAM left over on the target GPU(s) after model
// weights (see RenderEngineArgs' tensor-parallel sizing) - and the
// model's attention geometry.
func RenderKVCacheConfig(geometry ingest.ModelGeometry, budgetBytes int64) (KVCacheConfig, error) {
	bytesPerToken := ingest.KVCacheBytesPerToken(geometry)
	if bytesPerToken <= 0 {
		return KVCacheConfig{}, fmt.Errorf("inference: invalid model geometry %+v for kv cache sizing", geometry)
	}

	bytesPerBlock := bytesPerToken * int64(KVCacheBlockSize)
	numBlocks := budgetBytes / bytesPerBlock
	if numBlocks <= 0 {
		return KVCacheConfig{}, fmt.Errorf("%w: budget %d bytes, one block needs %d bytes",
			ErrKVCacheBudgetTooSmall, budgetBytes, bytesPerBlock)
	}

	return KVCacheConfig{
		BlockSize:    KVCacheBlockSize,
		NumGPUBlocks: int(numBlocks),
	}, nil
}

package lora_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/sanjayrohith/redline/internal/lora"
)

func TestRouter_ResolvesBaseModelDirectly(t *testing.T) {
	router := lora.NewRouter("llama-2-7b")

	got, err := router.Resolve("llama-2-7b")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != "llama-2-7b" {
		t.Errorf("Resolve() = %q, want llama-2-7b", got)
	}
}

func TestRouter_ResolvesLoadedAdapter(t *testing.T) {
	router := lora.NewRouter("llama-2-7b")
	router.MarkLoaded("legal-summarizer-v3")

	got, err := router.Resolve("legal-summarizer-v3")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != "legal-summarizer-v3" {
		t.Errorf("Resolve() = %q, want legal-summarizer-v3", got)
	}
}

func TestRouter_RefusesUnloadedAdapter(t *testing.T) {
	router := lora.NewRouter("llama-2-7b")

	_, err := router.Resolve("never-loaded")
	if !errors.Is(err, lora.ErrAdapterNotLoaded) {
		t.Fatalf("Resolve() error = %v, want ErrAdapterNotLoaded", err)
	}
}

func TestRouter_MarkUnloadedStopsRoutingToIt(t *testing.T) {
	router := lora.NewRouter("llama-2-7b")
	router.MarkLoaded("adapter-a")
	router.MarkUnloaded("adapter-a")

	_, err := router.Resolve("adapter-a")
	if !errors.Is(err, lora.ErrAdapterNotLoaded) {
		t.Fatalf("Resolve() error = %v, want ErrAdapterNotLoaded after unload", err)
	}
}

func TestRouter_ConcurrentResolveAndLoadIsRaceFree(t *testing.T) {
	router := lora.NewRouter("llama-2-7b")
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			router.MarkLoaded("adapter-a")
		}()
		go func() {
			defer wg.Done()
			_, _ = router.Resolve("adapter-a")
		}()
	}
	wg.Wait()
}

package log_test

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	log "github.com/pod32g/simple-logger"
)

// High-cardinality messages must not grow the sampler without bound.
func TestBurstSamplerBoundsMemory(t *testing.T) {
	s := log.NewBurstSampler(10*time.Millisecond, 1, 0)

	var before runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	for i := 0; i < 200_000; i++ {
		s.Allow(log.ERROR, fmt.Sprintf("request %d failed with a reasonably long message", i), nil)
		if i%5_000 == 0 {
			time.Sleep(time.Millisecond) // let windows expire so sweeps have work
		}
	}

	var after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(s)

	// Retaining every key costs ~25 MiB here; a bounded sampler settles in the
	// kilobytes. 4 MiB sits well clear of both.
	if grew := int64(after.HeapAlloc) - int64(before.HeapAlloc); grew > 4<<20 {
		t.Errorf("sampler retained %d KiB across 200k distinct messages", grew/1024)
	}
}

// Sweeping must not change the decisions the sampler makes.
func TestBurstSamplerStillRateLimits(t *testing.T) {
	s := log.NewBurstSampler(time.Hour, 2, 3)

	allowed := 0
	for i := 0; i < 10; i++ {
		if s.Allow(log.ERROR, "same message", nil) {
			allowed++
		}
	}
	// first 2, then every 3rd of the remaining 8: entries 5 and 8.
	if allowed != 4 {
		t.Errorf("allowed %d of 10, want 4", allowed)
	}

	// A distinct message keeps its own budget.
	if !s.Allow(log.ERROR, "other message", nil) {
		t.Error("first occurrence of a new message should be allowed")
	}
}

func TestBurstSamplerReopensAfterWindow(t *testing.T) {
	s := log.NewBurstSampler(20*time.Millisecond, 1, 0)
	if !s.Allow(log.WARN, "msg", nil) {
		t.Fatal("first entry should be allowed")
	}
	if s.Allow(log.WARN, "msg", nil) {
		t.Fatal("second entry within the window should be suppressed")
	}
	time.Sleep(30 * time.Millisecond)
	if !s.Allow(log.WARN, "msg", nil) {
		t.Error("entry after the window should be allowed again")
	}
}

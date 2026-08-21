package gen

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

// TestPoolBoundsConcurrency is the whole point of the pool: however many
// callers arrive, only so many chunks are generated at once. Without it,
// several worlds each with their own chunk workers can occupy every core and
// starve the goroutines that tick the worlds.
func TestPoolBoundsConcurrency(t *testing.T) {
	const size = 3
	p := NewPool(size)
	defer p.Close()

	var running, peak atomic.Int64
	var wg sync.WaitGroup
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.run(func() {
				now := running.Add(1)
				for {
					old := peak.Load()
					if now <= old || peak.CompareAndSwap(old, now) {
						break
					}
				}
				// Enough work for overlap to be observable.
				sum := 0
				for i := range 200000 {
					sum += i
				}
				_ = sum
				running.Add(-1)
			})
		}()
	}
	wg.Wait()

	if got := peak.Load(); got > size {
		t.Errorf("%d jobs ran at once, want at most %d", got, size)
	}
	if peak.Load() < 2 {
		t.Error("the pool never ran two jobs at once; it is not a pool")
	}
}

// TestPoolRunsEveryJob checks nothing is dropped. A lost job means a chunk that
// never generates, which is a hole in the world.
func TestPoolRunsEveryJob(t *testing.T) {
	p := NewPool(4)
	defer p.Close()

	var done atomic.Int64
	var wg sync.WaitGroup
	for range 200 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.run(func() { done.Add(1) })
		}()
	}
	wg.Wait()
	if got := done.Load(); got != 200 {
		t.Errorf("%d of 200 jobs ran", got)
	}
}

// TestPoolAfterCloseStillGenerates checks work submitted to a closed pool runs
// inline rather than being dropped or panicking on a closed channel.
func TestPoolAfterCloseStillGenerates(t *testing.T) {
	p := NewPool(2)
	p.Close()
	p.Close() // Closing twice must be safe.

	ran := false
	p.run(func() { ran = true })
	if !ran {
		t.Error("a job submitted after Close did not run")
	}
}

// TestPooledGeneratorProducesTheSameWorld checks the wrapper is transparent:
// routing generation through the pool must not change a single block.
func TestPooledGeneratorProducesTheSameWorld(t *testing.T) {
	pos := world.ChunkPos{3, -4}

	direct, err := KindOverworld.New(2024)
	if err != nil {
		t.Fatal(err)
	}
	want := newChunk(KindOverworld)
	direct.GenerateChunk(pos, want)

	p := NewPool(3)
	defer p.Close()
	pooled, err := KindOverworld.NewPooled(2024, p)
	if err != nil {
		t.Fatal(err)
	}
	got := newChunk(KindOverworld)
	pooled.GenerateChunk(pos, got)

	if *snapshot(got) != *snapshot(want) {
		t.Error("the pooled generator produced a different chunk")
	}
	if pooled.DefaultSpawn(world.Overworld) != direct.DefaultSpawn(world.Overworld) {
		t.Error("the pooled generator reported a different spawn")
	}
}

// TestPooledNilIsPassthrough checks a nil pool leaves the generator alone, so
// callers that do not want pooling pay nothing.
func TestPooledNilIsPassthrough(t *testing.T) {
	// A pointer generator, so the comparison is identity rather than a
	// struct compare on a type holding a slice.
	g := NewOverworld(1)
	if Pooled(g, nil) != world.Generator(g) {
		t.Error("Pooled with a nil pool did not return the generator unchanged")
	}
}

// TestPoolSizeIsAtLeastOne checks a nonsensical size cannot produce a pool with
// no workers, which would deadlock every generation.
func TestPoolSizeIsAtLeastOne(t *testing.T) {
	for _, size := range []int{0, -5} {
		p := NewPool(size)
		if p.Size() < 1 {
			t.Errorf("NewPool(%d) has size %d", size, p.Size())
		}
		ran := false
		p.run(func() { ran = true })
		if !ran {
			t.Errorf("NewPool(%d) did not run a job", size)
		}
		p.Close()
	}
}

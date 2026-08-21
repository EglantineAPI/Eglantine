package gen

import (
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// TestConcurrentGeneration checks a generator can be used from several
// goroutines at once.
//
// This matters more than it looks: Dragonfly serialises GenerateChunk behind a
// mutex only when a world runs a single chunk worker. Configuring more than one
// removes that mutex, so anything shared and mutable in a generator becomes a
// data race. Run with -race, this is the proof that the extra workers are safe.
func TestConcurrentGeneration(t *testing.T) {
	for _, k := range []Kind{KindOverworld, KindNether, KindEnd, KindFlat} {
		g, err := k.New(1234)
		if err != nil {
			t.Fatalf("New(%s): %v", k, err)
		}

		const workers = 8
		var wg sync.WaitGroup
		for w := range workers {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				for i := range 6 {
					c := newChunk(k)
					g.GenerateChunk(world.ChunkPos{int32(w), int32(i)}, c)
				}
			}(w)
		}
		wg.Wait()
	}
}

// TestConcurrentIsDeterministic checks parallel generation still produces the
// same world. A generator that is race-free but order-dependent would give a
// different chunk depending on which worker reached it first.
func TestConcurrentIsDeterministic(t *testing.T) {
	pos := world.ChunkPos{5, -7}

	o := NewOverworld(99)
	want := newChunk(KindOverworld)
	o.GenerateChunk(pos, want)

	var wg sync.WaitGroup
	results := make([]*chunkSnapshot, 8)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := newChunk(KindOverworld)
			// Generate other chunks first, so this goroutine's generator state
			// would differ from a fresh one if any existed.
			for j := range 3 {
				o.GenerateChunk(world.ChunkPos{int32(i*10 + j), 0}, newChunk(KindOverworld))
			}
			o.GenerateChunk(pos, c)
			results[i] = snapshot(c)
		}(i)
	}
	wg.Wait()

	expect := snapshot(want)
	for i, got := range results {
		if got == nil {
			t.Fatalf("worker %d produced nothing", i)
		}
		if *got != *expect {
			t.Fatalf("worker %d generated a different chunk than a serial run", i)
		}
	}
}

// chunkSnapshot is a cheap fingerprint of a chunk's blocks.
type chunkSnapshot struct {
	sum, count uint64
}

func snapshot(c *chunk.Chunk) *chunkSnapshot {
	s := &chunkSnapshot{}
	r := c.Range()
	for x := range uint8(16) {
		for z := range uint8(16) {
			for y := int16(r.Min()); y <= int16(r.Max()); y++ {
				rid := uint64(c.Block(x, y, z, 0))
				s.sum = s.sum*31 + rid
				s.count++
			}
		}
	}
	return s
}

// TestGenerationThroughput is a guard against world generation becoming slow
// enough to freeze a joining player.
//
// A player asks for the whole square of chunks around them at once. At the view
// distance this server allows that is a few hundred chunks, and they all have
// to be generated before the world appears. When this was written a chunk took
// about 1.4ms across three workers; the bound below is deliberately loose, so
// it catches a generator that has become an order of magnitude slower rather
// than ordinary variation between machines.
func TestGenerationThroughput(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	o := NewOverworld(4242)

	const chunks = 60
	start := time.Now()
	for i := range chunks {
		o.GenerateChunk(world.ChunkPos{int32(i % 8), int32(i / 8)}, newChunk(KindOverworld))
	}
	per := time.Since(start) / chunks

	if per > 25*time.Millisecond {
		t.Errorf("a chunk takes %v to generate; a joining player would wait minutes", per)
	}
	t.Logf("%v per chunk", per)
}

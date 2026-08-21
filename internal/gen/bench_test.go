package gen

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

// BenchmarkOverworldChunk measures one chunk of overworld generation. It is
// what the throughput guard in race_test.go is calibrated against, and the
// place to start when generation gets slow: run it with -cpuprofile.
func BenchmarkOverworldChunk(b *testing.B) {
	o := NewOverworld(1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := newChunk(KindOverworld)
		o.GenerateChunk(world.ChunkPos{int32(i % 128), int32(i / 128)}, c)
	}
}

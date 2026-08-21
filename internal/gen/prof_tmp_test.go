package gen

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func BenchmarkOverworldChunk(b *testing.B) {
	o := NewOverworld(1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := newChunk(KindOverworld)
		o.GenerateChunk(world.ChunkPos{int32(i % 128), int32(i / 128)}, c)
	}
}

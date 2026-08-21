package gen

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// netherLavaLevel is the y below which carved space floods with lava.
const netherLavaLevel = 31

// Nether generates the nether: a netherrack mass hollowed into caverns, capped
// and floored with bedrock, flooded with lava at the bottom, with soul sand
// patches and glowstone on cavern ceilings.
type Nether struct {
	cave, patch *perlin
	seed        int64

	netherrack, lava, bedrock, soulSand, glowstone, gravel uint32
}

// NewNether builds a Nether generator for the seed passed.
func NewNether(seed int64) *Nether {
	br := world.DefaultBlockRegistry
	rid := func(b world.Block) uint32 { return br.BlockRuntimeID(b) }
	return &Nether{
		seed:  seed,
		cave:  newPerlin(seed + 11),
		patch: newPerlin(seed + 12),

		netherrack: rid(block.Netherrack{}),
		lava:       rid(block.Lava{Still: true, Depth: 8}),
		bedrock:    rid(block.Bedrock{}),
		soulSand:   rid(block.SoulSand{}),
		glowstone:  rid(block.Glowstone{}),
		gravel:     rid(block.Gravel{}),
	}
}

// hollow reports whether a cell is open cavern rather than solid netherrack.
func (n *Nether) hollow(x, y, z int) bool {
	v := n.cave.fbm3(float64(x), float64(y)*1.6, float64(z), 3, 1.0/60.0, 0.5)
	return v > 0.06
}

// GenerateChunk implements world.Generator.
func (n *Nether) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	min, max := int16(c.Range().Min()), int16(c.Range().Max())
	baseX, baseZ := int(pos.X())*16, int(pos.Z())*16
	bid := uint32(biome.NetherWastes{}.EncodeBiome())

	for lx := range 16 {
		for lz := range 16 {
			wx, wz := baseX+lx, baseZ+lz
			x, z := uint8(lx), uint8(lz)

			for y := min; y <= max; y++ {
				c.SetBiome(x, y, z, bid)
				wy := int(y)

				// The nether is a closed box: bedrock floor and ceiling.
				if wy <= int(min)+1 || wy >= 127 {
					c.SetBlock(x, y, z, 0, n.bedrock)
					continue
				}
				if wy > 127 {
					continue
				}

				if !n.hollow(wx, wy, wz) {
					c.SetBlock(x, y, z, 0, n.surfaceBlock(wx, wy, wz))
					continue
				}
				if wy <= netherLavaLevel {
					c.SetBlock(x, y, z, 0, n.lava)
					continue
				}
				// Open cavern. Hang glowstone where solid rock is just above.
				if !n.hollow(wx, wy+1, wz) && int(hashMix(wx, wy, wz, uint64(n.seed))%1000) < 12 {
					c.SetBlock(x, y, z, 0, n.glowstone)
				}
			}
		}
	}
}

// surfaceBlock varies the solid mass with soul sand and gravel patches.
func (n *Nether) surfaceBlock(x, y, z int) uint32 {
	p := n.patch.fbm3(float64(x), float64(y), float64(z), 2, 1.0/26.0, 0.5)
	switch {
	case p > 0.42:
		return n.soulSand
	case p < -0.46:
		return n.gravel
	default:
		return n.netherrack
	}
}

// DefaultSpawn implements world.Generator.
func (n *Nether) DefaultSpawn(world.Dimension) cube.Pos {
	for y := netherLavaLevel + 2; y < 120; y++ {
		if n.hollow(0, y, 0) && n.hollow(0, y+1, 0) && !n.hollow(0, y-1, 0) {
			return cube.Pos{0, y, 0}
		}
	}
	return cube.Pos{0, 64, 0}
}

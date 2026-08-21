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

// netherRoof is the bedrock ceiling. The nether is a closed box.
const netherRoof = 127

// Nether generates the nether: a netherrack mass hollowed into caverns, capped
// and floored with bedrock, flooded with lava at the bottom, with soul sand and
// gravel patches, glowstone clusters hanging from cavern ceilings, and the
// nether ores placed to vanilla's own table.
type Nether struct {
	cave, patch *perlin
	seed        int64

	netherrack, lava, bedrock, soulSand, glowstone, gravel uint32
	quartzOre, goldOre, debris, magma, blackstone          uint32

	ores *veinField
}

// NewNether builds a Nether generator for the seed passed.
func NewNether(seed int64) *Nether {
	br := world.DefaultBlockRegistry
	rid := func(b world.Block) uint32 { return br.BlockRuntimeID(b) }

	n := &Nether{
		seed:  seed,
		cave:  newPerlin(seed + 11),
		patch: newPerlin(seed + 12),

		netherrack: rid(block.Netherrack{}),
		lava:       rid(block.Lava{Still: true, Depth: 8}),
		bedrock:    rid(block.Bedrock{}),
		soulSand:   rid(block.SoulSand{}),
		glowstone:  rid(block.Glowstone{}),
		gravel:     rid(block.Gravel{}),
		quartzOre:  rid(block.NetherQuartzOre{}),
		goldOre:    rid(block.NetherGoldOre{}),
		debris:     rid(block.AncientDebris{}),
		magma:      rid(block.Magma{}),
		blackstone: rid(block.Blackstone{}),
	}
	n.ores = n.buildOreField()
	return n
}

// buildOreField assembles the nether ore table from vanilla's own numbers.
//
// Ancient debris uses vanilla's scattered_ore rather than a blob, which is why
// it turns up as isolated blocks, and both of its batches discard on any air
// exposure, which is why it is never found in the wall of an open cavern.
func (n *Nether) buildOreField() *veinField {
	return &veinField{
		seed: n.seed,
		air:  world.DefaultBlockRegistry.AirRuntimeID(),
		hostRock: func(rid uint32) (bool, bool) {
			return false, rid == n.netherrack
		},
		specs: []veinSpec{
			{block: n.quartzOre, count: 16, size: 14, dist: distUniform, minY: 10, maxY: 118},
			{block: n.goldOre, count: 10, size: 10, dist: distUniform, minY: 10, maxY: 118},

			// Netherite. One attempt each of two tiny scattered batches, both
			// discarded wherever they touch air, is the whole supply.
			{block: n.debris, count: 1, size: 3, dist: distTriangle, minY: 8, maxY: 24, discardOnAir: 1.0, scattered: true},
			{block: n.debris, count: 1, size: 2, dist: distUniform, minY: 8, maxY: 119, discardOnAir: 1.0, scattered: true},

			{block: n.magma, count: 4, size: 33, dist: distUniform, minY: 27, maxY: 36},
			{block: n.blackstone, count: 2, size: 33, dist: distUniform, minY: 5, maxY: 31},
			{block: n.gravel, count: 2, size: 33, dist: distUniform, minY: 5, maxY: 41},
		},
	}
}

// hollow reports whether a cell is open cavern rather than solid netherrack.
func (n *Nether) hollow(x, y, z int) bool {
	v := n.cave.fbm3(float64(x), float64(y)*1.6, float64(z), 3, 1.0/60.0, 0.5)
	return v > 0.06
}

// GenerateChunk implements world.Generator.
func (n *Nether) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	cp := chunkPos{x: int(pos.X()), z: int(pos.Z())}
	n.generateTerrain(cp, c)
	n.ores.place(cp, c, false)
	n.growGlowstone(cp, c)
}

// generateTerrain lays down the netherrack mass, the lava sea and the bedrock
// shell.
func (n *Nether) generateTerrain(pos chunkPos, c *chunk.Chunk) {
	min, max := int16(c.Range().Min()), int16(c.Range().Max())
	baseX, baseZ := pos.x*16, pos.z*16
	bid := uint32(biome.NetherWastes{}.EncodeBiome())

	for lx := range 16 {
		for lz := range 16 {
			wx, wz := baseX+lx, baseZ+lz
			x, z := uint8(lx), uint8(lz)

			for y := min; y <= max; y++ {
				c.SetBiome(x, y, z, bid)
				wy := int(y)

				if wy <= int(min)+1 || wy == netherRoof {
					c.SetBlock(x, y, z, 0, n.bedrock)
					continue
				}
				if wy > netherRoof {
					continue
				}
				if !n.hollow(wx, wy, wz) {
					c.SetBlock(x, y, z, 0, n.surfaceBlock(wx, wy, wz))
					continue
				}
				if wy <= netherLavaLevel {
					c.SetBlock(x, y, z, 0, n.lava)
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

// glowstoneAttempts is how many clusters are tried per chunk. Glowstone is
// meant to be a landmark you cross a cavern towards, so there are few
// attempts and each grows a real cluster, rather than many attempts each
// leaving a single block.
const glowstoneAttempts = 2

// growGlowstone hangs clusters of glowstone from cavern ceilings.
//
// Each attempt scans a column downward for a real ceiling — solid rock with
// open air directly beneath it — rather than testing one random height and
// giving up. Testing a single height almost always misses, which left the
// nether with a few isolated blocks instead of clusters.
func (n *Nether) growGlowstone(pos chunkPos, c *chunk.Chunk) {
	baseX, baseZ := pos.x*16, pos.z*16
	r := &detRand{s: hashMix(pos.x, 7, pos.z, uint64(n.seed)+0x91045)}

	for range glowstoneAttempts {
		lx, lz := r.intn(16), r.intn(16)
		wx, wz := baseX+lx, baseZ+lz
		start := netherLavaLevel + 6 + r.intn(netherRoof-netherLavaLevel-14)

		y, found := n.ceilingBelow(wx, start, wz)
		if !found {
			continue
		}
		n.hangCluster(c, r, lx, y, lz, wx, wz)
	}
}

// ceilingBelow scans down from a height for the first open cell with solid rock
// directly above it, returning that cell.
func (n *Nether) ceilingBelow(x, from, z int) (int, bool) {
	for y := from; y > netherLavaLevel+2; y-- {
		if n.hollow(x, y, z) && !n.hollow(x, y+1, z) {
			return y, true
		}
	}
	return 0, false
}

// hangCluster writes one glowstone cluster growing down from a ceiling.
func (n *Nether) hangCluster(c *chunk.Chunk, r *detRand, lx, y, lz, wx, wz int) {
	blocks := 14 + r.intn(18)
	minY := int(c.Range().Min())

	for range blocks {
		// Offsets are drawn twice and averaged, which biases the cluster
		// towards its anchor and keeps it connected rather than sprayed.
		dx := (r.intn(5)+r.intn(5))/2 - 2
		dz := (r.intn(5)+r.intn(5))/2 - 2
		dy := -((r.intn(4) + r.intn(4)) / 2)

		bx, by, bz := lx+dx, y+dy, lz+dz
		if bx < 0 || bx > 15 || bz < 0 || bz > 15 || by <= minY || by >= netherRoof {
			continue
		}
		// Only fill open cavern; a cluster must not bury itself in the rock.
		if !n.hollow(wx+dx, by, wz+dz) {
			continue
		}
		c.SetBlock(uint8(bx), int16(by), uint8(bz), 0, n.glowstone)
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

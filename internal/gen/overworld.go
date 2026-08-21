package gen

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// seaLevel is the y at and below which open air is filled with water.
const seaLevel = 62

// chunkPos is a chunk coordinate in ints, which is easier to do arithmetic with
// than world.ChunkPos.
type chunkPos struct{ x, z int }

// columns caches what the terrain pass decided for each column of a chunk, so
// later passes agree with the blocks actually placed.
//
// Recomputing a column's biome in a later pass is not equivalent: the biome
// depends on the river field as well as the height, and getting that argument
// wrong is what once put trees on river gravel.
type columns struct {
	height [16][16]int
	top    [16][16]uint32
	biome  [16][16]world.Biome
	// hasMountain records whether any column is a peak biome, letting the ore
	// pass skip emerald entirely almost everywhere.
	hasMountain bool
}

// Overworld generates survivable terrain: rolling land shaped by fractal noise,
// climate-driven biomes, oceans and rivers, noise-carved caves with lava at the
// bottom, trees on the surface, and ore placed to vanilla's own table.
//
// It is deliberately not a reimplementation of vanilla generation. There are no
// structures, and a given seed does not match the world the same seed makes in
// Bedrock. What it guarantees is a world you can survive in.
type Overworld struct {
	terrain, climate, humid, cave, river *perlin
	seed                                 int64

	// Runtime IDs are resolved once, since resolving a block on every one of
	// the ~98k positions in a chunk would dominate generation time.
	air, stone, deepslate, dirt, grass, sand, gravel, snow uint32
	water, lava, bedrock, log, leaves                      uint32

	// ore and oreDeepslate are indexed by oreKind.
	ore, oreDeepslate [oreKindCount]uint32
}

// NewOverworld builds an Overworld generator for the seed passed. Worlds
// generated from equal seeds are identical.
func NewOverworld(seed int64) *Overworld {
	br := world.DefaultBlockRegistry
	rid := func(b world.Block) uint32 { return br.BlockRuntimeID(b) }

	o := &Overworld{
		seed: seed,
		// Each field gets its own permutation, otherwise the heightmap and the
		// climate fields would be perfectly correlated.
		terrain: newPerlin(seed),
		climate: newPerlin(seed + 1),
		humid:   newPerlin(seed + 2),
		cave:    newPerlin(seed + 3),
		river:   newPerlin(seed + 4),

		air:       rid(block.Air{}),
		stone:     rid(block.Stone{}),
		deepslate: rid(block.Deepslate{Type: block.NormalDeepslate()}),
		dirt:      rid(block.Dirt{}),
		grass:     rid(block.Grass{}),
		sand:      rid(block.Sand{}),
		gravel:    rid(block.Gravel{}),
		snow:      rid(block.Snow{}),
		water:     rid(block.Water{Still: true, Depth: 8}),
		lava:      rid(block.Lava{Still: true, Depth: 8}),
		bedrock:   rid(block.Bedrock{}),
		log:       rid(block.Log{Wood: block.OakWood(), Axis: cube.Y}),
		leaves:    rid(block.Leaves{Type: block.OakLeaves(), Persistent: true}),
	}

	stone, deep := block.StoneOre(), block.DeepslateOre()
	for kind, pair := range map[oreKind][2]world.Block{
		oreCoal:     {block.CoalOre{Type: stone}, block.CoalOre{Type: deep}},
		oreIron:     {block.IronOre{Type: stone}, block.IronOre{Type: deep}},
		oreCopper:   {block.CopperOre{Type: stone}, block.CopperOre{Type: deep}},
		oreGold:     {block.GoldOre{Type: stone}, block.GoldOre{Type: deep}},
		oreRedstone: {block.RedstoneOre{Type: stone}, block.RedstoneOre{Type: deep}},
		oreDiamond:  {block.DiamondOre{Type: stone}, block.DiamondOre{Type: deep}},
		oreLapis:    {block.LapisOre{Type: stone}, block.LapisOre{Type: deep}},
		oreEmerald:  {block.EmeraldOre{Type: stone}, block.EmeraldOre{Type: deep}},
	} {
		o.ore[kind] = rid(pair[0])
		o.oreDeepslate[kind] = rid(pair[1])
	}
	return o
}

// hash mixes a coordinate triple and the seed into a well-distributed uint64.
func (o *Overworld) hash(x, y, z int, salt uint64) uint64 {
	return hashMix(x, y, z, uint64(o.seed)+salt)
}

// heightAt returns the surface height of a column and the river strength there,
// where a strength above zero means the column sits inside a river channel.
func (o *Overworld) heightAt(x, z int) (int, float64) {
	fx, fz := float64(x), float64(z)

	// fbm output clusters well inside [-1, 1], so each field is amplified and
	// clamped. Without this the whole world sits within a few blocks of sea
	// level and reads as flat.
	continent := clampF(o.terrain.fbm(fx, fz, 3, 1.0/900.0, 0.5)*2.4, -1, 1)
	hills := o.terrain.fbm(fx+1000, fz-1000, 4, 1.0/170.0, 0.5) * 2.0

	var h float64
	if continent < -0.10 {
		// Ocean basin, deepening away from the shore.
		depth := clampF((continent+0.10)/-0.90, 0, 1)
		h = float64(seaLevel) - 5 - depth*30
	} else {
		land := (continent + 0.10) / 1.10
		base := float64(seaLevel) + 2 + land*28

		// Ridged noise: folding the field about zero turns rounded hills into
		// the sharp crests that read as mountain ranges. Cubing it keeps the
		// ridges rare and the lowlands broad.
		ridge := 1 - absF(o.terrain.fbm(fx-3000, fz+3000, 4, 1.0/320.0, 0.5)*2.2)
		ridge = clampF(ridge, 0, 1)
		h = base + hills*13 + ridge*ridge*ridge*78*land
	}

	riverN := o.river.fbm(fx, fz, 2, 1.0/700.0, 0.5)
	strength := 0.0
	if w := 0.045; riverN > -w && riverN < w && continent >= -0.10 {
		strength = 1 - absF(riverN)/w
		// Pull the channel floor below sea level so the river actually holds
		// water. Blending the height instead of subtracting a fixed amount is
		// what keeps a river through high ground from becoming a dry canyon of
		// bare gravel.
		smooth := strength * strength * (3 - 2*strength)
		h = h*(1-smooth) + (float64(seaLevel)-3)*smooth
	}
	return int(h), strength
}

// clampF constrains v to [lo, hi].
func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// biomeAt picks a biome from climate, height and river state.
//
// Ocean and river are only ever chosen for a submerged column. A river biome on
// dry land would surface as a strip of gravel with no water in it, which is not
// a biome the game has.
func (o *Overworld) biomeAt(x, z, height int, river float64) world.Biome {
	if height < seaLevel {
		if river > 0.35 {
			return biome.River{}
		}
		return biome.Ocean{}
	}
	if height <= seaLevel+2 {
		return biome.Beach{}
	}

	temp := o.climate.fbm(float64(x), float64(z), 3, 1.0/600.0, 0.5)
	rain := o.humid.fbm(float64(x)+500, float64(z)+500, 3, 1.0/600.0, 0.5)

	switch {
	case height > seaLevel+42:
		if temp < -0.1 {
			return biome.FrozenPeaks{}
		}
		return biome.StonyPeaks{}
	case temp < -0.35:
		return biome.SnowyPlains{}
	case temp > 0.35 && rain < -0.1:
		return biome.Desert{}
	case temp > 0.2 && rain > 0.25:
		return biome.Jungle{}
	case rain > 0.15:
		return biome.Forest{}
	case temp < -0.1:
		return biome.Taiga{}
	case rain < -0.25:
		return biome.Savanna{}
	default:
		return biome.Plains{}
	}
}

// surfaceFor returns the top block and the block filling the few layers under
// it. Submerged columns get a river or sea bed rather than a land surface.
func (o *Overworld) surfaceFor(b world.Biome, height int) (top, filler uint32) {
	if height < seaLevel {
		return o.gravel, o.dirt
	}
	switch b.(type) {
	case biome.Desert, biome.Beach:
		return o.sand, o.sand
	case biome.SnowyPlains, biome.FrozenPeaks:
		return o.snow, o.dirt
	case biome.StonyPeaks:
		return o.stone, o.stone
	}
	if height > seaLevel+38 {
		return o.stone, o.stone
	}
	return o.grass, o.dirt
}

// GenerateChunk implements world.Generator. It runs three passes: terrain,
// then ore, then decoration. Ore has to see the finished stone, and decoration
// has to see the finished surface.
func (o *Overworld) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	cp := chunkPos{x: int(pos.X()), z: int(pos.Z())}
	cols := &columns{}
	o.generateTerrain(cp, c, cols)
	o.placeOres(cp, c, cols)
	o.decorate(c, cols)
}

// generateTerrain lays down stone, surface, water and bedrock, and carves caves.
func (o *Overworld) generateTerrain(pos chunkPos, c *chunk.Chunk, cols *columns) {
	min, max := int16(c.Range().Min()), int16(c.Range().Max())
	baseX, baseZ := pos.x*16, pos.z*16

	for lx := range 16 {
		for lz := range 16 {
			wx, wz := baseX+lx, baseZ+lz
			height, river := o.heightAt(wx, wz)
			b := o.biomeAt(wx, wz, height, river)
			top, filler := o.surfaceFor(b, height)

			cols.height[lx][lz] = height
			cols.biome[lx][lz] = b
			cols.top[lx][lz] = top
			switch b.(type) {
			case biome.StonyPeaks, biome.FrozenPeaks:
				cols.hasMountain = true
			}

			bid := uint32(b.EncodeBiome())
			x, z := uint8(lx), uint8(lz)
			for y := min; y <= max; y++ {
				c.SetBiome(x, y, z, bid)

				wy := int(y)
				switch {
				case wy <= int(min)+o.bedrockDepth(wx, wz):
					c.SetBlock(x, y, z, 0, o.bedrock)
					continue
				case wy > height:
					if wy <= seaLevel {
						c.SetBlock(x, y, z, 0, o.water)
					}
					continue
				}

				// Solid column. Carve caves before deciding on a block, so a
				// carved cell can still flood with lava at the very bottom.
				if o.carved(wx, wy, wz, height) {
					if wy < -50 {
						c.SetBlock(x, y, z, 0, o.lava)
					}
					continue
				}

				switch {
				case wy == height:
					c.SetBlock(x, y, z, 0, top)
				case wy > height-4:
					c.SetBlock(x, y, z, 0, filler)
				case wy < 0:
					c.SetBlock(x, y, z, 0, o.deepslate)
				default:
					c.SetBlock(x, y, z, 0, o.stone)
				}
			}
		}
	}
}

// bedrockDepth returns how many layers of bedrock cover this column, giving the
// floor a ragged rather than a perfectly flat underside.
func (o *Overworld) bedrockDepth(x, z int) int {
	return int(o.hash(x, 0, z, 0x8ed7) % 4)
}

// carved reports whether a cell is hollowed out by the cave system. Caves stop
// short of the surface so the terrain is not perforated from above.
func (o *Overworld) carved(x, y, z, height int) bool {
	if y > height-6 || y < -60 {
		return false
	}
	// The deepslate layer is far more riddled with caves than the stone above
	// it, as it is in vanilla. That is also what keeps deep ore honest: the
	// rare ores lean on discarding blocks that touch air, and a solid deep
	// layer would leave every one of them in place.
	threshold := 0.13
	if y < 0 {
		threshold = 0.20
	}
	// Two independent fields intersected produce tunnels rather than blobs.
	a := o.cave.fbm3(float64(x), float64(y)*2.4, float64(z), 2, 1.0/48.0, 0.5)
	if absF(a) > threshold {
		return false
	}
	b := o.cave.fbm3(float64(x)+700, float64(y)*2.4, float64(z)-700, 2, 1.0/48.0, 0.5)
	return absF(b) <= threshold
}

// treeChanceFor returns the per-column probability of a tree, scaled by 1e-4.
func treeChanceFor(b world.Biome) int {
	switch b.(type) {
	case biome.Forest:
		return 900
	case biome.Jungle:
		return 1100
	case biome.Taiga:
		return 750
	case biome.Plains:
		return 120
	case biome.Savanna:
		return 70
	default:
		return 0
	}
}

// decorate places trees, reading the biome and surface the terrain pass
// actually used rather than recomputing them.
func (o *Overworld) decorate(c *chunk.Chunk, cols *columns) {
	maxY := int(c.Range().Max())

	// Trees are kept two blocks clear of the chunk edge. A tree straddling the
	// border would need to write into a neighbouring chunk, which GenerateChunk
	// cannot do.
	for lx := 2; lx < 14; lx++ {
		for lz := 2; lz < 14; lz++ {
			height := cols.height[lx][lz]
			// Only grow on a grass top, which rules out sand, stone, snow and
			// the gravel of a river or sea bed.
			if cols.top[lx][lz] != o.grass || height <= seaLevel {
				continue
			}
			chance := treeChanceFor(cols.biome[lx][lz])
			if chance == 0 {
				continue
			}
			// Local coordinates are enough here: two columns in different
			// chunks never share one, because the hash also takes the height.
			h := o.hash(lx, height, lz, uint64(cols.height[0][0])*31+0x7ee5)
			if int(h%10000) >= chance {
				continue
			}
			o.placeTree(c, lx, height+1, lz, maxY, h)
		}
	}
}

// placeTree writes a small oak into the chunk at the local column passed.
func (o *Overworld) placeTree(c *chunk.Chunk, lx, baseY, lz, maxY int, h uint64) {
	trunk := 4 + int(h%3)
	if baseY+trunk+2 > maxY {
		return
	}
	// Canopy: two wide layers around the top of the trunk, then a small cap.
	leafBase := baseY + trunk - 2
	for dy := range 3 {
		y := leafBase + dy
		r := 2
		if dy == 2 {
			r = 1
		}
		for dx := -r; dx <= r; dx++ {
			for dz := -r; dz <= r; dz++ {
				// Clip the corners of the widest layers to round the canopy.
				if r == 2 && absI(dx) == 2 && absI(dz) == 2 {
					continue
				}
				x, z := lx+dx, lz+dz
				if x < 0 || x > 15 || z < 0 || z > 15 {
					continue
				}
				c.SetBlock(uint8(x), int16(y), uint8(z), 0, o.leaves)
			}
		}
	}
	c.SetBlock(uint8(lx), int16(baseY+trunk), uint8(lz), 0, o.leaves)
	for dy := range trunk {
		c.SetBlock(uint8(lx), int16(baseY+dy), uint8(lz), 0, o.log)
	}
}

func absI(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// DefaultSpawn implements world.Generator. It walks outward from the origin
// looking for dry land, so players do not spawn in the middle of an ocean.
func (o *Overworld) DefaultSpawn(dim world.Dimension) cube.Pos {
	for r := 0; r < 3000; r += 16 {
		for _, p := range [][2]int{{r, 0}, {0, r}, {-r, 0}, {0, -r}, {r, r}, {-r, -r}} {
			h, river := o.heightAt(p[0], p[1])
			if h > seaLevel+1 && river == 0 {
				return cube.Pos{p[0], h + 1, p[1]}
			}
		}
	}
	return cube.Pos{0, seaLevel + 1, 0}
}

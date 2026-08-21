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

// Overworld generates survivable terrain: rolling land shaped by fractal
// noise, climate-driven biomes, oceans and rivers, ore veins spread by depth,
// noise-carved caves with lava at the bottom, and trees on the surface.
//
// It is deliberately not a reimplementation of vanilla generation. There are
// no structures, and a given seed does not match the world the same seed makes
// in Bedrock. What it guarantees is a world you can survive in.
type Overworld struct {
	terrain, climate, humid, cave, river *perlin
	seed                                 int64

	// Runtime IDs are resolved once, since resolving a block on every one of
	// the ~98k column positions in a chunk would dominate generation time.
	air, stone, deepslate, dirt, grass, sand, gravel, snow uint32
	water, lava, bedrock, log, leaves                      uint32
	oreStone, oreDeep                                      [6]uint32
}

// oreSpec describes where a single ore type may appear and how common it is.
type oreSpec struct {
	minY, maxY int
	// chance is the per-block probability of starting a vein, scaled by 1e-4.
	chance int
	// size is the number of blocks placed once a vein starts.
	size int
}

// oreSpecs is indexed in step with the oreStone and oreDeep runtime ID arrays.
var oreSpecs = [6]oreSpec{
	{minY: -60, maxY: 128, chance: 90, size: 12}, // coal
	{minY: -60, maxY: 64, chance: 60, size: 8},   // iron
	{minY: -60, maxY: 30, chance: 20, size: 6},   // gold
	{minY: -60, maxY: 14, chance: 8, size: 5},    // diamond
	{minY: -60, maxY: 15, chance: 22, size: 7},   // redstone
	{minY: -60, maxY: 30, chance: 10, size: 6},   // lapis
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
	stoneOres := []world.Block{
		block.CoalOre{Type: block.StoneOre()},
		block.IronOre{Type: block.StoneOre()},
		block.GoldOre{Type: block.StoneOre()},
		block.DiamondOre{Type: block.StoneOre()},
		block.RedstoneOre{Type: block.StoneOre()},
		block.LapisOre{Type: block.StoneOre()},
	}
	deepOres := []world.Block{
		block.CoalOre{Type: block.DeepslateOre()},
		block.IronOre{Type: block.DeepslateOre()},
		block.GoldOre{Type: block.DeepslateOre()},
		block.DiamondOre{Type: block.DeepslateOre()},
		block.RedstoneOre{Type: block.DeepslateOre()},
		block.LapisOre{Type: block.DeepslateOre()},
	}
	for i := range stoneOres {
		o.oreStone[i] = rid(stoneOres[i])
		o.oreDeep[i] = rid(deepOres[i])
	}
	return o
}

// hash mixes a coordinate triple and the seed into a well-distributed uint64,
// giving decisions that depend only on position and seed. Using a hash rather
// than a rand.Source keeps generation reproducible under the concurrent
// GenerateChunk calls Dragonfly makes.
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

	// Rivers cut narrow channels wherever a separate noise field crosses zero.
	riverN := o.river.fbm(fx, fz, 2, 1.0/700.0, 0.5)
	strength := 0.0
	if w := 0.045; riverN > -w && riverN < w && continent >= -0.10 {
		strength = 1 - absF(riverN)/w
		h -= strength * 9
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
func (o *Overworld) biomeAt(x, z, height int, river float64) world.Biome {
	if height < seaLevel-5 {
		return biome.Ocean{}
	}
	if river > 0 {
		return biome.River{}
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
// it for the biome passed.
func (o *Overworld) surfaceFor(b world.Biome, height int) (top, filler uint32) {
	switch b.(type) {
	case biome.Desert:
		return o.sand, o.sand
	case biome.Beach:
		return o.sand, o.sand
	case biome.Ocean, biome.River:
		return o.gravel, o.dirt
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

// GenerateChunk implements world.Generator.
func (o *Overworld) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	min, max := int16(c.Range().Min()), int16(c.Range().Max())
	baseX, baseZ := int(pos.X())*16, int(pos.Z())*16

	// Heights are needed again during decoration, so keep them for the chunk.
	var heights [16][16]int

	for lx := range 16 {
		for lz := range 16 {
			wx, wz := baseX+lx, baseZ+lz
			height, river := o.heightAt(wx, wz)
			heights[lx][lz] = height

			b := o.biomeAt(wx, wz, height, river)
			bid := uint32(b.EncodeBiome())
			top, filler := o.surfaceFor(b, height)

			x, z := uint8(lx), uint8(lz)
			for y := min; y <= max; y++ {
				c.SetBiome(x, y, z, bid)

				wy := int(y)
				switch {
				case wy <= int(min)+o.bedrockDepth(wx, wy, wz):
					c.SetBlock(x, y, z, 0, o.bedrock)
					continue
				case wy > height:
					// Open air: water down to sea level, otherwise nothing.
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
				default:
					c.SetBlock(x, y, z, 0, o.stoneAt(wx, wy, wz))
				}
			}
		}
	}
	o.decorate(pos, c, &heights)
}

// bedrockDepth returns how many layers of bedrock cover this column, giving the
// floor a ragged rather than a perfectly flat underside.
func (o *Overworld) bedrockDepth(x, y, z int) int {
	return int(o.hash(x, 0, z, 0x8ed7) % 4)
}

// carved reports whether a cell is hollowed out by the cave system. Caves stop
// short of the surface so the terrain is not perforated from above.
func (o *Overworld) carved(x, y, z, height int) bool {
	if y > height-6 || y < -58 {
		return false
	}
	// Two independent fields intersected produce tunnels rather than blobs.
	a := o.cave.fbm3(float64(x), float64(y)*2.4, float64(z), 2, 1.0/48.0, 0.5)
	if absF(a) > 0.13 {
		return false
	}
	b := o.cave.fbm3(float64(x)+700, float64(y)*2.4, float64(z)-700, 2, 1.0/48.0, 0.5)
	return absF(b) <= 0.13
}

// stoneAt returns the stone-family block for a position, folding in ore veins.
func (o *Overworld) stoneAt(x, y, z int) uint32 {
	base := o.stone
	deep := y < 0
	if deep {
		base = o.deepslate
	}
	for i, spec := range oreSpecs {
		if y < spec.minY || y > spec.maxY {
			continue
		}
		// A vein starts at this block, or this block sits inside one that
		// started nearby. Checking a small neighbourhood keeps veins contiguous
		// without carrying state between columns.
		if int(o.hash(x, y, z, uint64(i)*0x51ed)%10000) < spec.chance {
			if deep {
				return o.oreDeep[i]
			}
			return o.oreStone[i]
		}
		if o.inVein(x, y, z, i, spec) {
			if deep {
				return o.oreDeep[i]
			}
			return o.oreStone[i]
		}
	}
	return base
}

// inVein reports whether a nearby block started a vein of ore i that reaches
// the position passed.
func (o *Overworld) inVein(x, y, z, i int, spec oreSpec) bool {
	// radius grows with vein size but stays small; the loop below is 27 hashes
	// at radius 1, which is affordable per stone block.
	const radius = 1
	for dx := -radius; dx <= radius; dx++ {
		for dy := -radius; dy <= radius; dy++ {
			for dz := -radius; dz <= radius; dz++ {
				if dx == 0 && dy == 0 && dz == 0 {
					continue
				}
				nx, ny, nz := x+dx, y+dy, z+dz
				if ny < spec.minY || ny > spec.maxY {
					continue
				}
				if int(o.hash(nx, ny, nz, uint64(i)*0x51ed)%10000) < spec.chance {
					// The neighbour is a vein origin; extend it here when the
					// vein is large enough to cover the offset.
					if spec.size > 4 {
						return true
					}
				}
			}
		}
	}
	return false
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

// decorate places trees. It runs after the terrain pass so it can read the
// finished heightmap for the chunk.
func (o *Overworld) decorate(pos world.ChunkPos, c *chunk.Chunk, heights *[16][16]int) {
	maxY := int(c.Range().Max())
	baseX, baseZ := int(pos.X())*16, int(pos.Z())*16

	// Trees are kept two blocks clear of the chunk edge. A tree straddling the
	// border would need to write into a neighbouring chunk, which GenerateChunk
	// cannot do.
	for lx := 2; lx < 14; lx++ {
		for lz := 2; lz < 14; lz++ {
			wx, wz := baseX+lx, baseZ+lz
			height := heights[lx][lz]
			if height <= seaLevel+1 {
				continue
			}
			b := o.biomeAt(wx, wz, height, 0)
			chance := treeChanceFor(b)
			if chance == 0 {
				continue
			}
			if int(o.hash(wx, 0, wz, 0x7ee5)%10000) >= chance {
				continue
			}
			// Only grow on a grass top, so trees never sprout on sand or stone.
			if top, _ := o.surfaceFor(b, height); top != o.grass {
				continue
			}
			o.placeTree(c, lx, height+1, lz, maxY, o.hash(wx, 1, wz, 0x7ee5))
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

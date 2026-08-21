package gen

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
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
	biome  [16][16]biomeKind
	// hasMountain records whether any column is a peak biome, letting the ore
	// pass skip emerald entirely almost everywhere.
	hasMountain bool
}

// Overworld generates survivable terrain: rolling land shaped by fractal noise,
// climate-driven biomes with their own trees and ground cover, oceans and
// rivers, a cave system of tunnels, deep caverns, ravines and rare lush
// pockets, and ore placed to vanilla's own table.
//
// It is deliberately not a reimplementation of vanilla generation. There are no
// structures, and a given seed does not match the world the same seed makes in
// Bedrock. What it guarantees is a world you can survive in.
type Overworld struct {
	terrain, climate, humid, cave, river, cavern, lush, ravine *perlin
	seed                                                       int64

	// Runtime IDs are resolved once, since resolving a block on every one of
	// the ~98k positions in a chunk would dominate generation time.
	air, stone, deepslate, dirt, grass, sand, gravel, snow, podzol uint32
	water, lava, bedrock                                           uint32

	// Per-biome surface, indexed by biomeKind.
	biomeID, topRID, fillerRID [biomeKindCount]uint32
	// Per-tree wood, indexed by treeKind.
	treeLog, treeLeaves [treeKindCount]uint32
	// ore and oreDeepslate are indexed by oreKind.
	ore, oreDeepslate [oreKindCount]uint32

	plants plants
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
		cavern:  newPerlin(seed + 5),
		lush:    newPerlin(seed + 6),
		ravine:  newPerlin(seed + 7),

		air:       rid(block.Air{}),
		stone:     rid(block.Stone{}),
		deepslate: rid(block.Deepslate{Type: block.NormalDeepslate()}),
		dirt:      rid(block.Dirt{}),
		grass:     rid(block.Grass{}),
		sand:      rid(block.Sand{}),
		gravel:    rid(block.Gravel{}),
		snow:      rid(block.Snow{}),
		podzol:    rid(block.Podzol{}),
		water:     rid(block.Water{Still: true, Depth: 8}),
		lava:      rid(block.Lava{Still: true, Depth: 8}),
		bedrock:   rid(block.Bedrock{}),
	}

	for k := biomeKind(0); k < biomeKindCount; k++ {
		info := biomeTable[k]
		o.biomeID[k] = uint32(info.biome.EncodeBiome())
		o.topRID[k] = rid(info.top)
		o.fillerRID[k] = rid(info.filler)
	}
	o.resolveTrees(rid)
	o.plants = resolvePlants(rid)

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
		h = float64(seaLevel) - 5 - depth*34
	} else {
		land := (continent + 0.10) / 1.10
		// Raising land to a power under one makes the ground climb steeply
		// away from the shore. With a linear rise most of the landmass sat
		// within a couple of blocks of sea level and was classified as beach.
		lift := math.Pow(land, 0.55)
		base := float64(seaLevel) + 3 + lift*34

		// Ridged noise: folding the field about zero turns rounded hills into
		// the sharp crests that read as mountain ranges. Cubing it keeps the
		// ridges rare and the lowlands broad.
		ridge := 1 - absF(o.terrain.fbm(fx-3000, fz+3000, 4, 1.0/320.0, 0.5)*2.2)
		ridge = clampF(ridge, 0, 1)
		// Scaling the hills by lift as well keeps the coast from being dragged
		// back under water by a downward swing of the hill field.
		h = base + hills*13*lift + ridge*ridge*ridge*82*land
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

func absI(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// GenerateChunk implements world.Generator. It runs three passes: terrain, then
// ore, then decoration. Ore has to see the finished stone, and decoration has
// to see the finished surface.
func (o *Overworld) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	cp := chunkPos{x: int(pos.X()), z: int(pos.Z())}
	cols := &columns{}
	o.generateTerrain(cp, c, cols)
	o.placeOres(cp, c, cols)
	o.decorate(c, cp, cols)
}

// generateTerrain lays down stone, surface, water and bedrock, and carves the
// cave system.
func (o *Overworld) generateTerrain(pos chunkPos, c *chunk.Chunk, cols *columns) {
	min, max := int16(c.Range().Min()), int16(c.Range().Max())
	baseX, baseZ := pos.x*16, pos.z*16

	for lx := range 16 {
		for lz := range 16 {
			wx, wz := baseX+lx, baseZ+lz
			height, river := o.heightAt(wx, wz)
			kind := o.biomeAt(wx, wz, height, river)

			cols.height[lx][lz] = height
			cols.biome[lx][lz] = kind
			if kind == bStonyPeaks || kind == bFrozenPeaks || kind == bSnowySlopes {
				cols.hasMountain = true
			}

			cc := o.columnCaveAt(wx, wz, height)
			bid, top, filler := o.biomeID[kind], o.topRID[kind], o.fillerRID[kind]

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

				// Carve before deciding on a block, so a carved cell can still
				// flood with lava at the very bottom.
				if o.carvedAt(wx, wy, wz, height, cc) {
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

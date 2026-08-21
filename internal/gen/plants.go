package gen

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// plants holds the runtime IDs of everything the decoration pass can place.
type plants struct {
	shortGrass, fern, deadBush       uint32
	tallGrassLower, tallGrassUpper   uint32
	cactus, sugarCane, lilyPad, kelp uint32
	mossCarpet                       uint32
	flower                           map[block.FlowerType]uint32
}

func resolvePlants(rid func(world.Block) uint32) plants {
	p := plants{
		shortGrass:     rid(block.ShortGrass{}),
		fern:           rid(block.Fern{}),
		deadBush:       rid(block.DeadBush{}),
		tallGrassLower: rid(block.DoubleTallGrass{Type: block.NormalDoubleTallGrass()}),
		tallGrassUpper: rid(block.DoubleTallGrass{Type: block.NormalDoubleTallGrass(), UpperPart: true}),
		cactus:         rid(block.Cactus{}),
		sugarCane:      rid(block.SugarCane{}),
		lilyPad:        rid(block.LilyPad{}),
		kelp:           rid(block.Kelp{}),
		mossCarpet:     rid(block.MossCarpet{}),
		flower:         map[block.FlowerType]uint32{},
	}
	for _, f := range block.FlowerTypes() {
		p.flower[f] = rid(block.Flower{Type: f})
	}
	return p
}

// decorate places everything that grows, reading the biome and surface the
// terrain pass recorded rather than recomputing them.
func (o *Overworld) decorate(c *chunk.Chunk, pos chunkPos, cols *columns) {
	maxY := int(c.Range().Max())
	baseX, baseZ := pos.x*16, pos.z*16

	for lx := range 16 {
		for lz := range 16 {
			wx, wz := baseX+lx, baseZ+lz
			height := cols.height[lx][lz]
			kind := cols.biome[lx][lz]
			info := &biomeTable[kind]

			if height < seaLevel {
				o.growWater(c, lx, height, lz, kind, wx, wz)
				continue
			}
			// Anything growing needs the ground the terrain pass actually laid
			// down, not the biome's nominal surface: a column can end up stone
			// where a cave or a peak cut through.
			top := c.Block(uint8(lx), int16(height), uint8(lz), 0)
			o.growLand(c, lx, height, lz, wx, wz, kind, info, top, maxY)
		}
	}
	// The lake runs after the ground cover so it can clear whatever grew
	// where it lands.
	o.growLavaLake(c, pos, cols)
	o.decorateLushCaves(c, pos, cols)
}

// growLand places a tree or ground cover on one land column.
func (o *Overworld) growLand(c *chunk.Chunk, lx, height, lz, wx, wz int, kind biomeKind, info *biomeInfo, top uint32, maxY int) {
	above := height + 1
	if above > maxY {
		return
	}

	// Desert has its own cover, since cactus and dead bush stand on sand rather
	// than soil and nothing else grows there.
	if kind == bDesert {
		o.growDesert(c, lx, above, lz, wx, wz, top)
		return
	}

	soil := top == o.grass || top == o.podzol || top == o.snow
	if !soil {
		return
	}

	if info.tree != treeNone && info.treeChance > 0 {
		margin := treeSpecs[info.tree].radius
		// A tree too close to the edge would have to write into the next chunk.
		if lx >= margin && lx < 16-margin && lz >= margin && lz < 16-margin {
			if h := o.hash(wx, height, wz, 0x7ee5); int(h%10000) < info.treeChance {
				o.growTree(c, info.tree, lx, above, lz, maxY, h>>16)
				return
			}
		}
	}

	h := o.hash(wx, height, wz, 0x91a3)
	roll := int(h % 10000)
	switch {
	case roll < info.flowerChance:
		palette := info.flowers
		if len(palette) == 0 {
			palette = defaultFlowers
		}
		f := palette[int(h>>20)%len(palette)]
		setIfInside(c, lx, above, lz, o.plants.flower[f])
	case roll < info.flowerChance+info.grassChance:
		// Taiga and jungle undergrowth is ferns; everywhere else it is grass,
		// occasionally the two-block kind.
		switch {
		case (kind == bTaiga || kind == bSnowyTaiga || kind == bJungle || kind == bGrove) && h>>24%3 == 0:
			setIfInside(c, lx, above, lz, o.plants.fern)
		case h>>28%9 == 0 && above+1 <= maxY:
			setIfInside(c, lx, above, lz, o.plants.tallGrassLower)
			setIfInside(c, lx, above+1, lz, o.plants.tallGrassUpper)
		default:
			setIfInside(c, lx, above, lz, o.plants.shortGrass)
		}
	}
}

// growDesert places cactus and dead bush on sand.
func (o *Overworld) growDesert(c *chunk.Chunk, lx, above, lz, wx, wz int, top uint32) {
	if top != o.sand {
		return
	}
	h := o.hash(wx, above, wz, 0x0cac)
	switch roll := int(h % 10000); {
	case roll < 55:
		// Cactus needs its own column clear, so it is kept off the chunk edge
		// where the neighbouring blocks cannot be checked.
		if lx < 1 || lx > 14 || lz < 1 || lz > 14 {
			return
		}
		for i := range 1 + int(h>>20%3) {
			setIfInside(c, lx, above+i, lz, o.plants.cactus)
		}
	case roll < 300:
		setIfInside(c, lx, above, lz, o.plants.deadBush)
	}
}

// growWater places what grows under and on the water of a submerged column.
//
// Dragonfly has no seagrass block, so the underwater cover is kelp in the
// deeper columns and lily pads on the calm shallow ones.
func (o *Overworld) growWater(c *chunk.Chunk, lx, height, lz int, kind biomeKind, wx, wz int) {
	depth := seaLevel - height
	if depth < 1 {
		return
	}
	h := o.hash(wx, height, wz, 0x4e19)
	roll := int(h % 10000)

	switch {
	case depth >= 4 && roll < 900:
		// A stalk of kelp rising most of the way to the surface.
		stalk := 2 + int(h>>20)%(depth-1)
		for i := range stalk {
			o.setWaterlogged(c, lx, height+1+i, lz, o.plants.kelp)
		}
	case kind == bRiver && depth <= 3 && roll < 260:
		// The pad floats on the surface, so it goes in the air block above the
		// top water block rather than replacing it.
		setIfInside(c, lx, seaLevel+1, lz, o.plants.lilyPad)
	}
}

// setWaterlogged places a plant that lives inside water.
//
// A chunk holds two block layers per position: the block itself and the liquid
// around it. Writing only the first layer removes the water the plant was
// standing in, and kelp with no water around it breaks the moment the chunk is
// ticked. The water has to be written back on the second layer.
func (o *Overworld) setWaterlogged(c *chunk.Chunk, lx, y, lz int, rid uint32) {
	if lx < 0 || lx > 15 || lz < 0 || lz > 15 {
		return
	}
	r := c.Range()
	if y < r.Min() || y > r.Max() {
		return
	}
	c.SetBlock(uint8(lx), int16(y), uint8(lz), 0, rid)
	c.SetBlock(uint8(lx), int16(y), uint8(lz), 1, o.water)
}

// decorateLushCaves carpets the floor of the rare overgrown cave pockets.
//
// It walks each column from the surface down looking for a solid block with air
// above it inside a lush region, which is the definition of a cave floor.
func (o *Overworld) decorateLushCaves(c *chunk.Chunk, pos chunkPos, cols *columns) {
	baseX, baseZ := pos.x*16, pos.z*16
	minY := int(c.Range().Min())

	for lx := range 16 {
		for lz := range 16 {
			wx, wz := baseX+lx, baseZ+lz
			top := minInt(cols.height[lx][lz], 24)
			for y := top; y > minY+1; y-- {
				if !o.lushAt(wx, y, wz) {
					continue
				}
				if c.Block(uint8(lx), int16(y), uint8(lz), 0) != o.air {
					continue
				}
				below := c.Block(uint8(lx), int16(y-1), uint8(lz), 0)
				if below != o.stone && below != o.deepslate {
					continue
				}
				h := o.hash(wx, y, wz, 0x1a55)
				if int(h%100) < 55 {
					setIfInside(c, lx, y, lz, o.plants.mossCarpet)
				} else if int(h%100) < 70 {
					setIfInside(c, lx, y, lz, o.plants.fern)
				}
			}
		}
	}
}

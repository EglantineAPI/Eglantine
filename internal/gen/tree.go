package gen

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// treeKind selects both the wood used and the shape grown.
type treeKind uint8

const (
	treeNone treeKind = iota
	treeOak
	treeBirch
	treeSpruce
	treeJungle
	treeAcacia
	treeDarkOak
	treeSwampOak
	treeKindCount
)

// treeShape is how a kind is drawn.
type treeShape uint8

const (
	// shapeRound is the blob canopy of oak, birch and dark oak.
	shapeRound treeShape = iota
	// shapeConifer is the stack of shrinking rings that makes a spruce.
	shapeConifer
	// shapeCanopy is a bare trunk with a wide flat crown, used for jungle and
	// acacia.
	shapeCanopy
)

// treeSpec is the static description of a tree kind.
type treeSpec struct {
	wood   block.WoodType
	leaves block.LeavesType
	shape  treeShape

	// minTrunk and trunkVary bound the trunk height.
	minTrunk, trunkVary int
	// radius is the widest canopy radius, and doubles as the margin a tree
	// needs from the chunk edge.
	radius int
	// lean tilts the trunk by one block partway up, which is what stops a
	// stand of acacia from looking like a row of posts.
	lean bool
}

var treeSpecs = [treeKindCount]treeSpec{
	treeOak:      {wood: block.OakWood(), leaves: block.OakLeaves(), shape: shapeRound, minTrunk: 4, trunkVary: 3, radius: 2},
	treeBirch:    {wood: block.BirchWood(), leaves: block.BirchLeaves(), shape: shapeRound, minTrunk: 5, trunkVary: 3, radius: 2},
	treeSpruce:   {wood: block.SpruceWood(), leaves: block.SpruceLeaves(), shape: shapeConifer, minTrunk: 6, trunkVary: 5, radius: 2},
	treeJungle:   {wood: block.JungleWood(), leaves: block.JungleLeaves(), shape: shapeCanopy, minTrunk: 8, trunkVary: 6, radius: 3},
	treeAcacia:   {wood: block.AcaciaWood(), leaves: block.AcaciaLeaves(), shape: shapeCanopy, minTrunk: 4, trunkVary: 3, radius: 3, lean: true},
	treeDarkOak:  {wood: block.DarkOakWood(), leaves: block.DarkOakLeaves(), shape: shapeRound, minTrunk: 5, trunkVary: 3, radius: 3},
	treeSwampOak: {wood: block.OakWood(), leaves: block.OakLeaves(), shape: shapeCanopy, minTrunk: 5, trunkVary: 2, radius: 3},
}

// resolveTrees fills in the runtime IDs for every tree kind.
func (o *Overworld) resolveTrees(rid func(world.Block) uint32) {
	for k := treeKind(1); k < treeKindCount; k++ {
		spec := treeSpecs[k]
		o.treeLog[k] = rid(block.Log{Wood: spec.wood, Axis: cube.Y})
		o.treeLeaves[k] = rid(block.Leaves{Type: spec.leaves, Persistent: true})
	}
}

// growTree writes a tree of the kind passed into the chunk. baseY is the first
// block above the ground.
func (o *Overworld) growTree(c *chunk.Chunk, kind treeKind, lx, baseY, lz, maxY int, h uint64) {
	spec := treeSpecs[kind]
	trunk := spec.minTrunk + int(h%uint64(spec.trunkVary))
	if baseY+trunk+3 > maxY {
		return
	}
	logRID, leafRID := o.treeLog[kind], o.treeLeaves[kind]

	// A leaning trunk shifts sideways halfway up.
	shiftAt, dx, dz := trunk+1, 0, 0
	if spec.lean {
		shiftAt = trunk / 2
		switch h % 4 {
		case 0:
			dx = 1
		case 1:
			dx = -1
		case 2:
			dz = 1
		default:
			dz = -1
		}
	}

	tipX, tipZ := lx, lz
	for dy := range trunk {
		if dy == shiftAt {
			tipX, tipZ = tipX+dx, tipZ+dz
		}
		setIfInside(c, tipX, baseY+dy, tipZ, logRID)
	}

	top := baseY + trunk
	switch spec.shape {
	case shapeConifer:
		o.canopyConifer(c, tipX, top, tipZ, spec.radius, leafRID, maxY)
	case shapeCanopy:
		o.canopyFlat(c, tipX, top, tipZ, spec.radius, leafRID, maxY)
	default:
		o.canopyRound(c, tipX, top, tipZ, spec.radius, leafRID, maxY)
	}
}

// canopyRound draws two wide rings under a narrow cap.
func (o *Overworld) canopyRound(c *chunk.Chunk, lx, top, lz, radius int, leafRID uint32, maxY int) {
	for dy := -2; dy <= 1; dy++ {
		r := radius
		if dy >= 0 {
			r = radius - 1
		}
		if r < 1 {
			r = 1
		}
		o.ring(c, lx, top+dy, lz, r, leafRID, maxY, dy >= 0)
	}
	o.setIfAir(c, lx, top+2, lz, leafRID)
}

// canopyConifer stacks rings that shrink with height into a cone.
func (o *Overworld) canopyConifer(c *chunk.Chunk, lx, top, lz, radius int, leafRID uint32, maxY int) {
	// Start well down the trunk: a spruce is leafy for most of its height.
	for dy, r := -5, radius; dy <= 1; dy++ {
		// Rings alternate between the full radius and one less, which is what
		// gives a spruce its stepped silhouette.
		cur := r
		if (dy+5)%2 == 1 {
			cur = r - 1
		}
		if dy >= 0 {
			cur = 1
		}
		if cur < 1 {
			cur = 1
		}
		o.ring(c, lx, top+dy, lz, cur, leafRID, maxY, true)
	}
	o.setIfAir(c, lx, top+2, lz, leafRID)
}

// canopyFlat draws a wide crown two blocks deep, for jungle and acacia.
func (o *Overworld) canopyFlat(c *chunk.Chunk, lx, top, lz, radius int, leafRID uint32, maxY int) {
	o.ring(c, lx, top, lz, radius, leafRID, maxY, false)
	o.ring(c, lx, top+1, lz, radius-1, leafRID, maxY, false)
}

// ring fills a square of leaves of the radius passed, optionally clipping the
// corners to round it off.
func (o *Overworld) ring(c *chunk.Chunk, lx, y, lz, radius int, leafRID uint32, maxY int, round bool) {
	if y > maxY {
		return
	}
	for dx := -radius; dx <= radius; dx++ {
		for dz := -radius; dz <= radius; dz++ {
			if round && absI(dx) == radius && absI(dz) == radius {
				continue
			}
			o.setIfAir(c, lx+dx, y, lz+dz, leafRID)
		}
	}
}

// setIfAir writes a block only where there is nothing already.
//
// Canopies reach over neighbouring columns, and those columns may be lower. If
// leaves overwrote whatever they landed on, a tree on a slope would replace the
// ground of the column below it and anything planted there would end up
// standing on leaves.
func (o *Overworld) setIfAir(c *chunk.Chunk, lx, y, lz int, rid uint32) {
	if lx < 0 || lx > 15 || lz < 0 || lz > 15 {
		return
	}
	r := c.Range()
	if y < r.Min() || y > r.Max() {
		return
	}
	if c.Block(uint8(lx), int16(y), uint8(lz), 0) != o.air {
		return
	}
	c.SetBlock(uint8(lx), int16(y), uint8(lz), 0, rid)
}

// setIfInside writes a block, ignoring positions outside the chunk. A tree near
// a chunk border would otherwise need to write into its neighbour, which
// GenerateChunk cannot do.
func setIfInside(c *chunk.Chunk, lx, y, lz int, rid uint32) {
	if lx < 0 || lx > 15 || lz < 0 || lz > 15 {
		return
	}
	r := c.Range()
	if y < r.Min() || y > r.Max() {
		return
	}
	c.SetBlock(uint8(lx), int16(y), uint8(lz), 0, rid)
}

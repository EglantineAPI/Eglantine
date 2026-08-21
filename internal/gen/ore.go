package gen

// The overworld ore table. The numbers come from the game's own worldgen data:
// the attempt counts and height ranges from
// data/minecraft/worldgen/placed_feature/ore_*.json, and the vein sizes and
// air-exposure discard chances from data/minecraft/worldgen/feature/ore_*.json.
// They are then scaled by oreDensity, which is where this server departs from
// vanilla on purpose.

// oreKind indexes the runtime ID tables on Overworld.
type oreKind int

const (
	oreCoal oreKind = iota
	oreIron
	oreCopper
	oreGold
	oreRedstone
	oreDiamond
	oreLapis
	oreEmerald
	oreKindCount
)

// buildOreField assembles the overworld ore table.
//
// Two vanilla batches are deliberately absent: copper_large and gold_extra
// apply only to badlands, a biome this generator does not produce.
func (o *Overworld) buildOreField() *veinField {
	spec := func(k oreKind, count, size int, dist heightDist, minY, maxY int, discard float64) veinSpec {
		return veinSpec{
			block: o.ore[k], deep: o.oreDeepslate[k],
			count: count, size: size, dist: dist,
			minY: minY, maxY: maxY, discardOnAir: discard,
		}
	}
	emerald := spec(oreEmerald, 100, 3, distTriangle, -16, 480, 0)
	emerald.mountainOnly = true

	return &veinField{
		seed: o.seed,
		air:  o.air,
		hostRock: func(rid uint32) (bool, bool) {
			// Ore replaces stone only. Anything else means the vein ran into
			// air, a cave, water or the surface layers.
			switch rid {
			case o.stone:
				return false, true
			case o.deepslate:
				return true, true
			}
			return false, false
		},
		specs: []veinSpec{
			// Coal: very common, and the upper batch is why surface coal is easy.
			spec(oreCoal, 30, 17, distUniform, 136, 320, 0),
			spec(oreCoal, 20, 17, distTriangle, 0, 192, 0.5),

			// Iron: three batches. The 90-attempt upper one covers mountains,
			// where most of it lands above the terrain and places nothing.
			spec(oreIron, 90, 9, distTriangle, 80, 384, 0),
			spec(oreIron, 10, 9, distTriangle, -24, 56, 0),
			spec(oreIron, 10, 4, distUniform, -64, 72, 0),

			spec(oreCopper, 16, 10, distTriangle, -16, 112, 0),

			spec(oreGold, 4, 9, distTriangle, -64, 32, 0.5),
			spec(oreGold, 1, 9, distUniform, -64, -48, 0.5),

			spec(oreRedstone, 4, 8, distUniform, -64, 15, 0),
			// above_bottom -32..32 around a bottom of -64.
			spec(oreRedstone, 8, 8, distTriangle, -96, -32, 0),

			// Diamond: three of the four batches are ranged above_bottom
			// -80..80, which is -144..16 for a world whose bottom is -64. Half
			// of that distribution falls below the world and is thrown away,
			// and that is most of why diamond is rare and why what survives
			// clusters just above bedrock. Clamping the range to the buildable
			// area instead would multiply the yield several times over.
			spec(oreDiamond, 7, 4, distTriangle, -144, 16, 0.5),
			spec(oreDiamond, 2, 8, distUniform, -64, -4, 0.5),
			spec(oreDiamond, 1, 12, distTriangle, -144, 16, 0.7),
			spec(oreDiamond, 4, 8, distTriangle, -144, 16, 1.0),

			spec(oreLapis, 2, 7, distTriangle, -32, 32, 0),
			spec(oreLapis, 4, 7, distUniform, -64, 64, 1.0),

			emerald,
		},
	}
}

package gen

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
)

// biomeKind indexes the biome table. Selecting on a small integer keeps the
// per-column work down: a chunk asks for its biome 256 times, and every later
// pass looks the answer up again.
type biomeKind uint8

const (
	bOcean biomeKind = iota
	bDeepOcean
	bRiver
	bBeach
	bStonyShore
	bPlains
	bSunflowerPlains
	bForest
	bBirchForest
	bDarkForest
	bTaiga
	bSnowyTaiga
	bSnowyPlains
	bDesert
	bSavanna
	bJungle
	bSwamp
	bMeadow
	bGrove
	bSnowySlopes
	bWindsweptHills
	bStonyPeaks
	bFrozenPeaks
	biomeKindCount
)

// biomeInfo is the static description of a biome: what the client is told it
// is, what its ground is made of, and what grows on it.
type biomeInfo struct {
	biome  world.Biome
	top    world.Block
	filler world.Block

	tree treeKind
	// treeChance, grassChance and flowerChance are per-column probabilities
	// scaled by 1e-4.
	treeChance, grassChance, flowerChance int
	// flowers is the palette this biome draws from. An empty palette means the
	// biome uses the default meadow mix.
	flowers []block.FlowerType
}

// biomeTable describes every biome the overworld can produce.
//
// The tree and plant chances here are what make biomes read differently from
// the ground: a forest is not a plains with a different name, it is a plains
// with twenty times the trees.
var biomeTable [biomeKindCount]biomeInfo

func init() {
	grass, dirt := block.Grass{}, block.Dirt{}
	sand := block.Sand{}
	gravel := block.Gravel{}
	stone := block.Stone{}
	snow := block.Snow{}
	podzol := block.Podzol{}

	biomeTable = [biomeKindCount]biomeInfo{
		bOcean:     {biome: biome.Ocean{}, top: gravel, filler: dirt},
		bDeepOcean: {biome: biome.DeepOcean{}, top: gravel, filler: gravel},
		bRiver:     {biome: biome.River{}, top: sand, filler: dirt},
		bBeach:     {biome: biome.Beach{}, top: sand, filler: sand},
		// A stony shore is the cliff-footed coast that appears where high
		// ground runs straight into the sea, instead of a sand beach.
		bStonyShore: {biome: biome.StonyShore{}, top: stone, filler: stone},

		bPlains: {biome: biome.Plains{}, top: grass, filler: dirt,
			tree: treeOak, treeChance: 60, grassChance: 1800, flowerChance: 160},
		bSunflowerPlains: {biome: biome.SunflowerPlains{}, top: grass, filler: dirt,
			tree: treeOak, treeChance: 40, grassChance: 2000, flowerChance: 900,
			flowers: []block.FlowerType{block.Dandelion(), block.Poppy()}},

		bForest: {biome: biome.Forest{}, top: grass, filler: dirt,
			tree: treeOak, treeChance: 1000, grassChance: 1200, flowerChance: 220},
		bBirchForest: {biome: biome.BirchForest{}, top: grass, filler: dirt,
			tree: treeBirch, treeChance: 1000, grassChance: 1200, flowerChance: 220,
			flowers: []block.FlowerType{block.LilyOfTheValley(), block.OxeyeDaisy()}},
		bDarkForest: {biome: biome.DarkForest{}, top: grass, filler: dirt,
			tree: treeDarkOak, treeChance: 1300, grassChance: 700, flowerChance: 60},

		bTaiga: {biome: biome.Taiga{}, top: grass, filler: dirt,
			tree: treeSpruce, treeChance: 900, grassChance: 900, flowerChance: 40},
		bSnowyTaiga: {biome: biome.SnowyTaiga{}, top: snow, filler: dirt,
			tree: treeSpruce, treeChance: 700, grassChance: 200},
		bSnowyPlains: {biome: biome.SnowyPlains{}, top: snow, filler: dirt,
			tree: treeSpruce, treeChance: 30, grassChance: 150},

		// Desert grows no grass at all; cactus and dead bush are placed
		// separately because they need a sand check rather than a soil one.
		bDesert: {biome: biome.Desert{}, top: sand, filler: sand},
		bSavanna: {biome: biome.Savanna{}, top: grass, filler: dirt,
			tree: treeAcacia, treeChance: 120, grassChance: 2400, flowerChance: 20},
		bJungle: {biome: biome.Jungle{}, top: grass, filler: dirt,
			tree: treeJungle, treeChance: 1400, grassChance: 2600, flowerChance: 90},
		bSwamp: {biome: biome.Swamp{}, top: grass, filler: dirt,
			tree: treeSwampOak, treeChance: 350, grassChance: 1400, flowerChance: 40,
			flowers: []block.FlowerType{block.BlueOrchid()}},

		// The mountain band. Meadow and grove are why a mountain is green
		// partway up instead of bare stone from sea level.
		bMeadow: {biome: biome.Meadow{}, top: grass, filler: dirt,
			tree: treeOak, treeChance: 15, grassChance: 2600, flowerChance: 1400},
		bGrove: {biome: biome.Grove{}, top: snow, filler: podzol,
			tree: treeSpruce, treeChance: 800, grassChance: 300},
		bSnowySlopes: {biome: biome.SnowySlopes{}, top: snow, filler: stone},
		bWindsweptHills: {biome: biome.WindsweptHills{}, top: grass, filler: dirt,
			tree: treeSpruce, treeChance: 120, grassChance: 900, flowerChance: 40},
		bStonyPeaks:  {biome: biome.StonyPeaks{}, top: stone, filler: stone},
		bFrozenPeaks: {biome: biome.FrozenPeaks{}, top: snow, filler: stone},
	}
}

// defaultFlowers is the palette for biomes that do not name their own.
var defaultFlowers = []block.FlowerType{
	block.Dandelion(), block.Poppy(), block.AzureBluet(),
	block.RedTulip(), block.OrangeTulip(), block.WhiteTulip(),
	block.Cornflower(),
}

// beachTop is the y above sea level up to which a coast is still shore rather
// than inland. Keeping it at one block is what stopped most of the landmass
// from being classified as beach.
const beachTop = seaLevel + 1

// steepInland reports whether the ground rises sharply within a few blocks of
// a shore column, which is what separates a cliff foot from a beach.
func (o *Overworld) steepInland(x, z, height int) bool {
	for _, d := range [4][2]int{{7, 0}, {-7, 0}, {0, 7}, {0, -7}} {
		if h, _ := o.heightAt(x+d[0], z+d[1]); h > height+8 {
			return true
		}
	}
	return false
}

// biomeAt picks the biome for a column.
//
// Ocean, river, beach and the mountain bands are decided by height, because
// they describe where a column sits rather than its climate. Only what is left
// over is chosen from temperature and rainfall.
func (o *Overworld) biomeAt(x, z, height int, river float64) biomeKind {
	if height < seaLevel {
		if river > 0.30 {
			return bRiver
		}
		if height < seaLevel-22 {
			return bDeepOcean
		}
		return bOcean
	}

	temp := o.climate.fbm(float64(x), float64(z), 3, 1.0/620.0, 0.5) * 1.7
	rain := o.humid.fbm(float64(x)+500, float64(z)+500, 3, 1.0/540.0, 0.5) * 1.7

	if height <= beachTop {
		if temp < -0.45 {
			return bSnowyPlains
		}
		// A shore with high ground right behind it is a stony cliff foot
		// rather than a sand beach. Beaches are well under a percent of the
		// world, so the two extra height solves here cost almost nothing.
		if o.steepInland(x, z, height) {
			return bStonyShore
		}
		return bBeach
	}

	// Mountain bands. Each is a slice of height rather than a climate cell, so
	// a mountain reads as green low down, then snowy, then bare rock only at
	// the very top.
	switch {
	case height > seaLevel+95:
		if temp < 0.1 {
			return bFrozenPeaks
		}
		return bStonyPeaks
	case height > seaLevel+72:
		if temp < 0.15 {
			return bSnowySlopes
		}
		return bWindsweptHills
	case height > seaLevel+48:
		switch {
		case temp < -0.15:
			return bGrove
		case rain > 0.2:
			return bWindsweptHills
		default:
			return bMeadow
		}
	}

	// Swamps are low, wet and warm. Restricting them to nearly flat ground just
	// above the waterline is what keeps them at the edges of lakes and rivers
	// rather than partway up a hill.
	if height <= seaLevel+8 && rain > 0.12 && temp > -0.05 {
		return bSwamp
	}

	switch {
	case temp < -0.55:
		return bSnowyPlains
	case temp < -0.2:
		if rain > 0.1 {
			return bSnowyTaiga
		}
		return bTaiga
	case temp > 0.33 && rain < -0.05:
		return bDesert
	case temp > 0.18 && rain > 0.28:
		return bJungle
	case temp > 0.2 && rain < -0.25:
		return bSavanna
	case rain > 0.5:
		return bDarkForest
	case rain > 0.2:
		if temp < 0.05 {
			return bBirchForest
		}
		return bForest
	case rain < -0.35:
		return bSavanna
	case rain > 0.05:
		return bForest
	case temp > 0.3 && rain > -0.1:
		return bSunflowerPlains
	default:
		return bPlains
	}
}

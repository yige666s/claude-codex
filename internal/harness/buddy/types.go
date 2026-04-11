package buddy

type Rarity string
type Species string
type Eye string
type Hat string
type StatName string

const (
	RarityCommon    Rarity = "common"
	RarityUncommon  Rarity = "uncommon"
	RarityRare      Rarity = "rare"
	RarityEpic      Rarity = "epic"
	RarityLegendary Rarity = "legendary"
)

var Rarities = []Rarity{RarityCommon, RarityUncommon, RarityRare, RarityEpic, RarityLegendary}
var SpeciesList = []Species{"duck", "goose", "blob", "cat", "dragon", "octopus", "owl", "penguin", "turtle", "snail", "ghost", "axolotl", "capybara", "cactus", "robot", "rabbit", "mushroom", "chonk"}
var Eyes = []Eye{"·", "✦", "×", "◉", "@", "°"}
var Hats = []Hat{"none", "crown", "tophat", "propeller", "halo", "wizard", "beanie", "tinyduck"}
var StatNames = []StatName{"DEBUGGING", "PATIENCE", "CHAOS", "WISDOM", "SNARK"}

type CompanionBones struct {
	Rarity  Rarity
	Species Species
	Eye     Eye
	Hat     Hat
	Shiny   bool
	Stats   map[StatName]int
}

type CompanionSoul struct {
	Name        string
	Personality string
}

type Companion struct {
	CompanionBones
	CompanionSoul
	HatchedAt int64
}

var RarityWeights = map[Rarity]int{
	RarityCommon: 60, RarityUncommon: 25, RarityRare: 10, RarityEpic: 4, RarityLegendary: 1,
}

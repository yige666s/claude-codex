package buddy

import "hash/fnv"

const salt = "friend-2026-401"

type CompanionRoll struct {
	Bones           CompanionBones
	InspirationSeed int
}

func RollWithSeed(seed string) CompanionRoll {
	rng := mulberry32(hashString(seed))
	rarity := rollRarity(rng)
	stats := map[StatName]int{}
	for _, name := range StatNames {
		stats[name] = 5 + int(rng()*95)
	}
	return CompanionRoll{
		Bones: CompanionBones{
			Rarity:  rarity,
			Species: pickSpecies(rng),
			Eye:     Eyes[int(rng()*float64(len(Eyes)))],
			Hat:     Hats[int(rng()*float64(len(Hats)))],
			Shiny:   rng() < 0.01,
			Stats:   stats,
		},
		InspirationSeed: int(rng() * 1e9),
	}
}

func Roll(userID string) CompanionRoll {
	return RollWithSeed(userID + salt)
}

func CompanionIntroText(name string, species string) string {
	return "# Companion\n\nA small " + species + " named " + name + " sits beside the user's input box and occasionally comments in a speech bubble. You're not " + name + " - it's a separate watcher.\n\nWhen the user addresses " + name + " directly (by name), its bubble will answer. Your job in that moment is to stay out of the way: respond in ONE line or less, or just answer any part of the message meant for you."
}

func hashString(value string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(value))
	return h.Sum32()
}

func mulberry32(seed uint32) func() float64 {
	a := seed
	return func() float64 {
		a |= 0
		a = a + 0x6d2b79f5
		t := a
		t = uint32(int32(t^(t>>15)) * int32(1|t))
		t = t + uint32(int32(t^(t>>7))*int32(61|t))
		return float64((t^(t>>14))>>0) / 4294967296.0
	}
}

func rollRarity(rng func() float64) Rarity {
	total := 0
	for _, rarity := range Rarities {
		total += RarityWeights[rarity]
	}
	roll := rng() * float64(total)
	for _, rarity := range Rarities {
		roll -= float64(RarityWeights[rarity])
		if roll < 0 {
			return rarity
		}
	}
	return RarityCommon
}

func pickSpecies(rng func() float64) Species {
	return SpeciesList[int(rng()*float64(len(SpeciesList)))]
}

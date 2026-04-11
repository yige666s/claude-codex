package vim

type Cursor interface {
	Left() Cursor
	Right() Cursor
	DownLogicalLine() Cursor
	UpLogicalLine() Cursor
	Down() Cursor
	Up() Cursor
	NextVimWord() Cursor
	PrevVimWord() Cursor
	EndOfVimWord() Cursor
	NextWORD() Cursor
	PrevWORD() Cursor
	EndOfWORD() Cursor
	StartOfLogicalLine() Cursor
	FirstNonBlankInLogicalLine() Cursor
	EndOfLogicalLine() Cursor
	StartOfLastLine() Cursor
	Equals(Cursor) bool
}

func ResolveMotion(key string, cursor Cursor, count int) Cursor {
	result := cursor
	for i := 0; i < count; i++ {
		next := applySingleMotion(key, result)
		if next.Equals(result) {
			break
		}
		result = next
	}
	return result
}

func IsInclusiveMotion(key string) bool {
	return key == "e" || key == "E" || key == "$"
}

func IsLinewiseMotion(key string) bool {
	return key == "j" || key == "k" || key == "G" || key == "gg"
}

func applySingleMotion(key string, cursor Cursor) Cursor {
	switch key {
	case "h":
		return cursor.Left()
	case "l":
		return cursor.Right()
	case "j":
		return cursor.DownLogicalLine()
	case "k":
		return cursor.UpLogicalLine()
	case "gj":
		return cursor.Down()
	case "gk":
		return cursor.Up()
	case "w":
		return cursor.NextVimWord()
	case "b":
		return cursor.PrevVimWord()
	case "e":
		return cursor.EndOfVimWord()
	case "W":
		return cursor.NextWORD()
	case "B":
		return cursor.PrevWORD()
	case "E":
		return cursor.EndOfWORD()
	case "0":
		return cursor.StartOfLogicalLine()
	case "^":
		return cursor.FirstNonBlankInLogicalLine()
	case "$":
		return cursor.EndOfLogicalLine()
	case "G":
		return cursor.StartOfLastLine()
	default:
		return cursor
	}
}

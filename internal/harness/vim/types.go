package vim

type Operator string
type FindType string
type TextObjScope string

const (
	OperatorDelete Operator = "delete"
	OperatorChange Operator = "change"
	OperatorYank   Operator = "yank"

	ScopeInner  TextObjScope = "inner"
	ScopeAround TextObjScope = "around"
)

type VimState struct {
	Mode         string
	InsertedText string
	Command      CommandState
}

type CommandState struct {
	Type   string
	Op     Operator
	Count  int
	Digits string
	Find   FindType
	Dir    string
	Scope  TextObjScope
}

type PersistentState struct {
	Register           string
	RegisterIsLinewise bool
}

var Operators = map[string]Operator{
	"d": OperatorDelete,
	"c": OperatorChange,
	"y": OperatorYank,
}

var SimpleMotions = map[string]bool{
	"h": true, "l": true, "j": true, "k": true,
	"w": true, "b": true, "e": true, "W": true, "B": true, "E": true,
	"0": true, "^": true, "$": true,
}

var FindKeys = map[string]bool{"f": true, "F": true, "t": true, "T": true}

var TextObjScopes = map[string]TextObjScope{
	"i": ScopeInner,
	"a": ScopeAround,
}

var TextObjTypes = map[string]bool{
	"w": true, "W": true, "\"": true, "'": true, "`": true,
	"(": true, ")": true, "b": true, "[": true, "]": true,
	"{": true, "}": true, "B": true, "<": true, ">": true,
}

const MaxVimCount = 10000

func CreateInitialVimState() VimState {
	return VimState{
		Mode:         "INSERT",
		InsertedText: "",
	}
}

func CreateInitialPersistentState() PersistentState {
	return PersistentState{}
}

func IsOperatorKey(key string) bool {
	_, ok := Operators[key]
	return ok
}

func IsTextObjScopeKey(key string) bool {
	_, ok := TextObjScopes[key]
	return ok
}

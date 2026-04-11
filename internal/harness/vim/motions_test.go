package vim

import "testing"

type fakeCursor struct{ pos int }

func (c fakeCursor) Left() Cursor                       { return fakeCursor{pos: c.pos - 1} }
func (c fakeCursor) Right() Cursor                      { return fakeCursor{pos: c.pos + 1} }
func (c fakeCursor) DownLogicalLine() Cursor            { return fakeCursor{pos: c.pos + 10} }
func (c fakeCursor) UpLogicalLine() Cursor              { return fakeCursor{pos: c.pos - 10} }
func (c fakeCursor) Down() Cursor                       { return fakeCursor{pos: c.pos + 1} }
func (c fakeCursor) Up() Cursor                         { return fakeCursor{pos: c.pos - 1} }
func (c fakeCursor) NextVimWord() Cursor                { return fakeCursor{pos: c.pos + 2} }
func (c fakeCursor) PrevVimWord() Cursor                { return fakeCursor{pos: c.pos - 2} }
func (c fakeCursor) EndOfVimWord() Cursor               { return fakeCursor{pos: c.pos + 3} }
func (c fakeCursor) NextWORD() Cursor                   { return fakeCursor{pos: c.pos + 4} }
func (c fakeCursor) PrevWORD() Cursor                   { return fakeCursor{pos: c.pos - 4} }
func (c fakeCursor) EndOfWORD() Cursor                  { return fakeCursor{pos: c.pos + 5} }
func (c fakeCursor) StartOfLogicalLine() Cursor         { return fakeCursor{pos: 0} }
func (c fakeCursor) FirstNonBlankInLogicalLine() Cursor { return fakeCursor{pos: 1} }
func (c fakeCursor) EndOfLogicalLine() Cursor           { return fakeCursor{pos: 99} }
func (c fakeCursor) StartOfLastLine() Cursor            { return fakeCursor{pos: 1000} }
func (c fakeCursor) Equals(other Cursor) bool           { return c.pos == other.(fakeCursor).pos }

func TestResolveMotionAndHelpers(t *testing.T) {
	got := ResolveMotion("w", fakeCursor{pos: 0}, 2).(fakeCursor)
	if got.pos != 4 {
		t.Fatalf("unexpected cursor %#v", got)
	}
	if !IsInclusiveMotion("$") || !IsLinewiseMotion("G") || !IsOperatorKey("d") || !IsTextObjScopeKey("i") {
		t.Fatal("expected vim helpers to classify known keys")
	}
}

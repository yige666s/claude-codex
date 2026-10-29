package server

// Terminal represents a terminal instance with PTY
type Terminal struct {
	Token      string
	UserID     string
	Cols       int
	Rows       int
	Scrollback *ScrollbackBuffer
}

// NewTerminal creates a new terminal instance
func NewTerminal(token, userID string, cols, rows int, scrollbackSize int) *Terminal {
	return &Terminal{
		Token:      token,
		UserID:     userID,
		Cols:       cols,
		Rows:       rows,
		Scrollback: NewScrollbackBuffer(scrollbackSize),
	}
}

// Resize changes the terminal dimensions
func (t *Terminal) Resize(cols, rows int) {
	t.Cols = cols
	t.Rows = rows
}

// Write writes data to the scrollback buffer
func (t *Terminal) Write(data []byte) (int, error) {
	return t.Scrollback.Write(data)
}

// GetScrollback returns the buffered terminal output
func (t *Terminal) GetScrollback() []byte {
	return t.Scrollback.Read()
}

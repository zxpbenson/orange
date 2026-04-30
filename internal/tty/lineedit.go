package tty

import (
	"fmt"
	"io"
	"unicode"
	"unicode/utf8"
)

// LineEditor provides readline-like line editing with cursor support.
// It manages a rune buffer and cursor position, emitting ANSI escape
// sequences to keep the terminal display in sync.
type LineEditor struct {
	runes  []rune
	pos    int       // cursor position as rune index (0 .. len(runes))
	writer io.Writer // terminal output (os.Stdout)
}

// NewLineEditor creates a new LineEditor that writes terminal output to w.
func NewLineEditor(w io.Writer) *LineEditor {
	return &LineEditor{writer: w}
}

// runeWidth returns the terminal column width of a rune.
// CJK and fullwidth characters occupy 2 columns, others occupy 1.
func runeWidth(r rune) int {
	if r == 0 {
		return 0
	}
	// CJK Unified Ideographs and common fullwidth ranges
	if unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hangul, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		(r >= 0xFF01 && r <= 0xFF60) || // Fullwidth Forms
		(r >= 0xFFE0 && r <= 0xFFE6) { // Fullwidth Signs
		return 2
	}
	// Emoji that are typically rendered wide
	if r >= 0x1F300 && r <= 0x1FAD6 {
		return 2
	}
	return 1
}

// sliceWidth returns the total terminal column width of runes[i:j].
func sliceWidth(runes []rune, i, j int) int {
	w := 0
	for _, r := range runes[i:j] {
		w += runeWidth(r)
	}
	return w
}

// moveCursorLeft sends ANSI sequence to move cursor left by cols columns.
func (le *LineEditor) moveCursorLeft(cols int) {
	if cols > 0 {
		fmt.Fprintf(le.writer, "\x1b[%dD", cols)
	}
}

// moveCursorRight sends ANSI sequence to move cursor right by cols columns.
func (le *LineEditor) moveCursorRight(cols int) {
	if cols > 0 {
		fmt.Fprintf(le.writer, "\x1b[%dC", cols)
	}
}

// eraseToEOL sends ESC[K to clear from cursor to end of line.
func (le *LineEditor) eraseToEOL() {
	le.writer.Write([]byte("\x1b[K"))
}

// writeRune writes a single rune as UTF-8 bytes to the terminal.
func (le *LineEditor) writeRune(r rune) {
	var buf [utf8.UTFMax]byte
	n := utf8.EncodeRune(buf[:], r)
	le.writer.Write(buf[:n])
}

// redrawFromCursor rewrites all runes from current pos to end,
// clears any leftover characters, then moves cursor back to pos.
func (le *LineEditor) redrawFromCursor() {
	le.eraseToEOL()
	tail := le.runes[le.pos:]
	for _, r := range tail {
		le.writeRune(r)
	}
	// Move cursor back to pos
	tailW := sliceWidth(le.runes, le.pos, len(le.runes))
	le.moveCursorLeft(tailW)
}

// Insert inserts a rune at the current cursor position.
func (le *LineEditor) Insert(r rune) {
	// Splice rune into slice at pos
	le.runes = append(le.runes, 0)
	copy(le.runes[le.pos+1:], le.runes[le.pos:])
	le.runes[le.pos] = r
	le.pos++

	if le.pos == len(le.runes) {
		// Append at end — just write the rune
		le.writeRune(r)
	} else {
		// Mid-line insertion: write rune then redraw tail
		le.writeRune(r)
		le.redrawFromCursor()
	}
}

// Backspace deletes the rune before the cursor.
func (le *LineEditor) Backspace() {
	if le.pos == 0 {
		return
	}
	deleted := le.runes[le.pos-1]
	le.runes = append(le.runes[:le.pos-1], le.runes[le.pos:]...)
	le.pos--
	w := runeWidth(deleted)
	le.moveCursorLeft(w)
	le.redrawFromCursor()
}

// Delete removes the rune at the cursor (forward delete).
func (le *LineEditor) Delete() {
	if le.pos >= len(le.runes) {
		return
	}
	le.runes = append(le.runes[:le.pos], le.runes[le.pos+1:]...)
	le.redrawFromCursor()
}

// MoveLeft moves the cursor one rune to the left.
func (le *LineEditor) MoveLeft() {
	if le.pos == 0 {
		return
	}
	le.pos--
	le.moveCursorLeft(runeWidth(le.runes[le.pos]))
}

// MoveRight moves the cursor one rune to the right.
func (le *LineEditor) MoveRight() {
	if le.pos >= len(le.runes) {
		return
	}
	le.moveCursorRight(runeWidth(le.runes[le.pos]))
	le.pos++
}

// Home moves the cursor to the beginning of the line.
func (le *LineEditor) Home() {
	if le.pos == 0 {
		return
	}
	w := sliceWidth(le.runes, 0, le.pos)
	le.moveCursorLeft(w)
	le.pos = 0
}

// End moves the cursor to the end of the line.
func (le *LineEditor) End() {
	if le.pos >= len(le.runes) {
		return
	}
	w := sliceWidth(le.runes, le.pos, len(le.runes))
	le.moveCursorRight(w)
	le.pos = len(le.runes)
}

// KillToEnd deletes from cursor to end of line (Ctrl+K).
func (le *LineEditor) KillToEnd() {
	if le.pos >= len(le.runes) {
		return
	}
	le.runes = le.runes[:le.pos]
	le.eraseToEOL()
}

// KillToStart deletes from start to cursor (Ctrl+U).
func (le *LineEditor) KillToStart() {
	if le.pos == 0 {
		return
	}
	w := sliceWidth(le.runes, 0, le.pos)
	le.runes = le.runes[le.pos:]
	le.pos = 0
	le.moveCursorLeft(w)
	le.redrawFromCursor()
}

// KillPrevWord deletes the previous word (Ctrl+W).
func (le *LineEditor) KillPrevWord() {
	if le.pos == 0 {
		return
	}
	end := le.pos
	// Skip trailing spaces
	for le.pos > 0 && le.runes[le.pos-1] == ' ' {
		le.pos--
	}
	// Skip non-spaces (the word)
	for le.pos > 0 && le.runes[le.pos-1] != ' ' {
		le.pos--
	}
	w := sliceWidth(le.runes, le.pos, end)
	le.runes = append(le.runes[:le.pos], le.runes[end:]...)
	le.moveCursorLeft(w)
	le.redrawFromCursor()
}

// String returns the current line content as a string.
func (le *LineEditor) String() string {
	return string(le.runes)
}

// Reset clears the buffer and cursor for a new input session.
func (le *LineEditor) Reset() {
	le.runes = le.runes[:0]
	le.pos = 0
}

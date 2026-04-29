# Bug Report & Fix: Vim Auto-Edit & `:00/00/00` Artifacts with Ctrl+G Shortcut

## 1. Background
The default AI assistant wake-up shortcut in Orange was initially `Ctrl+A` (ASCII `0x01`). Because `Ctrl+A` is a highly frequently used shortcut (e.g., jump to the beginning of the line in bash, or tmux prefix), it was modified to `Ctrl+G` (ASCII `0x07` / `BEL`).

## 2. The Bug
After switching the shortcut to `Ctrl+G`, opening `vim` over the Orange SSH proxy caused unexpected behavior:
- `vim` automatically entered edit/command mode.
- Artifacts like `:00/00/00` or `0000/0000/0000` were automatically typed into the editor.

## 3. Root Cause Analysis
This bug is a classic terminal ANSI escape sequence collision.

1. **Vim Color Query**: When `vim` initializes, it queries the terminal's background and foreground colors to adapt its theme. It does this by sending an OSC (Operating System Command) escape sequence to the terminal.
2. **Terminal Response**: The terminal automatically responds to this query with the current color palette. The response format typically looks like this:
   `\033]11;rgb:0000/0000/0000\007`
   *(Note: `\033` is `ESC`, and `\007` is `BEL`)*
3. **Interceptor Confusion**: Orange's `tty.go` interceptor inspects all byte streams from the terminal to detect the AI wake-up shortcut. Since the shortcut was set to `Ctrl+G` (`0x07`), the interceptor incorrectly identified the `\007` terminator of the terminal's OSC response as a user's `Ctrl+G` keystroke.
4. **Data Corruption**: The interceptor "swallowed" the `\007` byte. As a result, `vim` received an unterminated escape sequence (`\033]11;rgb:0000/0000/0000`). Without the terminator, `vim`'s input parser eventually timed out or fell back, interpreting the trailing characters (`0000/0000/0000`) as literal keyboard input from the user.

## 4. The Solution
Instead of abandoning the `Ctrl+G` shortcut or attempting to blindly parse the stream, a **state machine** was introduced into the TTY interceptor (`internal/tty/tty.go`).

The state machine tracks whether the byte stream is currently inside an ANSI escape sequence. It operates with the following states (`escState`):
- **State 0 (Normal)**: Standard user input. If `0x07` is detected, trigger the AI assistant.
- **State 1 (Seen ESC)**: Detected `\x1b` (`ESC`). Waiting to see if it's a sequence.
- **State 2 (In OSC)**: Detected `\x1b]`. We are inside an OSC sequence.
- **State 3 (In OSC, seen ESC)**: Inside an OSC sequence, but saw an `ESC` (could be part of the `ESC \` String Terminator).

**Behavioral Change**:
When the interceptor is in **State 2 (In OSC)**, and it encounters a `0x07` (`BEL`), it recognizes this as the termination of the OSC sequence, NOT a user typing `Ctrl+G`. It passes the `0x07` through to the remote session untouched, and resets the state to **State 0**.

This completely resolves the `vim` sequence collision while allowing `Ctrl+G` to function normally in standard CLI usage.

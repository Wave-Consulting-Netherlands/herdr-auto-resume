# herdr-auto-resume

> **Fork notice:** this is a fork of [`henryaj/autoclaude`](https://github.com/henryaj/autoclaude)
> (MIT, Copyright (c) 2025 Henry Stanley — see [LICENSE](LICENSE), preserved unmodified).
> The fork refactors the tmux-specific core behind a runtime abstraction and adds
> [Herdr](https://herdr.dev) support, per [BRIEF.md](BRIEF.md). Progress and design
> decisions are tracked in [PROGRESS.md](PROGRESS.md). The original tmux TUI behavior
> is preserved.

A TUI app that monitors tmux panes running [Claude Code](https://claude.com/claude-code) and automatically sends "continue" when rate limits reset.

![CI](https://github.com/henryaj/autoclaude/actions/workflows/ci.yml/badge.svg)

## The Problem

When using Claude Code heavily, you'll hit rate limits. Claude shows a message like:

```
limit reached ∙ resets 2pm
```

You then have to wait and manually type "continue" when the limit resets. If you're running multiple Claude Code sessions, this becomes tedious.

## The Solution

**autoclaude** monitors your tmux panes and automatically sends "continue" when the rate limit resets. Just enable auto-continue on the panes you want to monitor, and autoclaude handles the rest.

## Installation

### Homebrew (macOS/Linux)

```bash
brew install henryaj/tap/autoclaude
```

### From source

```bash
go install github.com/henryaj/autoclaude@latest
```

### Download binary

Download from [Releases](https://github.com/henryaj/autoclaude/releases).

## Usage

1. Start autoclaude in a tmux pane (it must run inside tmux):

```bash
autoclaude
```

2. Use arrow keys to navigate to a Claude Code pane
3. Press `tab` to enable auto-continue for that pane
4. Leave autoclaude running - it will send "continue" when rate limits reset

### Keybindings

| Key | Action |
|-----|--------|
| `←↑↓→` | Navigate between panes |
| `tab` | Toggle auto-continue for selected pane |
| `a` | Enable auto-continue for all Claude Code panes |
| `n` | Disable auto-continue for all Claude Code panes |
| `r` | Refresh pane layout |
| `h` / `?` | Show help |
| `q` | Quit |

### Pane Colors

| Color | Meaning |
|-------|---------|
| Orange | Claude Code pane (auto-continue off) |
| Green | Claude Code pane (auto-continue on) |
| Red | Rate limited (waiting for reset time) |
| Cyan | Selected pane |

## How It Works

1. autoclaude polls tmux panes every 3 seconds
2. It detects Claude Code by looking for characteristic UI patterns
3. When it finds "limit reached ∙ resets Xpm", it parses the reset time
4. When the reset time passes, it sends: `Escape` → `continue` → `Enter`
5. The pane resumes automatically

## Requirements

- tmux (autoclaude must run inside a tmux session)
- Go 1.21+ (if building from source)

## Development

```bash
# Run tests
go test ./...

# Build
go build

# Run with test pattern (for debugging without hitting rate limits)
./autoclaude --test-pattern "<<<TEST>>>"
```

## License

MIT License - see [LICENSE](LICENSE)

## Credits

Made by [Henry Stanley](https://henrystanley.com)

Built with [Claude Code](https://claude.com/claude-code)

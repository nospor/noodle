# Noodle - RSS Feed Terminal App

A keyboard-driven RSS/Atom feed reader for the terminal, built with Go and Bubble Tea.

## Features

- **Two-pane interface**: Feeds list on the left, articles on the right
- **Vim-style navigation**: Use `h`, `j`, `k`, `l` or arrow keys
- **Feed management**: Add, edit, and delete feeds
- **Article management**: Mark as read/unread, favorite, and delete articles
- **Auto-refresh**: Background worker refreshes feeds based on config
- **Browser integration**: Open articles in your default browser
- **Local storage**: SQLite database for fast access to feeds and articles

## Installation

```bash
go build -o noodle .
```

## Configuration

Configuration is stored at `~/.config/noodle/config.json`:

```json
{
  "refresh_time": 300,
  "feeds": [
    {"url": "https://example.com/feed.xml", "title": "Custom Title"},
    {"url": "https://other.com/feed.xml"}
  ]
}
```

- `refresh_time`: Time in seconds between automatic feed refreshes
- `feeds`: Array of feed objects with `url` (required) and `title` (optional)

If no title is provided, the feed's own title will be used.

## Usage

### Main View

- **Navigation**:
  - `h` / `←`: Move to feeds pane
  - `l` / `→`: Move to articles pane
  - `j` / `↓`: Move down
  - `k` / `↑`: Move up
  - `Enter`: Open selected feed/articles or enter article view

- **Feed Management**:
  - `a`: Add new feed (prompts for URL and optional title)
  - `e`: Edit selected feed
  - `d`: Delete selected feed
  - `r`: Refresh selected feed

- **Article Actions** (in articles pane):
  - `r`: Mark article as read
  - `u`: Mark article as unread
  - `f`: Toggle favorite
  - `x`: Delete article

- **Other**:
  - `q` / `Ctrl+C`: Quit

### Article View

- **Navigation**:
  - `h` / `←` / `Esc`: Return to main view
  - `l` / `→`: Move to content pane
  - `j` / `↓`: Move down
  - `k` / `↑`: Move up
  - `Enter`: View article content

- **Article Actions**:
  - `o`: Open article URL in browser
  - `r`: Mark as read
  - `u`: Mark as unread
  - `f`: Toggle favorite
  - `x`: Delete article

## Data Storage

- **Database**: SQLite database at `~/.config/noodle/noodle.db`
- **Config**: JSON file at `~/.config/noodle/config.json`

The database stores:
- Feed metadata (URL, title, last fetched time)
- Articles (title, link, content, published date, read status, favorite status)

## Building

```bash
go build -o noodle .
```

## Dependencies

- `github.com/charmbracelet/bubbletea` - TUI framework
- `github.com/charmbracelet/bubbles` - UI components
- `github.com/charmbracelet/lipgloss` - Styling
- `github.com/mmcdole/gofeed` - RSS/Atom parser
- `github.com/mattn/go-sqlite3` - SQLite driver


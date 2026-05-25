
## [unreleased]

### Miscellaneous Tasks

- Update CHANGELOG.md for v0.8.4 [skip ci]

## [0.8.4] - 2026-05-25

### Features

- *(ci)* Add CI/CD pipelines, release configurations, and Cobra CLI entry point
  - Integrate Cobra CLI for version control and daemon commands
  - Support platform-independent background process spawning for
  Windows/Unix
  - Configure GoReleaser (v2) for cross-platform binary builds
  - Configure git-cliff for automated changelog generation
  - Set up GitHub Actions workflows for testing and releases

## [0.8.3] - 2026-02-11

### Features

- Possibility to disable a feed

## [0.8.2] - 2025-12-22

### Features

- Cleaning DB to keep db file small

## [0.8.1] - 2025-11-30

### Features

- Keeping the same colors for feeds and articles

## [0.8] - 2025-11-28

### Features

- Adding distinction for feeds with not read articles

### Bug Fixes

- Removing another missed help

## [0.7] - 2025-11-28

### Features

- *(doc)* Update doc

### Bug Fixes

- Refresh feeds on app start
- [**breaking**] Changing way how fetching new articles
  Possible break change, you may need to delete existing noodle db as new
  column was added to articles table
- Now migration for new column should work

## [0.6] - 2025-11-28

### Features

- Keep feeds order the same as in config file
- Confirmation before deleting favourite article
- Delete all not-favorite articles
- Manage help box a bit better

## [0.5] - 2025-11-27

### Features

- Reorganise layout a bit
- Adding number of all articles
- Confirmation message when deleting feed
- H/L jumps pages in articles list
- Setting article as read after opening or after viewing longer than x seconds
- Add set_as_read_after to config.json
- Updating readme
- *(doc)* Updating readme
- *(doc)* Update readme
- Replace x with d in articles view

### Bug Fixes

- Lack of distinction between read/unread articles
- Deleting article should immediately delete it from the list
- Feed doesnt refresh
- Refresh articles view
- Prevent articles resurection
  When article was deleted, it resurected after refresh
- Preventing refresh message from breaking panes layout
- Stop jumping panes on first feed

### Other

- Fixing toogle favourtie functionality

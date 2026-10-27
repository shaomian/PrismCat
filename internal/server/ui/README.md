# Embedded UI placeholder

This directory must exist in a clean checkout because `internal/server/server.go`
uses `//go:embed all:ui`.

Release builds copy `web/dist` into this directory before compiling the Go
binary. The generated UI assets are ignored by Git; keep this file tracked.

For local builds, use the root `快速编译并运行.bat` (Windows) or
`快速编译并运行.command` (macOS). Both invoke `scripts/build-and-run.mjs`,
which removes stale generated assets while preserving this placeholder.

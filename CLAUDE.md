# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

helm-s3 is a Helm plugin that provides Amazon S3 protocol support for Helm chart repositories. It allows hosting private or public Helm chart repositories on Amazon S3.

## Build Commands

```bash
# Build the binary
make build

# Run tests
go test ./...

# Run linter
golangci-lint run

# Build for release
goreleaser build --snapshot --clean
```

## Project Structure

- `cmd/helm-s3/` - Main CLI application with commands (init, push, delete, reindex, download)
- `internal/awss3/` - S3 storage operations
- `internal/awsutil/` - AWS session and authentication utilities
- `internal/helmutil/` - Helm chart and index handling (supports both v2 and v3)
- `tests/e2e/` - End-to-end tests
- `hack/` - Installation and utility scripts

## Key Architecture Notes

- The plugin supports both Helm v2 and v3, with version-specific implementations in `internal/helmutil/`
- S3 operations are centralized in `internal/awss3/storage.go`
- The plugin registers as a Helm downloader for the `s3://` protocol
- Entry point is `cmd/helm-s3/main.go`

## Git Workflow

- Main branch: `master`
- Current working branch: `exa-master`

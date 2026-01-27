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

## Release Process

**IMPORTANT**: Both the git tag AND `plugin.yaml` version must be updated for a release to work correctly.

The install script (`hack/install.sh`) reads the version from `plugin.yaml` to determine which release to download from GitHub. If `plugin.yaml` version doesn't match the git tag, users will get the wrong version.

### Steps to Release

1. Update version in `plugin.yaml`:
   ```yaml
   version: "X.Y.Z"
   ```

2. Commit the change:
   ```bash
   git add plugin.yaml
   git commit -m "release: Bump version to X.Y.Z"
   ```

3. Create and push the tag:
   ```bash
   git tag vX.Y.Z
   git push origin exa-master
   git push origin vX.Y.Z
   ```

4. GitHub Actions will automatically build and publish the release.

## Configuration

### Environment Variables

- `HELM_S3_TRAVERSE_WORKERS` - Number of concurrent workers for S3 HEAD requests during reindex (default: 50)

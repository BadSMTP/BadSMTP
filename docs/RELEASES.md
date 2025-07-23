# Release Process

This document describes how to create releases for BadSMTP.

## Automated Releases

BadSMTP uses GitHub Actions to automatically build and publish releases for multiple platforms and architectures.

### Supported Platforms

The release workflow builds binaries for:

- **Linux**
  - Intel/AMD (amd64)
  - ARM (arm64)
- **macOS**
  - Intel (amd64)
  - Apple Silicon (arm64)
- **Windows**
  - Intel/AMD (amd64)

### Creating a Release

#### 1. Tag-based Releases (Recommended)

To create a new release, push a version tag:

```bash
# Create a new tag
git tag -a v1.0.0 -m "Release version 1.0.0"

# Push the tag to GitHub
git push origin v1.0.0
```

The workflow will automatically:
1. Build binaries for all platforms
2. Create compressed archives (.tar.gz for Unix, .zip for Windows)
3. Generate SHA256 checksums for all files
4. Create a GitHub Release with all artifacts attached
5. Generate release notes from commits

#### 2. Manual Releases

You can also trigger a release manually from the GitHub Actions UI:

1. Go to the "Actions" tab in your repository
2. Select the "Release" workflow
3. Click "Run workflow"
4. Enter the version tag (e.g., `v1.0.0`)
5. Click "Run workflow"

### Release Artifacts

Each release includes:

- **Binary archives**: Compressed binaries for each platform
- **Checksums**: SHA256 checksums for verification
- **Release notes**: Auto-generated from git commits

### Downloading Releases

Users can download binaries from the GitHub Releases page:

```
https://github.com/badsmtp/badsmtp/releases
```

### Installation Instructions

The release page includes installation instructions for each platform. Example for Linux:

```bash
# Download and install (Intel/AMD)
curl -L https://github.com/badsmtp/badsmtp/releases/download/v1.0.0/badsmtp-linux-amd64.tar.gz | tar xz
chmod +x badsmtp-linux-amd64
sudo mv badsmtp-linux-amd64 /usr/local/bin/badsmtp
```

### Verifying Downloads

All releases include SHA256 checksums:

```bash
# Download the checksum file
curl -LO https://github.com/badsmtp/badsmtp/releases/download/v1.0.0/badsmtp-linux-amd64.tar.gz.sha256

# Verify the download
sha256sum -c badsmtp-linux-amd64.tar.gz.sha256
```

### Version Information

The version is embedded in the binary at build time. Check the version with:

```bash
badsmtp --version
# or
badsmtp -v
```

### Pre-releases

Tags containing `alpha`, `beta`, or `rc` are automatically marked as pre-releases:

```bash
git tag -a v1.0.0-beta.1 -m "Beta release"
git push origin v1.0.0-beta.1
```

## Build Configuration

The release workflow uses:

- **Go version**: 1.25.5
- **CGO**: Disabled (for static binaries)
- **Build flags**: `-trimpath -ldflags="-s -w -X main.Version=<version>"`
  - `-s`: Strip debug symbols
  - `-w`: Strip DWARF debug info
  - `-trimpath`: Remove file path information
  - `-X main.Version`: Inject version at build time

## CI/CD Integration

Other projects can download BadSMTP binaries directly from GitHub Releases in their CI/CD pipelines:

```yaml
# Example: GitHub Actions
- name: Install BadSMTP
  run: |
    curl -L https://github.com/badsmtp/badsmtp/releases/latest/download/badsmtp-linux-amd64.tar.gz | tar xz
    chmod +x badsmtp-linux-amd64
    sudo mv badsmtp-linux-amd64 /usr/local/bin/badsmtp
```

```bash
# Example: Shell script
#!/bin/bash
VERSION="v1.0.0"
PLATFORM="linux-amd64"
URL="https://github.com/badsmtp/badsmtp/releases/download/${VERSION}/badsmtp-${PLATFORM}.tar.gz"

curl -L "$URL" | tar xz
chmod +x badsmtp-${PLATFORM}
sudo mv badsmtp-${PLATFORM} /usr/local/bin/badsmtp
```

## Troubleshooting

### Build Failures

If the release workflow fails:

1. Check the Actions logs for specific errors
2. Ensure all tests pass on the main branch
3. Verify the tag format matches `v*.*.*`
4. Check that `go.mod` and `go.sum` are up to date

### Missing Binaries

If some platform binaries are missing:

1. Check if the build job for that platform failed
2. Review the workflow matrix configuration
3. Ensure the Go version supports the target platform

### Version Not Showing

If `badsmtp --version` shows "dev":

1. Verify the binary was built with the release workflow
2. Check that ldflags were applied correctly
3. Rebuild with explicit version: `go build -ldflags="-X main.Version=v1.0.0"`
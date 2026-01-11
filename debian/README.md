# Debian Package for BadSMTP

This directory contains the Debian packaging files for BadSMTP.

## Package Information

- **Package name**: badsmtp
- **Maintainer**: BadSMTP Maintainers <badsmtp@badsmtp.com>
- **Homepage**: https://github.com/BadSMTP/BadSMTP
- **Supported architectures**: amd64, arm64, riscv64

## Building the Package Locally

### Prerequisites

```bash
sudo apt-get install debhelper devscripts build-essential golang-go
```

### Build Commands

```bash
# Build for your current architecture
dpkg-buildpackage -us -uc -b

# Build for a specific architecture (requires cross-compilation setup)
dpkg-buildpackage -us -uc -b -a arm64

# The .deb file will be created in the parent directory
cd ..
ls -lh badsmtp_*.deb
```

### Testing the Package

```bash
# Install locally built package
sudo dpkg -i badsmtp_1.0.0-1_amd64.deb

# Check for missing dependencies
sudo apt-get install -f

# Test the service
sudo systemctl start badsmtp
sudo systemctl status badsmtp
```

## Automated Builds

Packages are automatically built for amd64, arm64, and riscv64 architectures via GitHub Actions when a version tag is pushed.

### Triggering a Build

```bash
# Tag a release
git tag -a v1.0.0 -m "Release 1.0.0"
git push origin v1.0.0
```

The workflow will:
1. Build .deb packages for all supported architectures
2. Create a GitHub Release
3. Attach the .deb files to the release

## Package Contents

### Installed Files

- **Binary**: `/usr/bin/badsmtp`
- **Config**: `/etc/badsmtp/badsmtp.yaml`
- **Env defaults**: `/etc/default/badsmtp`
- **Systemd unit**: `/lib/systemd/system/badsmtp.service`
- **Data directory**: `/var/lib/badsmtp/mailbox/`

### Created User

The package creates an unprivileged system user `badsmtp:badsmtp` that runs the service.

## Configuration

### Primary Configuration File

Edit `/etc/badsmtp/badsmtp.yaml`:

```yaml
port: 2525
mailbox_dir: /var/lib/badsmtp/mailbox
listen_address: 127.0.0.1
```

### Environment Variable Overrides

Edit `/etc/default/badsmtp` to override settings without modifying the YAML file:

```bash
BADSMTP_PORT=2525
BADSMTP_LISTEN_ADDRESS=0.0.0.0
```

Environment variables take precedence over the config file.

### Applying Configuration Changes

```bash
# After editing config files
sudo systemctl restart badsmtp

# View current configuration
systemctl show badsmtp --property=Environment
```

## Service Management

```bash
# Start service
sudo systemctl start badsmtp

# Stop service
sudo systemctl stop badsmtp

# Restart service
sudo systemctl restart badsmtp

# Enable on boot
sudo systemctl enable badsmtp

# Disable on boot
sudo systemctl disable badsmtp

# Check status
sudo systemctl status badsmtp

# View logs
sudo journalctl -u badsmtp -f

# View recent logs
sudo journalctl -u badsmtp -n 50
```

## File Locations

| Path                                  | Purpose                          |
|---------------------------------------|----------------------------------|
| `/usr/bin/badsmtp`                    | Main executable                  |
| `/etc/badsmtp/badsmtp.yaml`           | Primary configuration            |
| `/etc/default/badsmtp`                | Environment variable overrides   |
| `/lib/systemd/system/badsmtp.service` | Systemd service unit             |
| `/var/lib/badsmtp/`                   | Working directory                |
| `/var/lib/badsmtp/mailbox/`           | Message storage (Maildir format) |
| `/var/lib/badsmtp/mailbox/new/`       | New messages                     |
| `/var/lib/badsmtp/mailbox/cur/`       | Current messages                 |
| `/var/lib/badsmtp/mailbox/tmp/`       | Temporary files                  |

## Upgrading

```bash
# Download new version
wget https://github.com/BadSMTP/BadSMTP/releases/download/v1.1.0/badsmtp_1.1.0-1_amd64.deb

# Install (will upgrade existing installation)
sudo dpkg -i badsmtp_1.1.0-1_amd64.deb

# Service will automatically restart
```

Configuration files (`/etc/badsmtp/badsmtp.yaml` and `/etc/default/badsmtp`) are preserved during upgrades.

## Uninstalling

```bash
# Remove package but keep configuration and data
sudo apt-get remove badsmtp

# Remove everything including configuration and data
sudo apt-get purge badsmtp
```

## Troubleshooting

### Service won't start

```bash
# Check service status
sudo systemctl status badsmtp

# Check logs for errors
sudo journalctl -u badsmtp -n 100 --no-pager

# Verify configuration
sudo -u badsmtp /usr/bin/badsmtp --config /etc/badsmtp/badsmtp.yaml --help
```

### Permission issues

```bash
# Verify ownership
ls -la /var/lib/badsmtp/

# Fix permissions if needed
sudo chown -R badsmtp:badsmtp /var/lib/badsmtp/
sudo chmod 750 /var/lib/badsmtp/
```

### Port already in use

Edit `/etc/badsmtp/badsmtp.yaml` or `/etc/default/badsmtp` to change the port:

```bash
# In /etc/default/badsmtp
BADSMTP_PORT=2526
```

Then restart:

```bash
sudo systemctl restart badsmtp
```

## Package Maintenance

### Updating the Version

1. Update `debian/changelog`:
   ```bash
   dch -v 1.1.0-1 "New upstream release"
   ```

2. Commit changes:
   ```bash
   git add debian/changelog
   git commit -m "Update changelog for version 1.1.0"
   ```

3. Tag and push:
   ```bash
   git tag -a v1.1.0 -m "Release 1.1.0"
   git push origin v1.1.0
   ```

### Linting the Package

```bash
# Build and lint in one command
dpkg-buildpackage -us -uc -b
lintian ../badsmtp_*.deb

# Fix any warnings or errors reported
```

## Contributing

When making changes to the Debian packaging:

1. Test builds locally for multiple architectures
2. Verify installation/upgrade/removal scenarios
3. Check configuration file handling
4. Ensure systemd integration works correctly
5. Update this README if adding new features

## References

- [Debian Policy Manual](https://www.debian.org/doc/debian-policy/)
- [Debian New Maintainers' Guide](https://www.debian.org/doc/manuals/maint-guide/)
- [debhelper Manual](https://manpages.debian.org/debhelper)
- [systemd Service Unit Configuration](https://www.freedesktop.org/software/systemd/man/systemd.service.html)
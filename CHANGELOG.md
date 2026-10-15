# Changelog

All notable changes to BadSMTP will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.1](https://github.com/BadSMTP/BadSMTP/compare/v0.2.0...v0.2.1) - 2026-10-15

### Fixed

- Bump staticcheck version to work with Go 1.27

## [0.2.0](https://github.com/BadSMTP/BadSMTP/compare/v0.1.7...v0.2.0) - 2026-10-15

### Added
- Backed the logging package with Go standard library `log/slog`, providing thread-safe structured logging, atomic log levels via `slog.LevelVar`, and support for stdout, remote syslog, and network logging.
- Added full logging configuration through CLI flags (`--log-level`, `--log-format`, `--log-output`, etc.), `BADSMTP_LOG_*` environment variables, and configuration files.
- Added multi-path configuration file search in order of precedence: current directory (`.`), `$HOME/.badsmtp/`, and `/etc/badsmtp/`.
- Added network operation timeouts: 10-second TLS handshake timeout (`HandshakeContext`), 2-second timeout for reverse DNS lookups (`net.Resolver`), and 2-second dial timeout for remote logging (`net.Dialer`).
- Added context-aware listener creation via `net.ListenConfig`.
- Added comprehensive unit tests for the logging package, including concurrency safety validation.

### Changed
- Upgraded Go toolchain and codebase to Go 1.27.
- Migrated configuration handling from Koanf v1 to modular Koanf v2, significantly reducing transitive dependencies and `go.sum` size.
- Hoisted per-command dispatch tables, argument validators, and state validation tables to package scope, eliminating hot-path memory allocations.
- Adopted modern Go idioms across the codebase: `any` alias, `slices.Contains`, `strings.CutPrefix`, `min`/`max`, and integer range loops.
- Modernized Dockerfile to use pinned Alpine 3.21 runtime, BuildKit cache mounts, and OCI image metadata labels.
- Upgraded dependencies: `cobra` (v1.10.2), `pflag` (v1.0.10), `fsnotify` (v1.10.1), and `x/sys` (v0.47.0).
- Upgraded `golangci-lint` configuration to v2 schema and switched to official GitHub Action in CI.
- Updated Codecov GitHub Action to v5 and simplified CI module caching.

### Fixed
- Fixed `Start()` hanging forever after graceful `Shutdown()` by closing the completion channel on all exit paths.
- Fixed CRAM-MD5 authentication mechanism to use HMAC-MD5 per RFC 2195 instead of SHA-256.
- Routed interactive AUTH I/O through the session's buffered reader, preventing command desynchronization during pipelined commands or TLS records.
- Serialized connection writes during shutdown with a per-session write mutex (`writeMu`) to eliminate data races on response queues and socket writes.
- Fixed `HasTLS()` logic to only attempt loading certificate files when explicitly configured, falling back silently to self-signed certificates without error warnings.
- Aggregated per-file removal errors in `Mailbox.Clear` using `errors.Join` rather than discarding them.
- Fixed configuration precedence and key mapping: CLI flags now properly take precedence over environment variables and configuration files, and `mapstructure` tags correctly unmarshal multi-word settings.

### Security
- Added annotations and exclusions for intentional, RFC 2195-mandated MD5 usage in CRAM-MD5 authentication to suppress false-positive security scanner warnings.

## [0.1.7](https://github.com/BadSMTP/BadSMTP/compare/v0.1.6...v0.1.7) - 2026-01-28

### Fixed
- Fixed process hang on service stop and restart by coordinating server shutdown termination via a completion channel (`s.done`).

## [0.1.6](https://github.com/BadSMTP/BadSMTP/compare/v0.1.5...v0.1.6) - 2026-01-28

### Added
- Added package linting workflow (`.github/workflows/package-lint.yml`) running `lintian` on Debian packages across `amd64`, `arm64`, and `riscv64` architectures.

### Changed
- Switched Debian package building to use `dh_installsystemd` in `debian/rules` for proper systemd service lifecycle management and daemon reload handling.

## [0.1.5](https://github.com/BadSMTP/BadSMTP/compare/v0.1.4...v0.1.5) - 2026-01-28

### Added
- Added project security policy in `SECURITY.md`.

### Changed
- Expanded release download and checksum verification documentation in `README.md`.
- Clarified Debian packaging documentation and build workflows.

## [0.1.4](https://github.com/BadSMTP/BadSMTP/compare/v0.1.3...v0.1.4) - 2026-01-25

### Added
- Added dash-separated label format for EHLO hostname-based capability configuration (e.g., `size10000-no8bit-authplain.example.com`), ensuring DNS compliance and compatibility with wildcard TLS certificates.
- Added dynamic capability toggles via EHLO hostname: `size<digits>`, `nosize`, `no8bit`, `nopipelining`, `nostarttls`, `nochunking`, `nosmtputf8`, `noenhancedstatuscodes`, `authplain`, `authlogin`, `authcram`, `authoauth`, `noauth`, and `reject`.
- Added pluggable extension interfaces: `SMTPExtension` for custom SMTP commands, `SessionWriter` for session interaction, and `CapabilityParser` for custom EHLO label parsing (e.g., auth token extraction).
- Added dynamic command response delay interval configuration via EHLO hostname parameter (`dlay<ms>`).
- Added UFW firewall application profile (`debian/ufw/badsmtp.profile`) and documentation (`debian/README.UFW.md`).
- Added project repository link in SMTP greeting banner.
- Added "latest" download links for release assets.

### Changed
- Replaced large port delay ranges with compact 25xxx port blocks: greeting delays on 25200..25209 and drop delays on 25600..25609.
- Increased default maximum message size to 10 MB (10,485,760 bytes).
- Restricted CI release builds to execute only on tagged commits.

### Fixed
- Stopped advertising STARTTLS in EHLO capabilities when the connection is already encrypted.

## [0.1.3](https://github.com/BadSMTP/BadSMTP/compare/v0.1.2...v0.1.3) - 2026-01-11

### Fixed
- Added `contents: write` permission to GitHub Actions release workflow to allow publishing release assets and packages.

## [0.1.2](https://github.com/BadSMTP/BadSMTP/compare/v0.1.1...v0.1.2) - 2026-01-11

### Added
- Added Debian packaging support for `amd64`, `arm64`, and `riscv64` architectures (`debian/control`, `debian/rules`, `debian/badsmtp.service`, `debian/badsmtp.default`).
- Added RISC-V 64-bit (`linux-riscv64`) target to cross-platform release builds.

### Fixed
- Fixed cross-compilation toolchain paths, packaging dependencies (`wget`), and disabled `dwz` debug optimization during Debian package builds.

## [0.1.1](https://github.com/BadSMTP/BadSMTP/compare/v0.1.0...v0.1.1) - 2026-01-10

### Added
- Added `--interface` / `-i` command-line flag and configuration setting to bind listeners to a specific network interface or IP address.

### Changed
- Added documentation notes regarding listening ports and removed unused benchmark files.

## [0.1.0](https://github.com/BadSMTP/BadSMTP/releases/tag/v0.1.0) - 2026-01-09

### Added
- Initial release of BadSMTP test server.
- SMTP state machine enforcing proper command sequence (`StateGreeting`, `StateHelo`, `StateMail`, `StateRcpt`, `StateData`).
- Multiple authentication mechanisms: PLAIN, LOGIN, CRAM-MD5, CRAM-SHA256, and XOAUTH2.
- Pattern-based authentication handling (`goodauth` for success, `badauth` for 535 rejection).
- Port-based behaviour simulation for greeting delays and connection dropping.
- Verb-prefixed error simulation across commands (`mail`, `rcpt`, `data`, `rset`, `quit`, `starttls`, `noop`, `helo`/`ehlo`) with enhanced status code support.
- File-based message storage in Maildir format (`mailbox/` with `new/`, `cur/`, and `tmp/`).
- TLS and STARTTLS support with automatic fallback to generated self-signed certificates.
- Unified configuration loading using Koanf supporting CLI flags, environment variables, and configuration files.

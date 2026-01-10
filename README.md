# BadSMTP — The Reliably Unreliable Mail Server

[![BadSMTP Logo](img/badsmtp_logo.webp)](https://badsmtp.com)

BadSMTP is an SMTP server designed for testing SMTP clients in development and CI/CD pipelines. It provides configurable error responses, behaviours, and comprehensive logging to allow thorough testing of SMTP client implementations.

For the most part, all error behaviours can be triggered entirely from the client side, so you don't need to configure anything on the server. It can also be used as a simple delivery agent, storing sent messages in a folder for later inspection or integration with test suites. It's fast enough that you can use it for load testing the performance of email clients. BadSMTP does for SMTP what [badssl.com](https://badssl.com/) does for TLS.

## Features

- **No configuration required**: Runs out of the box with sensible defaults, no complicated setup needed
- **Address-based error triggers**: Trigger specific SMTP error codes using special email addresses
- **Port-based connection behaviours**: Different ports trigger specific connection behaviours, e.g. timeouts, delays, connection drops
- **Enhanced error code support**: Work with or without [RFC2034](https://www.rfc-editor.org/rfc/rfc2034) enhanced error codes
- **Authentication testing**: Support for multiple AUTH mechanisms with configurable outcomes
- **Message storage**: Optionally save successfully submitted messages to disk
- **TLS/STARTTLS support**: Full TLS encryption with self-signed certificate generation
- **Structured logging**: JSON and text logging with external service support (syslog, TCP, UDP)
- **Extensible architecture**: Pluggable interfaces for custom authentication, storage, rate limiting, API integration, and more
- **Non-delivery guarantee**: Does not relay or deliver email to end recipients, perfect for testing and CI
- **Lightweight and portable**: Single binary with minimal dependencies
- **Cross-platform**: Builds for Linux, macOS, and Windows
- **Superlative performance**: Optimised for fast startup, low resource usage, and high concurrency thanks to the Go runtime

## Installation

### Option 1: Download Pre-built Binary (Recommended)

Download the latest release for your platform from [the Releases page](https://github.com/BadSMTP/BadSMTP/issues/releases).

**Linux (Intel/AMD):**

```bash
curl -L https://github.com/BadSMTP/BadSMTP/releases/latest/download/badsmtp-linux-amd64.tar.gz | tar xz
chmod +x badsmtp-linux-amd64
sudo mv badsmtp-linux-amd64 /usr/local/bin/badsmtp
```

**macOS (Apple Silicon or Intel):**

```bash
curl -L https://github.com/BadSMTP/BadSMTP/releases/latest/download/badsmtp-darwin-arm64.tar.gz | tar xz
chmod +x badsmtp-darwin-arm64
sudo mv badsmtp-darwin-arm64 /usr/local/bin/badsmtp
```

**Windows:**
Download the appropriate `badsmtp-windows-amd64.zip` from releases and add it to your `PATH`.

### Option 2: Build from Source

1. **Build the server:**

   ```bash
   make init
   make build
   ```

2. **Run the server:**

   ```bash
   ./badsmtp
   ```

3. **Test with telnet:**

   ```bash
   telnet localhost 2525
   ```

## Configuration

BadSMTP uses [Koanf](https://pkg.go.dev/github.com/knadh/koanf) for merging configuration from flags, environment variables, and config files. The precedence is:

1. Command-line switches
2. Environment variables (prefixed with `BADSMTP_`)
3. Configuration file (`badsmtp.yaml` / `badsmtp.yml` / `badsmtp.json`)
4. Built-in defaults (applied by the server)

Koanf loads keys using snake_case by default when you map them into a Go struct with `mapstructure` tags. This repository uses snake_case keys in YAML and environment variables to match the `server.Config` mapping.

### YAML / File configuration (example)

```yaml
# Server settings (snake_case keys)
port: 2525
mailbox_dir: ./mailbox

tls_port: 25465
starttls_port: 25587

greeting_delay_port_start: 3000
command_delay_port_start: 4000
drop_delay_port_start: 5000
immediate_drop_port: 6000
```

### Environment variables

Environment variables are read with the `BADSMTP_` prefix and use uppercase snake_case. For example:

```bash
export BADSMTP_PORT=2525
export BADSMTP_MAILBOX_DIR=./mailbox
export BADSMTP_TLS_PORT=25465
```

### Command-line switches

Switches are registered on the root command and follow the same logical names, typically with dashes. Command line options have the highest precedence of any config method, overriding config files or env vars. Example:

```bash
./badsmtp --port 2525 --mailbox ./mailbox
```

## Testing Features

### Listening Address

The IP address (and indirectly, the interface) the server binds to is configurable via the `listen_address` config key (YAML/JSON) or the `BADSMTP_LISTEN_ADDRESS` environment variable. The default is `127.0.0.1`. Set it to `0.0.0.0` to bind all interfaces, or specify a particular IP to restrict the listener.

> [!CAUTION]
> In CI and development environments, avoid using anything other than localhost to prevent exposing the test server externally.

### Port-based Connection Behaviours

BadSMTP uses only ports above 1024 by default to avoid requiring elevated privileges. You can change the listening port using the `port` configuration key or `BADSMTP_PORT` environment variable. If you want to run it on conventional ports (25, 465, 587), you'll need to either run it as a privileged user, or configure a firewall to reroute the traffic from those ports to its high-numbered ones.

Use different ports to simulate various connection issues:

- **Ports 3000-3099**: Greeting delay of `(port - 3000) * 10` seconds
  - Port 3000: No delay
  - Port 3001: 10-second delay
  - Port 3099: 990-second delay
- **Ports 4000-4099**: Command delay of `(port - 4000) * 10` seconds
  - Applied after each SMTP command
- **Ports 5000-5099**: Drop connection after `(port - 5000) * 10` seconds
  - Port 5000: Drop immediately after delay
  - Port 5001: Drop after 10 seconds
- **Port 6000**: Drop connection immediately without greeting

### TLS and STARTTLS Support

BadSMTP provides TLS support for encrypted SMTP testing, however, if you want to test TLS error handling more comprehensively, use [badssl.com](httpt://badssl.com):

#### SMTPS (Implicit TLS)

- **Port 25465** (default): TLS connection from the start
- Automatically generates self-signed certificates
- Supports custom certificates via `TLS_CERT_FILE` and `TLS_KEY_FILE`

Test with OpenSSL:

```bash
openssl s_client -connect localhost:25465 -crlf
```

#### SMTP+STARTTLS (Explicit TLS)

- **Port 25587** (default): Start with plain text, upgrade to TLS
- Use `STARTTLS` command after `EHLO`
- Supports hostname-based dynamic certificate generation

Test with OpenSSL:

```bash
openssl s_client -connect localhost:25587 -starttls smtp -crlf
```

### Requesting Specific Error Responses

This is BadSMTP's primary feature.

BadSMTP uses a **verb-prefixed pattern** for error simulation. The `MAIL FROM` address can contain error patterns for almost any SMTP command, and errors are triggered when the corresponding command is later executed.

#### Pattern Format

- **Basic**: `<verb><code>@example.com`, for example `mail421@example.com` – Triggers standard 3-digit error code.
- **Enhanced**: `<verb><code>_<subcode>@example.com`, for example `mail550_5.1.1@example.com` – Triggers standard 3-digit error code plus specific [RFC2034](https://www.rfc-editor.org/rfc/rfc2034) enhanced status code.

> [!TIP]
> Enhanced error codes are not supported if the `ENHANCEDSTATUSCODES` capability has been disabled and is not advertised in the `EHLO` response.

#### Supported Commands

The domain part is ignored; so long as it's a syntactically valid domain, it will be accepted.

| Verb       | Command     | Example                     | Result                                                           |
|------------|-------------|-----------------------------|------------------------------------------------------------------|
| `mail`     | `MAIL FROM` | `mail452@example.net`       | `452 Requested action not taken: insufficient system storage`    |
| `rcpt`     | `RCPT TO`   | `rcpt550_5.1.1@example.net` | `550 5.1.1 Requested action not taken: mailbox unavailable`      |
| `data`     | `DATA`      | `data552@example.net`       | `552 Requested mail action aborted: exceeded storage allocation` |
| `rset`     | `RSET`      | `rset421@example.net`       | `421 Service not available, closing transmission channel`        |
| `quit`     | `QUIT`      | `quit421@example.net`       | `421 Service not available, closing transmission channel`        |
| `starttls` | `STARTTLS`  | `starttls454@example.net`   | `454 Command not implemented`                                    |
| `noop`     | `NOOP`      | `noop421@example.net`       | `421 Service not available, closing transmission channel`        |
| `helo`     | `HELO/EHLO` | `helo500.example.com`       | `500 Syntax error, command unrecognised`                         |

All error requests provoke a single error response per transaction, except `RCPT TO`, where a message being sent to multiple addresses can request a different response for each one.

Commands are still subject to the SMTP specification, so if you submit a malformed `RCPT TO` command, it will return a `501 Syntax error in parameters` before checking for any error patterns. Similarly, if you submit an out-of sequence command, such as `RCPT TO` before `MAIL FROM`, you'll receive a normal SMTP error rather than a requested error code.

It's also possible to generate errors in response to an initial `HELO`/`EHLO` command by encoding the error code in the hostname parameter:

```
EHLO helo500.example.com
```

> [!NOTE]
> If a message submission triggers an error, it will not be written to a mailbox (if one is configured), though details of the error (deliberate or otherwise) will be logged.

#### Usage Examples

Trigger a 421 (server shutting down) error in response to a `RSET` verb:

```bash
MAIL FROM:<rset421@example.com>
# Later when client sends RSET:
RSET
# Returns: 421 Service not available, closing transmission channel
```

Trigger `RCPT` error with enhanced status code:

```bash
MAIL FROM:<rcpt550_5.1.1@example.com>
RCPT TO:<user@example.com>
# Returns: 550 5.1.1 Requested action not taken: mailbox unavailable
```

Errors from multiple `RCPT` addresses:

```bash
# This address will not work
MAIL FROM:<rcpt552@example.net>
# Instead, don't request an error here:
MAIL FROM:<user@example.net>
# Each RCPT TO can request its own error pattern:
RCPT TO:<rcpt550_5.1.1@example.net>
RCPT TO:<rcpt551_5.7.1@example.net>
# or not request an error at all
RCPT TO:<nobody@example.net>
```

`HELO`/`EHLO` errors** (use hostname, not email):

```bash
EHLO helo500.example.com
# Returns: 500 Syntax error, command unrecognised
```

### Authentication Testing

BadSMTP supports multiple `AUTH` mechanisms:

- `PLAIN`
- `LOGIN`
- `CRAM-MD5`
- `CRAM-SHA256`
- `XOAUTH2`

but *they are all fake* – there are no real accounts or credentials. See notes below about enabling specific auth mechanisms.

#### Authentication Outcomes

Whether authentication succeeds depends on the username pattern used:

- **Success**: Use usernames containing `goodauth` like `goodauth@example.com`.
- **Failure**: Use usernames containing `badauth` like `badauth@example.com`.

> [!NOTE]
> The authenticated username and the `MAIL FROM` address do not have to be the same (as they do on, for example, gmail).
> You can provoke a mismatch error using the address-based error code mechanism; the server will not enforce this for you.

## `EHLO` capability switching and pipelining behavior

BadSMTP advertises capabilities in its `EHLO` response and uses the `EHLO` hostname parameter as a convenient mechanism to alter behaviour for that session.

### What `EHLO` advertises

Like any *good* SMTP server, BadSMTP advertises a set of capabilities in its `EHLO` response. Common capabilities include:

- `SIZE` – indicates the maximum message size accepted by the server.
- `STARTTLS` — indicates `STARTTLS` is available for upgrading to TLS.
- `AUTH` – indicates supported authentication mechanisms (e.g., `PLAIN`, `LOGIN`, `CRAM-MD5`, `XOAUTH2`, etc.).
- `8BITMIME` – indicates support for 8-bit MIME message bodies.
- `ENHANCEDSTATUSCODES` – indicates support for [RFC2034](https://www.rfc-editor.org/rfc/rfc2034) enhanced status codes.
- `SMTPUTF8` – indicates support for UTF-8 encoded email addresses, headers, and message bodies.
- `PIPELINING` — indicates the server supports pipelined commands (batching multiple commands without waiting for a response between them).
- `CHUNKING` — indicates the server supports the `BDAT` command for chunked data submission.

All capabilities are enabled by default, but they can be disabled per-session by including keywords in the hostname parameter of the `EHLO` command:

- `EHLO noauth.example.com` — disables `AUTH` in the `EHLO` response.
- `EHLO nopipelining.example.com` — removes the `PIPELINING` capability from the response.
- `EHLO no8bit.example.com` — removes `8BITMIME`.
- `EHLO nochunking.example.com` — removes `CHUNKING` (`BDAT`).
- `EHLO nostarttls.example.com` — removes `STARTTLS` even if TLS is configured.

These hostname-driven switches are implemented as simple substring checks and are intended to make test scenarios quick to create; they do not persist between sessions. You can disable multiple capabilities by combining substrings, e.g., `EHLO noauth_nopipelining.example.com`.

As a consequence of this approach, BadSMTP does not enforce matching reverse DNS checks for `EHLO` hostnames.

#### Altering `SIZE` via hostname

You can set the size advertised in the `SIZE` capability by including a size value in the hostname:

- `EHLO size100000.example.com` — sets `SIZE` to `100000` bytes for that session.

The value may be between 1,000 (1kib) and 10,000,000 (10Mib).
#### Selecting AUTH mechanisms via hostname

By default, all auth mechanisms are available. You can switch `AUTH` off entirely using `noauth` in the hostname, but you can alternatively enable just a single mechanism:

- `EHLO authplain.example.com` — enables only `PLAIN`
- `EHLO authoauth.example.com` — enables only `XOAUTH2`
- etc.

### PIPELINING: advertised vs. actual behaviour

Advertising `PIPELINING` in `EHLO` is a statement of capability — the server only switches into queued-response pipelined mode when it detects the client is actually piping commands (the server peeks for additional immediate data after reading a command). When pipelining mode is active, responses may be queued, but they are flushed when commands that break pipelining are encountered (e.g., `DATA`, `BDAT`, `AUTH`, `STARTTLS`, `QUIT`).


### `VRFY` support

The `VRFY` command is a required part of the SMTP specification but is often disabled on production servers due to its potential for abuse. BadSMTP supports `VRFY`, allowing you to test client behaviour with it. Of course, there are no email accounts to leak, so it is safe. It responds to three preset addresses for the three possible outcomes of the `VRFY` command:

| `VRFY` Address          | Response Code | Description                             |
|-------------------------|---------------|-----------------------------------------|
| `exists@example.com`    | `250`         | User exists                             |
| `unknown@example.com`   | `551`         | User does not exist                     |
| `ambiguous@example.com` | `553`         | User ambiguous (e.g., multiple matches) |

## Building and Deployment

### Local Development

```bash
# Initialise and build
make init
make build

# Run the server
make run

# Build for all platforms
make build-all
```

### Docker Deployment

```bash
# Build Docker image
make docker

# Run in container
docker run -p 2525:2525 badsmtp:latest
```

### CI/CD Pipeline Integration

BadSMTP is designed to be easily integrated into CI pipelines:

```yaml
# Example GitHub Actions step
- name: Start BadSMTP Server
  run: |
    wget https://github.com/BadSMTP/BadSMTP/releases/latest/download/badsmtp-linux-amd64
    chmod +x badsmtp-linux-amd64
    ./badsmtp-linux-amd64 -port=2525 &
    sleep 2

- name: Run SMTP Tests
  run: |
    # Your SMTP client tests here
    python -m pytest tests/test_smtp.py
```

## Example Test Scenarios

Refer to the "Port-based Connection Behaviours" section above for port and delay semantics. The following quick examples show how to exercise those behaviours in tests.

### Test Error Handling

```python
import smtplib

# Test MAIL FROM error
try:
    server = smtplib.SMTP('localhost', 2525)
    server.mail('550@example.com')
except smtplib.SMTPResponseException as e:
    assert e.smtp_code == 550

# Test authentication failure
try:
    server.login('badauth@example.com', 'password')
except smtplib.SMTPAuthenticationError:
    print("Authentication correctly failed")
```

### Message Storage

When a mailbox is configured, successfully submitted messages (ones generating errors are not stored) are saved into a [maildir](https://en.wikipedia.org/wiki/Maildir) folder structure. Each message is stored as a separate file with a unique filename.

Maildir is widely supported and can be inspected using standard mail clients, command-line tools, or IMAP servers such.

## SMTP Command Sequence

BadSMTP enforces proper SMTP command sequencing:

- `HELO`/`EHLO` – Must be first command
- `AUTH` – (optional) – After `HELO`/`EHLO`
- `MAIL FROM` – Start mail transaction
- `RCPT TO` – Add recipients (can be repeated)
- `DATA` – Send message content, after `RCPT TO`
- `QUIT` – End session

`VRFY` and `RSET` can be used at any time after `HELO`/`EHLO`.

## Error Codes

Common SMTP error codes you can test:

- `421` – Service not available (will also drop connection)
- `450` – Requested mail action not taken
- `451` – Requested action aborted
- `452` – Requested action not taken (insufficient storage)
- `500` – Syntax error, command unrecognised
- `501` – Syntax error in parameters
- `502` – Command not implemented
- `503` – Bad sequence of commands
- `504` – Command parameter not implemented
- `535` – Authentication failed
- `550` – Requested action not taken (mailbox unavailable)
- `550_5.7.509` – Access denied, sending domain does not pass DMARC verification
- `551` – User not local
- `552` – Requested mail action aborted (exceeded storage)
- `553` – Requested action not taken (mailbox name invalid)
- `554` – Transaction failed
- `571` – Blocked - Rejected due to policy (likely spam filtering)

## Development

### Adding New Features

To add new behaviours or error conditions:

1. **Port-based behaviours**: Modify the `AnalysePortBehaviour()` function in `server/config.go`
2. **Email-based errors**: Update the email-based error extraction logic in `smtp/errors.go`
3. **Auth mechanisms**: Add new cases in the `handleAuth()` method
4. **Command handling**: Extend the `handleCommand()` method

### Testing BadSMTP

```bash
# Run unit tests
make test

# Run fast tests only (skips slow port behaviour tests)
make test-fast

# Format code
make fmt

# Lint code
make lint
```

## Dependencies

BadSMTP has minimal direct dependencies. The current top-level module dependencies are:

- `github.com/knadh/koanf` (configuration merging)
- `github.com/spf13/cobra` (command-line interface)

Most functionality is implemented using Go's standard library.

## Performance

Because by default BadSMTP does not write messages to disk, do database operations, or perform complex processing such as DKIM verification, it is capable of handling a very large number of concurrent connections with minimal resource usage. The Go runtime's efficient concurrency model allows BadSMTP to scale well under load. This makes it a good target for load testing SMTP clients. This is similar to the [smtp-sink](https://www.postfix.org/smtp-sink.1.html) server (part of postfix).

When message storage is enabled, performance will depend on the underlying filesystem and disk speed, but BadSMTP is still designed to handle high throughput scenarios.

## Troubleshooting

### Common Issues

1. **Permission denied on port 25**

   - Listening on ports below 1000 typically requires root privileges. Use unprivileged ports such as 2525 (default) or run with appropriate privileges.

1. **Connection refused**

   - Check if the port is already in use
   - Verify firewall settings
   - Ensure BadSMTP is running

1. **Messages not being saved**

   - Check the mailbox directory path and permissions
   - Verify mailbox configuration
   - Ensure there is sufficient disk space available

## Logging

BadSMTP provides comprehensive structured logging for detailed SMTP interaction tracking:

### Log Formats

- `json`: JSON, machine-readable structured logs
- `text`: Human-readable log format

### Log Outputs

- `stdout`: Standard output (default)
- `syslog`: System log integration
- `tcp`: Remote logging via TCP
- `udp`: Remote logging via UDP

### Example Log Output

```json
{
  "timestamp": "2025-01-05T10:36:21Z",
  "level": "INFO",
  "message": "SMTP connection established",
  "fields": {
    "session_id": "sess_abc123",
    "client_ip": "192.168.1.100",
    "port": 2525,
    "tls_enabled": false,
    "hostname": "badsmtp.test"
  }
}
```

### Debug Mode

Enable verbose logging by setting log level:

```bash
# Run with debug output
LOG_LEVEL=DEBUG ./badsmtp

# JSON format with external logging
LOG_FORMAT=json LOG_OUTPUT=tcp LOG_REMOTE_ADDR=logserver:514 ./badsmtp
```

## Configuration reference

### Command Line Flags

| Flag       | Description                | Default   |
|------------|----------------------------|-----------|
| `-port`    | Port to listen on          | 2525      |
| `-mailbox` | Directory to save messages | ./mailbox |

### Environment Variables

#### Basic Configuration

| Variable      | Description                | Default   |
|---------------|----------------------------|-----------|
| `PORT`        | Port to listen on          | 2525      |
| `MAILBOX_DIR` | Directory to save messages | ./mailbox |

#### TLS Configuration

| Variable        | Description                   | Default          |
|-----------------|-------------------------------|------------------|
| `TLS_CERT_FILE` | Path to TLS certificate file  | (auto-generated) |
| `TLS_KEY_FILE`  | Path to TLS private key file  | (auto-generated) |
| `TLS_PORT`      | Port for implicit TLS         | 25465            |
| `STARTTLS_PORT` | Port for STARTTLS             | 25587            |
| `TLS_HOSTNAME`  | Hostname for TLS certificates | badsmtp.test     |

#### Logging Configuration

| Variable          | Description                           | Default |
|-------------------|---------------------------------------|---------|
| `LOG_LEVEL`       | Logging level (DEBUG/INFO/WARN/ERROR) | INFO    |
| `LOG_FORMAT`      | Log format (json/text)                | json    |
| `LOG_OUTPUT`      | Log output (stdout/syslog/tcp/udp)    | stdout  |
| `LOG_REMOTE_ADDR` | Remote address for tcp/udp logging    | -       |
| `SYSLOG_FACILITY` | Syslog facility                       | mail    |

## Extensibility

BadSMTP features a pluggable architecture that allows you to extend its functionality without modifying the core codebase. The server provides several extension interfaces:

- **MessageStore**: Custom message storage backends (database, cloud storage, APIs)
- **Authenticator**: Custom authentication mechanisms (LDAP, OAuth, API tokens)
- **SessionObserver**: Monitor and react to SMTP session events
- **RateLimiter**: Custom rate limiting strategies
- **Authorizer**: Fine-grained authorization controls

### Using Extensions

Extensions are Go modules that implement the extension interfaces. To use an extension:

```go
import (
    "badsmtp/server"
    myextension "github.com/yourorg/badsmtp-extension"
)

func main() {
    config, _ := server.LoadConfig()

    // Install your custom extensions
    config.MessageStore = myextension.NewCustomMessageStore()
    config.Authenticator = myextension.NewCustomAuthenticator()
    config.Observer = myextension.NewCustomObserver()

    // Ensure defaults for any unset extensions
    config.EnsureDefaults()

    srv, _ := server.NewServer(config)
    srv.Start()
}
```

### Writing Custom Extensions

See `test-extension.go.example` for a complete example of writing a custom extension. Extensions must implement one or more of the interfaces defined in `server/extensions.go`.

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes and add tests
4. Run `make test` and `make lint`
5. Submit a pull request

## License

BadSMTP is released under the GPLv3 license.

## Support

For issues and feature requests, please use [the GitHub issue tracker](https://github.com/BadSMTP/BadSMTP/issues).

## Acknowledgements

BadSMTP was written by [Marcus Bointon](https://marcus.bointon.com). Find him on GitHub at [github.com/Synchro](https://github.com/Synchro), or on Mastodon [@Synchro@phpc.social](https://phpc.social/@Synchro).

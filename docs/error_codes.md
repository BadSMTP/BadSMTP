# Error codes available at each SMTP phase

BadSMTP ky feature is to be able to trigger SMTP errors of your choosing at every phase of the SMTP conversation. That said, it still only lets you get at the errors that are actually permitted at each phase, so for example you can't get a `550 unknown user` error during the `EHLO` phase, because that error is only valid in response to a `MAIL FROM` or `RCPT TO` command.

## EHLO/HELO

* `421` Service not available, closing transmission channel
* `500` Syntax error, command unrecognised
* `501` Syntax error in parameters or arguments
* `502` Command not implemented
* `504` Command parameter not implemented

Trigger one of these errors using the domain parameter of the `HELO`/`EHLO` command:
```shell
HELO helo500.example.com
```

## MAIL FROM

* `421` Service not available, closing transmission channel
* `451` Requested action aborted: local error in processing
* `452` Requested action not taken: insufficient system storage
* `454` TLS not available
* `500` Syntax error, command unrecognised
* `501` Syntax error in parameters or arguments
* `502` Command not implemented
* `503` Bad sequence of commands (e.g. `MAIL` sent before `EHLO`)
* `504` Command parameter not implemented
* `550` Requested action not taken: mailbox unavailable
* `551` User not local; please try <forward-path>
* `552` Requested mail action aborted: exceeded storage allocation
* `553` Parameters not recognised or not implemented

`MAIL` phase errors can be triggered using the address parameter:
```shell
MAIL FROM:<mail452@example.com>
452 Requested action not taken: insufficient system storage
```
You can specify extended error codes using the following format:
```shell
MAIL FROM:<mail550_5.7.1@example.com>
550 5.7.1 Requested action not taken: mailbox unavailable
```
This will trigger a `550 5.7.1` error response.
Note you can use any domain you like, it doesn't; only the local part of the address matters.

Any other local part will simply be accepted, and will not trigger any special error behaviour.

Other than a `421` error, all commands will leave the server in a state that expects another `MAIL FROM` command (or `RSET` or `QUIT`).

Errors triggered in the `DATA` phase are set up in the `MAIL FROM` phase; see below for details.

## RCPT TO

* `421` Service not available, closing transmission channel
* `450` Requested mail action not taken: mailbox unavailable
* `451` Requested action aborted: local error in processing
* `452` Requested action not taken: insufficient system storage
* `500` Syntax error, command unrecognised
* `501` Syntax error in parameters or arguments
* `502` Command not implemented
* `503` Bad sequence of commands (e.g. `RCPT TO` sent before `MAIL FROM`)
* `504` Command parameter not implemented
* `550` Requested action not taken: mailbox unavailable
* `551` User not local; please try <forward-path>
* `552` Requested mail action aborted: exceeded storage allocation
* `553` Parameters not recognised or not implemented

`RCPT` phase errors can be triggered using the address parameter:

```shell
RCPT TO:<rcpt452@example.com>
452 Requested action not taken: insufficient system storage
```

Other than a `421` error, all commands will leave the server in a state that expects another `RCPT TO` command (or `DATA`, `RSET` or `QUIT`).

## DATA

Because the DATA command has no parameters, errors triggered in this phase must be set up beforehand in the `MAIL FROM` phase:

```
MAIL FROM:<data552@example.com>
RCPT TO:<user@example.com>
DATA
message content
.
552 Requested mail action aborted: exceeded storage allocation
```

* `421` Service not available, closing transmission channel
* `450` Requested mail action not taken: mailbox unavailable
* `451` Requested action aborted: local error in processing
* `452` Requested action not taken: insufficient system storage
* `500` Syntax error, command unrecognised
* `501` Syntax error in parameters or arguments
* `502` Command not implemented
* `503` Bad sequence of commands (e.g. `DATA` sent before `RCPT TO`)
* `504` Command parameter not implemented
* `552` Requested mail action aborted: exceeded storage allocation
* `553` Parameters not recognised or not implemented
* `554` Transaction failed

After a data command, the server will expect a `.` on a line by itself to end the data phase.

## RSET

The RSET command resets the session state. Errors can be triggered by setting them in the `MAIL FROM` phase:

```shell
MAIL FROM:<rset421@example.com>
RCPT TO:<user@example.com>
RSET
421 Service not available, closing transmission channel
```

* `421` Service not available, closing transmission channel
* `250` OK

Extended status codes are supported:
```shell
MAIL FROM:<rset421_4.5.1@example.com>
RSET
421 4.5.1 Service not available, closing transmission channel
```

## NOOP

The NOOP command does nothing but can return errors when configured:

```shell
MAIL FROM:<noop421@example.com>
NOOP
421 Service not available, closing transmission channel
```

* `421` Service not available, closing transmission channel
* `250` OK

## STARTTLS

The STARTTLS command upgrades the connection to TLS. Errors can be triggered from `MAIL FROM`:

```shell
MAIL FROM:<starttls454@example.com>
STARTTLS
454 Command not implemented
```

* `421` Service not available, closing transmission channel
* `454` TLS not available
* `500` Syntax error, command unrecognised
* `220` Ready to start TLS (success)

Extended status codes work here too:
```shell
MAIL FROM:<starttls454_4.7.1@example.com>
STARTTLS
454 4.7.1 Command not implemented
```

## QUIT

The QUIT command terminates the session. Errors can be configured:

```shell
MAIL FROM:<quit421@example.com>
QUIT
421 Service not available, closing transmission channel
```

* `421` Service not available, closing transmission channel
* `221` Bye (success)

## Summary

All SMTP commands support verb-prefixed error patterns set via `MAIL FROM`:
- **Basic pattern**: `<verb><code>@example.com` (e.g., `mail452@example.net`)
- **Extended pattern**: `<verb><code>_<class>.<subject>.<detail>@example.com` (e.g., `rcpt550_5.1.1@example.net`)

Extended status codes support multi-digit components as per RFC 5248 (e.g., `mail550_5.7.509@example.net` for DMARC failures).

Supported verbs: `mail`, `rcpt`, `data`, `rset`, `noop`, `starttls`, `quit`, `helo`/`ehlo`


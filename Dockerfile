# BadSMTP Test Server Dockerfile

# Build stage
FROM golang:1.25-alpine AS builder

# Install git and ca-certificates (needed for downloading dependencies)
RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Note: Tests are run separately in CI pipeline
# Skipping tests in Docker build to avoid build failures

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags="-s -w" -o badsmtp .

# Runtime stage
FROM alpine:latest

# Install ca-certificates for SSL/TLS and netcat for health checks
RUN apk --no-cache add ca-certificates netcat-openbsd

# Create non-root user for security
RUN adduser -D -s /bin/sh badsmtp

WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/badsmtp .

# Copy configuration file if it exists
COPY badsmtp.env* ./

# Create mailbox directory
RUN mkdir -p /app/mailbox

# Change ownership to non-root user
RUN chown -R badsmtp:badsmtp /app

# Switch to non-root user
USER badsmtp

# Expose BadSMTP ports
# - 2525: Default SMTP port
# - 25465: Implicit TLS test port
# - 25587: STARTTLS test port
# - 3000-3099: Greeting delays
# - 4000-4099: Connection drop delays
# - 6000: Immediate connection drop
EXPOSE 2525 25465 25587 3000-3099 4000-4099 6000

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD nc -z localhost 2525 || exit 1

# Default command
ENTRYPOINT ["./badsmtp"]

# Labels for metadata
ARG BUILD_DATE=unknown
ARG VCS_REF=unknown

LABEL org.label-schema.name="BadSMTP" \
      org.label-schema.description="The Reliably Unreliable Mail Server" \
      org.label-schema.version="1.0" \
      org.label-schema.schema-version="1.0" \
      org.label-schema.build-date="undefined" \
      org.label-schema.vcs-url="https://github.com/BadSMTP/BadSMTP" \
      org.label-schema.vcs-ref="undefined"

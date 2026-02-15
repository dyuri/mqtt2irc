# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo \
    -ldflags '-extldflags "-static"' \
    -o mqtt2irc ./cmd/mqtt2irc

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 mqtt2irc && \
    adduser -D -u 1000 -G mqtt2irc mqtt2irc

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/mqtt2irc .

# Copy example config
COPY configs/config.example.yaml /etc/mqtt2irc/config.example.yaml

# Change ownership
RUN chown -R mqtt2irc:mqtt2irc /app

# Switch to non-root user
USER mqtt2irc

# Expose health check port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Run the application
ENTRYPOINT ["/app/mqtt2irc"]
CMD ["-config", "/etc/mqtt2irc/config.yaml"]

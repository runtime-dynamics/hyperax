# === Stage 1: Build React UI ===
FROM node:20-alpine AS ui-builder

WORKDIR /build/ui
COPY ui/package.json ui/package-lock.json ./
RUN npm ci --prefer-offline --no-audit

COPY ui/ ./
RUN npm run build

# === Stage 2: Build Go binary ===
FROM golang:1.25-alpine AS go-builder

# Install git for version injection via ldflags.
RUN apk add --no-cache git

WORKDIR /build

# Cache Go module downloads.
COPY go.mod go.sum ./
RUN go mod download

# Copy source and embedded UI dist from stage 1.
COPY . .
COPY --from=ui-builder /build/ui/dist ./ui/dist

# Build a statically-linked binary with no CGO.
ARG VERSION=dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /hyperax \
    ./cmd/hyperax

# === Stage 3: Runtime ===
FROM alpine:3.21

# Install ca-certificates for HTTPS calls to LLM providers and timezone data.
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S hyperax \
    && adduser -S hyperax -G hyperax

# Copy the binary.
COPY --from=go-builder /hyperax /usr/local/bin/hyperax

# Create default directories.
RUN mkdir -p /data /config \
    && chown -R hyperax:hyperax /data /config

USER hyperax

# Expose the default server port.
EXPOSE 9090

# Health check using the /health endpoint.
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:9090/health || exit 1

# Volume mounts for persistent data and configuration.
VOLUME ["/data", "/config"]

# Default entrypoint runs the server.
# Override with: docker run hyperax init
ENTRYPOINT ["hyperax"]
CMD ["serve", "--addr", ":9090"]

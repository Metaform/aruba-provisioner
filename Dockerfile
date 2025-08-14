# Build stage
FROM --platform=$BUILDPLATFORM golang:1.24 AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src

# Download dependencies early for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source (including embedded YAML files)
COPY . .

# Build static binary for the target platform
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -ldflags="-s -w" -o /out/app .

# Runtime stage (minimal, includes CA certs)
FROM gcr.io/distroless/static-debian12 AS runtime
WORKDIR /app
COPY --from=builder /out/app /app/app

# Expose API port
EXPOSE 9999

# Run as non-root
USER 65532:65532

# Default entrypoint (pass -kubeconfig at runtime if needed)
ENTRYPOINT ["/app/app", "--kubeconfig", "/app/.kube/config"]

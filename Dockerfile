# Build stage: runs on the build host and cross-compiles for the target platform,
# so multi-arch images need no emulation.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

WORKDIR /build

ARG TARGETOS
ARG TARGETARCH

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o argocd-cost-intelligence ./cmd

# Runtime stage
FROM alpine:3.24

RUN apk add --no-cache ca-certificates curl \
    && adduser -D -u 1000 app

WORKDIR /app

COPY --from=builder /build/argocd-cost-intelligence .

USER 1000

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD ["curl", "-f", "http://localhost:8080/health"]

ENTRYPOINT ["./argocd-cost-intelligence"]

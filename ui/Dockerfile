# Build stage. The bundle is the same on every platform, so it is built once, on the build
# machine's own platform, rather than under emulation for each target.
FROM --platform=$BUILDPLATFORM node:22-alpine AS builder

WORKDIR /build

# Copy package files
COPY package*.json ./

# Install dependencies
RUN npm ci

# Copy source
COPY . .

# Build plugin
RUN npm run build

# Simple HTTP server for serving static files. It is plain JavaScript, so it is also installed on
# the build platform and copied into the runtime image.
FROM --platform=$BUILDPLATFORM node:22-alpine AS server

RUN npm install -g --prefix /opt/http-server http-server@14.1.1

# Runtime stage - serve plugin from static server
FROM node:22-alpine

WORKDIR /app

COPY --from=server /opt/http-server /opt/http-server
ENV PATH="/opt/http-server/bin:${PATH}"

# Copy built plugin from builder
COPY --from=builder /build/dist ./dist

# The node user's numeric id, so Kubernetes can verify runAsNonRoot.
USER 1000

EXPOSE 3000

# Serve the built plugin
CMD ["http-server", "dist", "-p", "3000", "--cors"]

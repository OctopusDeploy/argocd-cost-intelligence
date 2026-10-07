# Build stage
FROM node:22-alpine AS builder

WORKDIR /build

# Copy package files
COPY package*.json ./

# Install dependencies
RUN npm ci

# Copy source
COPY . .

# Build plugin
RUN npm run build

# Runtime stage - serve plugin from static server
FROM node:22-alpine

WORKDIR /app

# Install simple HTTP server for serving static files
RUN npm install -g http-server@14.1.1

# Copy built plugin from builder
COPY --from=builder /build/dist ./dist

# The node user's numeric id, so Kubernetes can verify runAsNonRoot.
USER 1000

EXPOSE 3000

# Serve the built plugin
CMD ["http-server", "dist", "-p", "3000", "--cors"]

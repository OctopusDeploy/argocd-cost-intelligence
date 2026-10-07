#!/bin/bash
set -e

echo "=== ArgoCD Cost Intelligence - Development Setup ==="

# Check if Docker is installed
if ! command -v docker &> /dev/null; then
    echo "❌ Docker is not installed. Please install Docker first."
    exit 1
fi

# Check if Docker Compose is installed
if ! command -v docker-compose &> /dev/null; then
    echo "❌ Docker Compose is not installed. Please install Docker Compose first."
    exit 1
fi

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "⚠️  Go is not installed. You won't be able to build locally."
else
    echo "✓ Go is installed: $(go version)"
fi

# Create .env file from example
if [ ! -f .env ]; then
    echo "Creating .env file..."
    cp .env.example .env
    echo "✓ Created .env file (update with your values)"
fi

# Download Go modules
echo "Downloading Go modules..."
go mod download
go mod tidy
echo "✓ Go modules downloaded"

# Create directories
echo "Creating required directories..."
mkdir -p bin deploy

# Build binary
echo "Building binary..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/argocd-cost-intelligence ./cmd
echo "✓ Binary built: bin/argocd-cost-intelligence"

# Show next steps
echo ""
echo "=== Setup Complete! ==="
echo ""
echo "Next steps:"
echo "1. Start services with: docker-compose up -d"
echo "2. Check service health: curl http://localhost:8080/health"
echo "3. Test the API with sample sync notification:"
echo ""
echo "curl -X POST http://localhost:8080/api/cost-analysis/on-sync \\"
echo "  -H 'Content-Type: application/json' \\"
echo "  -H 'Authorization: Bearer local-dev-webhook-secret' \\"
echo "  -d '{"
echo "    \"applicationName\": \"test-app\","
echo "    \"applicationNamespace\": \"default\","
echo "    \"namespace\": \"default\","
echo "    \"syncTimestamp\": \"$(date -u +'%Y-%m-%dT%H:%M:%SZ')\","
echo "    \"syncResult\": \"Succeeded\""
echo "  }'"
echo ""
echo "For more information, see README.md"

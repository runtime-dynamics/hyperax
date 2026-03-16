#!/bin/zsh
set -e

echo "=== Hyperax Setup ==="

# Install Go dependencies
echo ">> Installing Go dependencies..."
go mod tidy

# Install UI dependencies
echo ">> Installing UI dependencies..."
cd ui && npm install --legacy-peer-deps && cd ..

# Build UI
echo ">> Building UI..."
cd ui && npm run build && cd ..

# Build Go binary
echo ">> Building Go binary..."
mkdir -p .build/
go build -o .build/hyperax ./cmd/hyperax

echo "=== Setup complete ==="
echo "Run 'air' to start the dev server on port 9090"

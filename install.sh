#!/bin/sh
set -e

REPO="oxforge/unlog"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  linux)  OS="linux" ;;
  darwin) OS="darwin" ;;
  *)      echo "Unsupported OS: $OS" >&2; exit 1 ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64)  ARCH="amd64" ;;
  arm64|aarch64)  ARCH="arm64" ;;
  *)              echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

# Get latest version from GitHub
if [ -z "$VERSION" ]; then
  VERSION=$(curl -sSf "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/')
  if [ -z "$VERSION" ]; then
    echo "Failed to determine latest version" >&2
    exit 1
  fi
fi

ARCHIVE="unlog_${VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/v${VERSION}/${ARCHIVE}"
CHECKSUM_URL="https://github.com/${REPO}/releases/download/v${VERSION}/checksums.txt"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

echo "Downloading unlog v${VERSION} for ${OS}/${ARCH}..."
curl -sSfL "$URL" -o "${TMPDIR}/${ARCHIVE}"
curl -sSfL "$CHECKSUM_URL" -o "${TMPDIR}/checksums.txt"

# Verify checksum
cd "$TMPDIR"
if command -v sha256sum > /dev/null 2>&1; then
  grep "$ARCHIVE" checksums.txt | sha256sum -c --quiet
elif command -v shasum > /dev/null 2>&1; then
  grep "$ARCHIVE" checksums.txt | shasum -a 256 -c --quiet
else
  echo "Warning: could not verify checksum (no sha256sum or shasum found)" >&2
fi

tar -xzf "$ARCHIVE"

# Install
if [ -w "$INSTALL_DIR" ]; then
  mv unlog "$INSTALL_DIR/unlog"
else
  echo "Installing to ${INSTALL_DIR} (requires sudo)..."
  sudo mv unlog "$INSTALL_DIR/unlog"
fi

echo "unlog v${VERSION} installed to ${INSTALL_DIR}/unlog"

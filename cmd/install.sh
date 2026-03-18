#!/usr/bin/env bash
# kli.st — install script
# Installs the kli CLI tool to ~/.local/bin (no sudo required)
# Usage: curl -sL https://kli.st/install.sh | bash

set -e

KLI_VERSION="1.0.0"
INSTALL_DIR="$HOME/.local/bin"
BINARY_NAME="kli"
BASE_URL="https://kli.st/releases"
# SHA256SUMS file is published alongside binaries at each release
SUMS_URL="${BASE_URL}/SHA256SUMS"

# ── Colours ────────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'

info()    { echo -e "  ${CYAN}→${RESET} $1"; }
success() { echo -e "  ${GREEN}✓${RESET} $1"; }
warn()    { echo -e "  ${YELLOW}!${RESET} $1"; }
die()     { echo -e "  ${RED}✗${RESET} $1"; exit 1; }

echo ""
echo -e "${BOLD}kli.st installer v${KLI_VERSION}${RESET}"
echo "  kli — DevOps CLI command search"
echo ""

# ── Detect OS / Arch ───────────────────────────────────────────────────────────
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$OS" in
    linux)  OS_KEY="linux" ;;
    darwin) OS_KEY="darwin" ;;
    *)      die "Unsupported OS: $OS" ;;
esac

case "$ARCH" in
    x86_64|amd64)  ARCH_KEY="amd64" ;;
    aarch64|arm64) ARCH_KEY="arm64" ;;
    *)             die "Unsupported architecture: $ARCH" ;;
esac

BINARY_FILENAME="kli-${OS_KEY}-${ARCH_KEY}"
BINARY_URL="${BASE_URL}/${BINARY_FILENAME}"
info "Detected: ${OS_KEY}/${ARCH_KEY}"

# ── Check for curl or wget ─────────────────────────────────────────────────────
if command -v curl &>/dev/null; then
    DOWNLOAD_CMD="curl -sL --fail"
    DOWNLOAD_OUT="-o"
elif command -v wget &>/dev/null; then
    DOWNLOAD_CMD="wget -q"
    DOWNLOAD_OUT="-O"
else
    die "curl or wget is required but not found"
fi

# ── Check for sha256sum or shasum ──────────────────────────────────────────────
SHA_CMD=""
if command -v sha256sum &>/dev/null; then
    SHA_CMD="sha256sum"
elif command -v shasum &>/dev/null; then
    SHA_CMD="shasum -a 256"
else
    warn "sha256sum not found — skipping integrity check (not recommended)"
fi

# ── Create install dir ─────────────────────────────────────────────────────────
if [ ! -d "$INSTALL_DIR" ]; then
    mkdir -p "$INSTALL_DIR"
    info "Created $INSTALL_DIR"
fi

# ── Download binary ────────────────────────────────────────────────────────────
DEST="${INSTALL_DIR}/${BINARY_NAME}"
TMPFILE="$(mktemp)"
TMPSUM="$(mktemp)"

# Ensure temp files are cleaned up on exit
trap 'rm -f "$TMPFILE" "$TMPSUM"' EXIT

info "Downloading kli ${KLI_VERSION}..."

if ! $DOWNLOAD_CMD "$BINARY_URL" $DOWNLOAD_OUT "$TMPFILE" 2>/dev/null; then
    die "Download failed from $BINARY_URL"
fi

# ── Verify SHA256 integrity ────────────────────────────────────────────────────
# Fetches SHA256SUMS from server and checks the specific binary line.
# Compatible with Bash 3.2+ (macOS default) — no associative arrays used.
if [ -n "$SHA_CMD" ]; then
    if $DOWNLOAD_CMD "$SUMS_URL" $DOWNLOAD_OUT "$TMPSUM" 2>/dev/null; then
        EXPECTED="$(grep "${BINARY_FILENAME}$" "$TMPSUM" | awk '{print $1}')"
        if [ -n "$EXPECTED" ]; then
            ACTUAL="$($SHA_CMD "$TMPFILE" | awk '{print $1}')"
            if [ "$ACTUAL" != "$EXPECTED" ]; then
                die "Integrity check FAILED — binary may be corrupted or tampered with"
            fi
            success "Integrity check passed"
        else
            warn "No checksum found for ${BINARY_FILENAME} — skipping verification"
        fi
    else
        warn "Could not fetch SHA256SUMS — skipping integrity check"
    fi
fi

# ── Install ────────────────────────────────────────────────────────────────────
mv "$TMPFILE" "$DEST"
chmod +x "$DEST"
success "Installed to $DEST"

# ── PATH check ─────────────────────────────────────────────────────────────────
SHELL_NAME="$(basename "$SHELL")"
PROFILE=""

case "$SHELL_NAME" in
    bash) PROFILE="$HOME/.bashrc" ;;
    zsh)  PROFILE="$HOME/.zshrc" ;;
    fish) PROFILE="$HOME/.config/fish/config.fish" ;;
esac

if ! echo "$PATH" | grep -q "$INSTALL_DIR"; then
    warn "$INSTALL_DIR is not in your PATH"
    if [ -n "$PROFILE" ]; then
        echo "" >> "$PROFILE"
        echo "# kli.st" >> "$PROFILE"
        echo "export PATH=\"\$HOME/.local/bin:\$PATH\"" >> "$PROFILE"
        success "Added to $PROFILE"
        warn "Run: source $PROFILE  (or open a new terminal)"
    else
        warn "Add this to your shell profile:"
        echo ""
        echo "    export PATH=\"\$HOME/.local/bin:\$PATH\""
        echo ""
    fi
else
    success "$INSTALL_DIR is already in PATH"
fi

# ── Done ───────────────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}${GREEN}Installation complete!${RESET}"
echo ""
echo "  Try it:"
echo -e "  ${CYAN}kli search docker${RESET}"
echo -e "  ${CYAN}kli search \"list containers\"${RESET}"
echo -e "  ${CYAN}kli --help${RESET}"
echo ""

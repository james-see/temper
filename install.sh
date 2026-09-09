#!/usr/bin/env bash
# temper installer - idempotent install/updates
# Supports macOS (Homebrew) and Linux (Homebrew or go install)

set -euo pipefail

TEMPER_REPO="github.com/james-see/temper"
BREW_TAP="james-see/tap"
BREW_FORMULA="temper"

# Detect OS and package manager
use_brew=false
if [[ "$(uname -s)" == "Darwin" ]]; then
    # macOS
    if command -v brew >/dev/null 2>&1; then
        use_brew=true
    fi
else
    # Linux or other
    if command -v brew >/dev/null 2>&1; then
        use_brew=true
    fi
fi

if $use_brew; then
    echo "Using Homebrew to install/update temper..."
    # Ensure tap exists
    if ! brew tap | grep -q "^$BREW_TAP\$"; then
        echo "Tapping $BREW_TAP..."
        brew tap $BREW_TAP
    fi
    # Install or upgrade
    if brew list --versions $BREW_FORMULA >/dev/null 2>&1; then
        echo "Upgrading $BREW_FORMULA..."
        brew upgrade $BREW_FORMULA
    else
        echo "Installing $BREW_FORMULA..."
        brew install $BREW_FORMULA
    fi
else
    echo "Homebrew not found, using go install..."
    # Ensure Go is available
    if ! command -v go >/dev/null 2>&1; then
        echo "Error: go command not found. Please install Go or use Homebrew." >&2
        exit 1
    fi
    # Install/update via go install (always gets latest)
    echo "Installing temper via go install..."
    go install $TEMPER_REPO/cmd/temper@latest
    echo "Installation complete. Ensure \$(go env GOPATH)/bin is in your PATH."
fi

echo "temper installation/upgrade finished."
echo "Run 'temper --help' to get started."
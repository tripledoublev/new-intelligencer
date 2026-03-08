#!/bin/bash
# Load Bluesky credentials from environment or macOS Keychain
#
# Setup (one-time):
#   security add-generic-password -s "bsky-agent" -a "handle" -w "your.handle.bsky.social"
#   security add-generic-password -s "bsky-agent" -a "password" -w "xxxx-xxxx-xxxx-xxxx"

if [ -f "./.env" ]; then
    set -a
    source "./.env"
    set +a
fi

if [ -z "$BSKY_HANDLE" ] && command -v security >/dev/null 2>&1; then
    export BSKY_HANDLE=$(security find-generic-password -s "bsky-agent" -a "handle" -w 2>/dev/null)
fi

if [ -z "$BSKY_PASSWORD" ] && command -v security >/dev/null 2>&1; then
    export BSKY_PASSWORD=$(security find-generic-password -s "bsky-agent" -a "password" -w 2>/dev/null)
fi

if [ -z "$BSKY_HANDLE" ] || [ -z "$BSKY_PASSWORD" ]; then
    echo "Error: Bluesky credentials not found in environment or keychain" >&2
    echo "Set BSKY_HANDLE and BSKY_PASSWORD, or on macOS set up keychain entries using:" >&2
    echo '  security add-generic-password -s "bsky-agent" -a "handle" -w "your.handle.bsky.social"' >&2
    echo '  security add-generic-password -s "bsky-agent" -a "password" -w "your-app-password"' >&2
    exit 1
fi

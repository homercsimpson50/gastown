#!/bin/sh
set -e

# Re-apply git/dolt config on every start so env var changes take effect
# even when the home volume already exists from a previous run.
if [ -n "$GIT_USER" ] && [ -n "$GIT_EMAIL" ]; then
    git config --global user.name "$GIT_USER"
    git config --global user.email "$GIT_EMAIL"
    git config --global credential.helper store
    dolt config --global --add user.name "$GIT_USER"
    dolt config --global --add user.email "$GIT_EMAIL"
fi

# --- D-Bus + GNOME Keyring for Claude Code credential storage ---
# Claude Code on Linux uses libsecret, which needs a running keyring daemon.
if command -v gnome-keyring-daemon >/dev/null 2>&1; then
    # Start a private D-Bus session if none exists
    if [ -z "$DBUS_SESSION_BUS_ADDRESS" ]; then
        eval "$(dbus-launch --sh-syntax)" 2>/dev/null || true
        export DBUS_SESSION_BUS_ADDRESS
    fi

    # Unlock the keyring with an empty password (non-interactive container)
    eval "$(printf '' | gnome-keyring-daemon --unlock --components=secrets 2>/dev/null)" || true
    export GNOME_KEYRING_CONTROL
    export SSH_AUTH_SOCK

    # Persist env vars so Claude Code subprocesses inherit them
    if [ -n "$CLAUDE_ENV_FILE" ]; then
        cat >> "$CLAUDE_ENV_FILE" <<ENVEOF
export DBUS_SESSION_BUS_ADDRESS="$DBUS_SESSION_BUS_ADDRESS"
export GNOME_KEYRING_CONTROL="${GNOME_KEYRING_CONTROL:-}"
ENVEOF
    fi
fi

# --- Sync host Claude settings into writable home ---
# Host .claude is mounted read-only at /home/agent/.claude-host.
# Copy settings (not credentials) into the writable /home/agent/.claude.
CLAUDE_HOST="/home/agent/.claude-host"
CLAUDE_HOME="/home/agent/.claude"
if [ -d "$CLAUDE_HOST" ]; then
    mkdir -p "$CLAUDE_HOME"
    # Sync settings files (don't overwrite if already present)
    for f in settings.json settings.local.json; do
        if [ -f "$CLAUDE_HOST/$f" ] && [ ! -f "$CLAUDE_HOME/$f" ]; then
            cp "$CLAUDE_HOST/$f" "$CLAUDE_HOME/$f"
        fi
    done
    # Sync projects directory
    if [ -d "$CLAUDE_HOST/projects" ] && [ ! -d "$CLAUDE_HOME/projects" ]; then
        cp -r "$CLAUDE_HOST/projects" "$CLAUDE_HOME/projects"
    fi
fi

if [ ! -f /gt/mayor/town.json ]; then
    echo "Initializing Gas Town workspace at /gt..."
    /app/gastown/gt install /gt --git
else
    echo "Refreshing Gas Town workspace at /gt..."
    /app/gastown/gt install /gt --git --force
fi

exec "$@"

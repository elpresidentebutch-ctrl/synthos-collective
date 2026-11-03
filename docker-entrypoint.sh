#!/bin/sh
set -e

# Fix volume permissions for persistent disks (e.g. Render, Kubernetes, Docker mounts)
# Render mounts persistent disks at /data as root:root (0755), which blocks unprivileged users.
if [ -d /data ]; then
    chown -R synthos:synthos /data 2>/dev/null || chmod 777 /data 2>/dev/null || true
fi
if [ -d /downloads ]; then
    chown -R synthos:synthos /downloads 2>/dev/null || chmod 777 /downloads 2>/dev/null || true
fi

# If running as root, drop privileges to the unprivileged synthos user
if [ "$(id -u)" = "0" ]; then
    exec su-exec synthos "$@"
fi

exec "$@"

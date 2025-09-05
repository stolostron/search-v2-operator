#!/bin/bash

set -euo pipefail

# postgres (PostgreSQL) 16.4 -> 16.4 -> 16
INSTALL_VERSION=$(postgres --version | awk '{print $3}' | cut -d. -f1)
PGDATA="/var/lib/pgsql/data"
PGVERSION_FILE="$PGDATA/userdata/PG_VERSION"

echo "[INFO] Running before-start.sh pre-check..."

if [[ -f "$PGVERSION_FILE" ]]; then
    CURRENT_VERSION=$(cat "$PGVERSION_FILE" | tr -d '[:space:]')
    echo "[INFO] Detected existing PostgreSQL version: $CURRENT_VERSION"

    if [[ "$CURRENT_VERSION" != "$INSTALL_VERSION" ]]; then
        echo "[WARN] Found PostgreSQL $CURRENT_VERSION data directory. This is incompatible with PostgreSQL $INSTALL_VERSION."
        echo "[WARN] Clearing $PGDATA so Postgres 16 can initialize fresh."

        # Safety check: only delete if path looks correct
        if [[ "$PGDATA" == "/var/lib/pgsql/data" ]]; then
            rm -rf "${PGDATA}"/*
        else
            echo "[ERROR] PGDATA path ($PGDATA) is unexpected, refusing to delete."
            exit 1
        fi
    else
        echo "[INFO] PG_VERSION is already up to date, keeping existing data."
    fi
else
    echo "[INFO] No existing PG_VERSION file found. Assuming fresh install."
fi

echo "[INFO] Pre-check complete. Handing off to Postgres..."
exec /usr/bin/run-postgresql

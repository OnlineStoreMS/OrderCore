#!/bin/sh
set -eu

PORTAL_URL="${VITE_PORTAL_URL:-}"
if [ -z "$PORTAL_URL" ] && [ -n "${PUBLIC_HOST:-}" ]; then
  PORTAL_URL="http://${PUBLIC_HOST}:5174"
fi
PORTAL_URL="${PORTAL_URL:-http://localhost:5174}"

cat > /usr/share/nginx/html/runtime-config.js <<EOF
window.__RUNTIME_CONFIG__ = {
  portalUrl: "${PORTAL_URL}"
};
EOF

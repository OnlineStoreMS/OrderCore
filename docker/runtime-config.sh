#!/bin/sh
set -eu

BASE_DOMAIN="${OSMS_BASE_DOMAIN:-}"
HTTPS_PORT="${OSMS_CADDY_HTTPS_PORT:-443}"

https_public() {
  host="$1"
  if [ -z "$HTTPS_PORT" ] || [ "$HTTPS_PORT" = "443" ]; then
    echo "https://${host}"
  else
    echo "https://${host}:${HTTPS_PORT}"
  fi
}

if [ -n "$BASE_DOMAIN" ]; then
  ORIGIN="$(https_public "$BASE_DOMAIN")"
  PORTAL_URL="${VITE_PORTAL_URL:-${ORIGIN}}"
  ORDERCORE_URL="${VITE_ORDERCORE_URL:-${ORIGIN}/apps/order}"
  SHIPPINGCORE_URL="${VITE_SHIPPINGCORE_URL:-${ORIGIN}/apps/shipping}"
else
  PORTAL_URL="${VITE_PORTAL_URL:-}"
  if [ -z "$PORTAL_URL" ] && [ -n "${PUBLIC_HOST:-}" ]; then
    PORTAL_URL="http://${PUBLIC_HOST}:5174"
  fi
  PORTAL_URL="${PORTAL_URL:-http://localhost:5174}"

  ORDERCORE_URL="${VITE_ORDERCORE_URL:-}"
  if [ -z "$ORDERCORE_URL" ] && [ -n "${PUBLIC_HOST:-}" ]; then
    ORDERCORE_URL="http://${PUBLIC_HOST}:5182"
  fi
  ORDERCORE_URL="${ORDERCORE_URL:-http://localhost:5182}"

  SHIPPINGCORE_URL="${VITE_SHIPPINGCORE_URL:-}"
  if [ -z "$SHIPPINGCORE_URL" ] && [ -n "${PUBLIC_HOST:-}" ]; then
    SHIPPINGCORE_URL="http://${PUBLIC_HOST}:5181"
  fi
  SHIPPINGCORE_URL="${SHIPPINGCORE_URL:-http://localhost:5181}"
fi

cat > /usr/share/nginx/html/runtime-config.js <<EOF
window.__RUNTIME_CONFIG__ = {
  portalUrl: "${PORTAL_URL}",
  orderCoreUrl: "${ORDERCORE_URL}",
  shippingCoreUrl: "${SHIPPINGCORE_URL}"
};
EOF

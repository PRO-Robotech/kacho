#!/usr/bin/env bash
# gen-tls-cert.sh — генерирует self-signed TLS-сертификат для api-gateway
# и создаёт k8s-secret api-gateway-tls в namespace kacho.
#
# Cert содержит SAN: api.kacho.local, localhost, 127.0.0.1, 0.0.0.0 — подходит
# для всех вариантов доступа (через ingress / port-forward / direct).
#
# Запускать ПЕРЕД `helm upgrade` (чтобы api-gateway pod при старте смонтировал
# секрет) или после первого dev-up для последующих helm reload.

set -euo pipefail

OUT_DIR="${OUT_DIR:-/tmp/kacho-tls}"
NAMESPACE="${NAMESPACE:-kacho}"
SECRET_NAME="${SECRET_NAME:-api-gateway-tls}"

mkdir -p "$OUT_DIR"
cd "$OUT_DIR"

if [[ -f tls.crt && -f tls.key ]]; then
  echo "$OUT_DIR/tls.{crt,key} already exist, reusing"
else
  openssl req -x509 -nodes -days 3650 -newkey rsa:2048 \
    -keyout tls.key -out tls.crt \
    -subj "/CN=api.kacho.local/O=Kacho" \
    -addext "subjectAltName=DNS:api.kacho.local,DNS:localhost,IP:127.0.0.1,IP:0.0.0.0" \
    > /dev/null 2>&1
  echo "generated $OUT_DIR/tls.crt + tls.key"
fi

# Create or update the k8s secret. --dry-run pattern allows update on existing secret.
kubectl -n "$NAMESPACE" create secret tls "$SECRET_NAME" \
  --cert="$OUT_DIR/tls.crt" --key="$OUT_DIR/tls.key" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null
echo "secret $NAMESPACE/$SECRET_NAME created/updated"

echo
echo "TLS cert ready. Set SSL_CERT_FILE for clients that need to verify the cert:"
echo "  export SSL_CERT_FILE=$OUT_DIR/tls.crt"

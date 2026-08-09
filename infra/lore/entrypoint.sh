#!/bin/sh
set -eu

mkdir -p /data/certificates /data/store

if [ ! -f /data/certificates/cert.pem ] || [ ! -f /data/certificates/key.pem ]; then
  openssl req -x509 -newkey rsa:2048 -nodes \
    -keyout /data/certificates/key.pem \
    -out /data/certificates/cert.pem \
    -days 365 \
    -subj "/CN=lore" \
    -addext "subjectAltName=DNS:lore,DNS:localhost,IP:127.0.0.1"
fi

exec loreserver --config /etc/lore/config

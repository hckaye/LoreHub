#!/bin/sh
set -eu

umask 077
mkdir -p /tls

environment=${LOREHUB_ENV:-development}
root_domain=${LOREHUB_LORE_ROOT_DOMAIN:-lorehub.localhost}

if [ "$environment" = production ]; then
  for required in ca.crt server.key server.crt lore-client.key lore-client.crt; do
    test -s "/input/${required}"
    cp "/input/${required}" "/tls/${required}"
  done
  openssl verify -CAfile /tls/ca.crt /tls/server.crt /tls/lore-client.crt >/dev/null
  server_names=${LOREHUB_TLS_SERVER_NAMES:?LOREHUB_TLS_SERVER_NAMES is required in production}
  old_ifs=$IFS
  IFS=,
  for server_name in $server_names; do
    test -n "$server_name"
    openssl x509 -in /tls/server.crt -checkhost "$server_name" -noout >/dev/null
  done
  IFS=$old_ifs
  chmod 0600 /tls/*.key
  chmod 0644 /tls/*.crt
  chown 0:999 /tls/server.key /tls/lore-client.key
  chmod 0640 /tls/server.key /tls/lore-client.key
  if [ -d /export ]; then
    cp /tls/ca.crt /export/lorehub-local-ca.crt
    chmod 0644 /export/lorehub-local-ca.crt
  fi
  exit 0
fi

if [ -e /tls/ca.crt ]; then
  for required in ca.key server.key server.crt lore-client.key lore-client.crt; do
    test -s "/tls/${required}"
  done
  chown 0:999 /tls/server.key /tls/lore-client.key
  chmod 0640 /tls/server.key /tls/lore-client.key
  if [ -d /export ]; then
    cp /tls/ca.crt /export/lorehub-local-ca.crt
    chmod 0644 /export/lorehub-local-ca.crt
  fi
  exit 0
fi

openssl req -x509 -newkey rsa:3072 -nodes \
  -keyout /tls/ca.key -out /tls/ca.crt -days 3650 \
  -subj "/CN=LoreHub local CA" \
  -addext "basicConstraints=critical,CA:TRUE,pathlen:1" \
  -addext "keyUsage=critical,keyCertSign,cRLSign"

case "$root_domain" in
  *[!A-Za-z0-9.-]*) echo "LOREHUB_LORE_ROOT_DOMAIN contains invalid characters" >&2; exit 1 ;;
esac
server_san="DNS:${root_domain}"
server_san="${server_san},DNS:auth.${root_domain}"
server_san="${server_san},DNS:api.${root_domain}"
server_san="${server_san},DNS:lore.${root_domain}"
server_san="${server_san},DNS:localhost,IP:127.0.0.1"
cat >/tmp/server.ext <<EOF
basicConstraints=CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
subjectAltName=${server_san}
EOF
openssl req -newkey rsa:2048 -nodes -keyout /tls/server.key \
  -out /tmp/server.csr -subj "/CN=lorehub-api"
openssl x509 -req -in /tmp/server.csr -CA /tls/ca.crt -CAkey /tls/ca.key \
  -CAcreateserial -out /tls/server.crt -days 825 -sha256 \
  -extfile /tmp/server.ext

cat >/tmp/client.ext <<'EOF'
basicConstraints=CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=clientAuth
EOF
openssl req -newkey rsa:2048 -nodes -keyout /tls/lore-client.key \
  -out /tmp/client.csr -subj "/CN=lore-policy-hook"
openssl x509 -req -in /tmp/client.csr -CA /tls/ca.crt -CAkey /tls/ca.key \
  -CAcreateserial -out /tls/lore-client.crt -days 825 -sha256 \
  -extfile /tmp/client.ext

chmod 0600 /tls/*.key
chmod 0644 /tls/*.crt
chown 0:999 /tls/server.key /tls/lore-client.key
chmod 0640 /tls/server.key /tls/lore-client.key
rm -f /tmp/server.csr /tmp/client.csr /tmp/server.ext /tmp/client.ext /tls/ca.srl

if [ -d /export ]; then
  cp /tls/ca.crt /export/lorehub-local-ca.crt
  chmod 0644 /export/lorehub-local-ca.crt
fi

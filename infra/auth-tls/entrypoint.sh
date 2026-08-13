#!/bin/sh
set -eu

umask 077
mkdir -p /state /tls

environment=${LOREHUB_ENV:-development}
server_names=${LOREHUB_TLS_SERVER_NAMES:?LOREHUB_TLS_SERVER_NAMES is required}

validate_server_names() {
  old_ifs=$IFS
  IFS=,
  for server_name in $server_names; do
    case "$server_name" in
      ""|*[!A-Za-z0-9.-]*)
        echo "LOREHUB_TLS_SERVER_NAMES contains an invalid DNS name" >&2
        IFS=$old_ifs
        return 1
        ;;
    esac
  done
  IFS=$old_ifs
}

certificate_covers_server_names() {
  openssl verify -CAfile /tls/ca.crt /tls/server.crt >/dev/null || return 1
  old_ifs=$IFS
  IFS=,
  for server_name in $server_names; do
    if ! openssl x509 -in /tls/server.crt -checkhost "$server_name" -noout >/dev/null; then
      IFS=$old_ifs
      return 1
    fi
  done
  IFS=$old_ifs
}

validate_server_names

if [ "$environment" = production ]; then
  rm -f /tls/ca.key /tls/ca.srl
  for required in ca.crt ca.key server.key server.crt lore-client.key lore-client.crt; do
    test -s "/input/${required}"
  done
  cp /input/ca.crt /tls/ca.crt
  cp /input/ca.key /state/ca.key
  for required in server.key server.crt lore-client.key lore-client.crt; do
    cp "/input/${required}" "/tls/${required}"
  done
  openssl verify -CAfile /tls/ca.crt /tls/server.crt /tls/lore-client.crt >/dev/null
  certificate_covers_server_names
  chmod 0600 /tls/*.key
  chown 0:999 /state/ca.key
  chmod 0640 /state/ca.key
  chmod 0644 /tls/*.crt
  chown 0:999 /tls/server.key /tls/lore-client.key
  chmod 0640 /tls/server.key /tls/lore-client.key
  if [ -d /export ]; then
    cp /tls/ca.crt /export/lorehub-local-ca.crt
    chmod 0644 /export/lorehub-local-ca.crt
  fi
  exit 0
fi

if [ -e /tls/ca.crt ] && [ -e /state/ca.key ] && certificate_covers_server_names; then
  for required in server.key server.crt lore-client.key lore-client.crt; do
    test -s "/tls/${required}"
  done
  chown 0:999 /tls/server.key /tls/lore-client.key
  chmod 0640 /tls/server.key /tls/lore-client.key
  chown 0:999 /state/ca.key
  chmod 0640 /state/ca.key
  if [ -d /export ]; then
    cp /tls/ca.crt /export/lorehub-local-ca.crt
    chmod 0644 /export/lorehub-local-ca.crt
  fi
  exit 0
fi

rm -f \
  /state/ca.key \
  /state/ca.srl \
  /tls/ca.crt \
  /tls/ca.key \
  /tls/ca.srl \
  /tls/lore-client.crt \
  /tls/lore-client.key \
  /tls/server.crt \
  /tls/server.key
openssl req -x509 -newkey rsa:3072 -nodes \
  -keyout /state/ca.key -out /tls/ca.crt -days 3650 \
  -subj "/CN=LoreHub local CA" \
  -addext "basicConstraints=critical,CA:TRUE,pathlen:1" \
  -addext "keyUsage=critical,keyCertSign,cRLSign"

server_san=""
old_ifs=$IFS
IFS=,
for server_name in $server_names; do
  if [ -n "$server_san" ]; then
    server_san="${server_san},"
  fi
  server_san="${server_san}DNS:${server_name}"
done
IFS=$old_ifs
server_san="${server_san},DNS:localhost,IP:127.0.0.1"
cat >/tmp/server.ext <<EOF
basicConstraints=CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
subjectAltName=${server_san}
EOF
openssl req -newkey rsa:2048 -nodes -keyout /tls/server.key \
  -out /tmp/server.csr -subj "/CN=lorehub-api"
openssl x509 -req -in /tmp/server.csr -CA /tls/ca.crt -CAkey /state/ca.key \
  -CAserial /state/ca.srl -CAcreateserial -out /tls/server.crt -days 825 -sha256 \
  -extfile /tmp/server.ext

cat >/tmp/client.ext <<'EOF'
basicConstraints=CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=clientAuth
EOF
openssl req -newkey rsa:2048 -nodes -keyout /tls/lore-client.key \
  -out /tmp/client.csr -subj "/CN=lore-policy-hook"
openssl x509 -req -in /tmp/client.csr -CA /tls/ca.crt -CAkey /state/ca.key \
  -CAserial /state/ca.srl -out /tls/lore-client.crt -days 825 -sha256 \
  -extfile /tmp/client.ext

chmod 0600 /tls/*.key
chown 0:999 /state/ca.key
chmod 0640 /state/ca.key
chmod 0644 /tls/*.crt
chown 0:999 /tls/server.key /tls/lore-client.key
chmod 0640 /tls/server.key /tls/lore-client.key
rm -f /tmp/server.csr /tmp/client.csr /tmp/server.ext /tmp/client.ext /state/ca.srl

if [ -d /export ]; then
  cp /tls/ca.crt /export/lorehub-local-ca.crt
  chmod 0644 /export/lorehub-local-ca.crt
fi

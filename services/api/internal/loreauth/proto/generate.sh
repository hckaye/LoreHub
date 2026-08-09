#!/bin/sh
set -eu

go_bin="$(go env GOPATH)/bin"
PATH="${go_bin}:${PATH}"
export PATH

protoc \
  -I=proto \
  --go_out=epic_urc \
  --go_opt=paths=source_relative \
  --go-grpc_out=epic_urc \
  --go-grpc_opt=paths=source_relative \
  proto/auth_api.proto proto/auth_session.proto proto/auth_exchange.proto \
	proto/auth_permission.proto proto/auth_user.proto proto/auth_token.proto \
	proto/rebac_api.proto

go run proto/format_generated.go

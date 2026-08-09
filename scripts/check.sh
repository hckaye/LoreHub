#!/bin/sh
set -eu

npm run format:check
npm run limits
npm run keycloak:check
npm run keycloak:test
npm run lint
npm run typecheck
npm test
npm run build
npm run api:format:check
npm run api:lint
npm run api:test
npm run api:build
npm run security

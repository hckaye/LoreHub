#!/bin/sh
set -eu

npm run format:check
npm run limits
npm run keycloak:check
npm run secrets:test
npm run backup:test
npm run compose:test
npm run lint
npm run typecheck
npm test
npm run build
npm run api:format:check
npm run api:lint
npm run api:test
npm run api:build
npm run cli:format:check
npm run cli:lint
npm run cli:test
npm run cli:build
npm run security

# Component boundaries

[English](components.md) | [日本語](components.ja.md)

## Web application

Next.js renders the interface, selects the locale, starts OIDC sign-in or registration, and calls the Go API. It does
not connect directly to PostgreSQL or the Lore SDK. The Go API handles the authorization code exchange, separate
audience checks for ID and access tokens, PKCE, login browser binding, user association, server-side sessions, and CSRF
tokens.

## API

The Go API validates input, authenticates requests, checks permissions, applies business rules, and manages PostgreSQL
transactions. HTTP handlers call feature services instead of issuing SQL queries or calling the Lore SDK directly.

## Lore adapter

The Lore adapter is the only package that calls the official Lore Go SDK. It maps Lore data to these application
models:

- Repository: Lore Server repository ID, URL, and default branch
- Branch: branch ID, name, latest revision, protection state, and archive state
- Revision: revision ID, number, parent revision, and user-facing metadata
- Tree entry: path, type, size, and file metadata

Stored Lore identifiers are used to resolve repositories and revisions.

## PostgreSQL

PostgreSQL stores collaboration records such as OIDC identities, login transactions, server-side sessions,
organizations, permissions, repository registrations, issues, comments, labels, pull requests, reviews, branch rules,
CI runs, jobs, audit events, and outbox events. Lore file contents and revision trees stay in Lore.

## Workers

The event worker subscribes to Lore notifications and records events such as branch pushes in the PostgreSQL outbox.
The CI worker claims queued jobs with `FOR UPDATE SKIP LOCKED`, clones the requested Lore revision, and runs `act`.

## Object storage

Object storage holds CI logs, artifacts, and attachments. PostgreSQL stores their location, size, content type, and
retention deadline. Download URLs expire after a short period and are issued only after an access check.

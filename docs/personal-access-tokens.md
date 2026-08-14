# Personal access tokens

[English](personal-access-tokens.md) | [日本語](personal-access-tokens.ja.md)

Personal access tokens authenticate the Lore CLI and scripts that call the LoreHub API. Create and delete them from
**Settings**, then **Developer settings**, then **Personal access tokens**.

## Create a token

1. Enter a name that identifies the device or application.
2. Choose an expiration from 7 days to 1 year.
3. Select the permissions the application needs.
4. Select **Generate token**.
5. Copy the value beginning with `lhp_` and store it in a password manager or secret store.

The token value appears once. LoreHub shows its name, prefix, permissions, expiration, and last use after the page is
reloaded, but it cannot display the value again.

## Permissions

| Permission         | Access                                                    |
| ------------------ | --------------------------------------------------------- |
| `read_api`         | Read data from LoreHub API endpoints                      |
| `api`              | Read and change data through LoreHub API endpoints        |
| `read_repository`  | Read Lore repositories available to the account           |
| `write_repository` | Read and write Lore repositories available to the account |

Repository permissions do not grant access on their own. The account must also have access to the requested Lore
repository. `write_repository` does not grant repository administration or `obliterate` permission.

Personal access tokens cannot open the personal access token settings endpoints. Use a signed-in browser session to
create or revoke tokens.

## Sign in to the Lore CLI

Copy the Lore URL from the repository page, then run:

```bash
lore auth login --token-type api-key --token "$LOREHUB_TOKEN" "$LORE_URL"
```

`LOREHUB_TOKEN` contains the value shown after token creation. `LORE_URL` contains the repository's Lore URL. The Lore
CLI exchanges the personal access token for short-lived Lore tokens and applies the selected repository permission.

Remove the stored Lore authentication when the device no longer needs access:

```bash
lore auth logout
```

## Call the LoreHub API

Send the token as a bearer credential:

```bash
curl --fail-with-body \
  --header "Authorization: Bearer $LOREHUB_TOKEN" \
  --header "Accept: application/json" \
  "$LOREHUB_URL/api/v1/dashboard"
```

Use `read_api` for clients that only send `GET` requests. Requests that change data require `api`.

## Delete a token

Select **Delete** next to the token in **Settings**, then **Developer settings**. Deleted and expired tokens cannot
authenticate new API requests or obtain new Lore tokens. Lore tokens already issued to the CLI remain valid until their
expiration, which is no more than 10 minutes. Suspending the account also prevents new authentication.

The **Last used** value may update up to five minutes after a request. A rejected request does not update it.

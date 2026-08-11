# File locks

[English](file-locks.md) | [日本語](file-locks.ja.md)

Use file locks for assets that cannot be merged safely, such as 3D scenes, textures, audio projects, and other binary
files. A lock applies to one file on one Lore branch.

## Lock a file in LoreHub

1. Open the repository and select **Locks**.
2. Select the branch that contains the file.
3. Enter the file path relative to the repository root.
4. Select **Lock file**.

The file must exist on the selected branch. The lock list shows the owner and acquisition time. Repository readers can
view locks. Repository write access is required to create one.

## Release a lock

Open **Locks** and select **Unlock** next to the file. The user who acquired the lock can release it. Repository admins
and organization owners can release a lock left by another user.

Removing or renaming a locked file does not remove the lock. Release it from the branch where it was acquired.

## Use the Lore CLI

Run the commands from a Lore workspace after signing in:

```bash
lore lock acquire Content/Characters/Hero.uasset --branch main
lore lock query --branch main
lore lock status Content/Characters/Hero.uasset --branch main
lore lock release Content/Characters/Hero.uasset --branch main
```

The web page and CLI read the same locks from Lore Server, so changes made with either client appear in both.

## Audit records

LoreHub records successful web lock and unlock operations in the organization audit log. The record includes the
branch, file path, lock owner, and acquisition time.

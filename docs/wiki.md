# Repository wiki

[English](wiki.md) | [日本語](wiki.ja.md)

The Wiki tab contains documentation maintained with the repository. Use it for setup instructions, release procedures,
and other information that should remain available beside the project.

## Create a page

Repository members with write access can select **New page** from the Wiki tab. Enter a title and Markdown content.
The page URL is generated from the title when the URL field is empty.

Page titles are limited to 256 bytes. Page URLs are limited to 160 bytes, and page content is limited to 1 MiB.

## Edit a page

Select **Edit**, change the title, page URL, or content, and add an edit summary. Each save creates a new version.
The History list shows who saved each version and when it was saved. Select a version to read its content.

LoreHub rejects a save or deletion when someone else has updated the page since it was opened. Reload the page,
review the latest version, and apply the change again.

## Markdown

Wiki pages support headings, lists, task lists, tables, links, images, quotes, and fenced code blocks. Raw HTML is not
rendered.

## Permissions

Anyone who can read a repository can read its wiki. Repository writers, maintainers, administrators, and organization
owners can create, edit, and delete pages.

Deleting a page removes it from the Wiki tab. The deletion remains in the organization audit log.

## Webhooks

Repository administrators can select **Wiki** when configuring a webhook. LoreHub sends `wiki.created`,
`wiki.updated`, and `wiki.deleted` events for page changes.

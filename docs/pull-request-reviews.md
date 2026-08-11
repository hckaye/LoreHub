# Review pull requests

[English](pull-request-reviews.md) | [日本語](pull-request-reviews.ja.md)

Open a pull request and select **Files changed** to review its diff. Select a line number on either side of the diff,
write a comment, and start the conversation. The left side refers to the target branch before the change. The right
side refers to the proposed result.

Text files with a complete diff support line comments. Binary files and truncated diffs can still be inspected, but
they do not accept line comments.

## Continue a conversation

Anyone who can read the repository can reply to a review conversation. The person who started the conversation, the
pull request author, and users with write access can mark it as resolved or reopen it.

A comment can be edited or deleted by its author or a user with write access. Deleted comments remain in the
conversation as a placeholder so replies keep their context.

## Review a new revision

When the source or target branch changes, conversations from the earlier diff appear under **Outdated
conversations**. Replies remain available there. Start a new conversation on the current diff when the comment applies
to a changed line.

Approvals and change requests apply to the current source revision. Submit them from the **Conversation** tab.

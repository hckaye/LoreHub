# Review pull requests

[English](pull-request-reviews.md) | [日本語](pull-request-reviews.ja.md)

Open a pull request and select **Files changed** to review its diff. Select a line number on either side of the diff,
write a comment, and start the conversation. The left side refers to the target branch before the change. The right
side refers to the proposed result.

Text files with a complete diff support line comments. Binary files and truncated diffs can still be inspected, but
they do not accept line comments.

## Draft pull requests

Select **Create as draft** when the pull request is not ready for review. A draft can receive comments and reviews,
but it cannot be merged.

Open the **Conversation** tab and select **Mark ready for review** when the change is ready. The author and users with
triage access can mark it ready or convert it back to a draft. LoreHub checks the draft state again before updating
the target branch.

## Continue a conversation

Anyone who can read the repository can reply to a review conversation. The person who started the conversation, the
pull request author, and users with write access can mark it as resolved or reopen it.

A comment can be edited or deleted by its author or a user with write access. Deleted comments remain in the
conversation as a placeholder so replies keep their context.

## Request a review

The pull request author and users with triage access can request reviews from repository users or teams. Open the
**Conversation** tab, choose a user or team under **Reviewers**, and select **Request**.

Each reviewer shows the decision submitted for the current source revision. If several active team members respond,
the team shows changes requested before approved, and approved before commented. Remove a request when that review
is no longer needed.

## Review a new revision

When the source or target branch changes, conversations from the earlier diff appear under **Outdated
conversations**. Replies remain available there. Start a new conversation on the current diff when the comment applies
to a changed line.

Approvals and change requests apply to the current source revision. Submit them from the **Conversation** tab. When
the source revision changes, requested reviewers return to **Review requested** until they review the new revision.

## Organize a pull request

Users with triage access can add labels, assign up to 10 repository users, and select a milestone from the
**Conversation** tab. Removing a label, assignee, or milestone does not change the pull request state or its review
history.

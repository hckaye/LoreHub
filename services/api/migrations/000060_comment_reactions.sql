CREATE TABLE comment_reactions (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    subject_kind varchar(32) NOT NULL,
    subject_id uuid NOT NULL,
    username varchar(64) NOT NULL,
    reaction varchar(16) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT comment_reactions_subject_kind_check CHECK (
        subject_kind IN ('issue', 'merge_request', 'issue_comment', 'merge_request_comment')
    ),
    CONSTRAINT comment_reactions_reaction_check CHECK (
        reaction IN ('+1', '-1', 'laugh', 'confused', 'heart', 'hooray', 'rocket', 'eyes')
    ),
    CONSTRAINT comment_reactions_subject_user_reaction_unique
        UNIQUE (subject_kind, subject_id, username, reaction)
);

CREATE INDEX comment_reactions_subject_reaction_idx
    ON comment_reactions (repository_id, subject_kind, subject_id, reaction);

CREATE INDEX comment_reactions_subject_viewer_idx
    ON comment_reactions (repository_id, subject_kind, subject_id, username);

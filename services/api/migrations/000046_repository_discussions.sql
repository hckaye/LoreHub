ALTER TABLE repository_counters
    ADD COLUMN next_discussion_number bigint NOT NULL DEFAULT 1;

ALTER TABLE repository_webhooks
    DROP CONSTRAINT repository_webhooks_events_check;

ALTER TABLE repository_webhooks
    ADD CONSTRAINT repository_webhooks_events_check CHECK (
        cardinality(events) BETWEEN 1 AND 14
        AND array_position(events, NULL) IS NULL
        AND events <@ ARRAY[
            'actions', 'branch_rules', 'branches', 'comments', 'discussions', 'issues',
            'labels', 'milestones', 'projects', 'pull_requests', 'releases', 'repository',
            'reviews', 'wiki'
        ]::text[]
    );

CREATE TABLE discussion_categories (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    slug varchar(64) NOT NULL,
    name varchar(100) NOT NULL,
    description varchar(500) NOT NULL DEFAULT '',
    format varchar(24) NOT NULL DEFAULT 'discussion',
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT discussion_categories_id_repository_unique UNIQUE (id, repository_id),
    CONSTRAINT discussion_categories_slug_check CHECK (
        slug ~ '^[a-z0-9][a-z0-9-]{0,63}$'
    ),
    CONSTRAINT discussion_categories_name_check CHECK (
        char_length(name) BETWEEN 1 AND 100 AND name = btrim(name)
    ),
    CONSTRAINT discussion_categories_description_check CHECK (
        char_length(description) <= 500
    ),
    CONSTRAINT discussion_categories_format_check CHECK (
        format IN ('discussion', 'question', 'announcement')
    )
);

CREATE UNIQUE INDEX discussion_categories_repository_slug_idx
    ON discussion_categories (repository_id, lower(slug));

CREATE UNIQUE INDEX discussion_categories_repository_name_idx
    ON discussion_categories (repository_id, lower(name));

CREATE TABLE discussions (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    category_id uuid NOT NULL,
    number bigint NOT NULL,
    author_id uuid NOT NULL REFERENCES users (id),
    title varchar(512) NOT NULL,
    body text NOT NULL DEFAULT '',
    state varchar(16) NOT NULL DEFAULT 'open',
    locked boolean NOT NULL DEFAULT false,
    pinned boolean NOT NULL DEFAULT false,
    answered_comment_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    closed_at timestamptz,
    CONSTRAINT discussions_id_repository_unique UNIQUE (id, repository_id),
    CONSTRAINT discussions_repository_number_unique UNIQUE (repository_id, number),
    CONSTRAINT discussions_category_boundary_fk
        FOREIGN KEY (category_id, repository_id)
        REFERENCES discussion_categories (id, repository_id) ON DELETE RESTRICT,
    CONSTRAINT discussions_title_check CHECK (
        char_length(title) BETWEEN 1 AND 512 AND title = btrim(title)
    ),
    CONSTRAINT discussions_body_check CHECK (octet_length(body) <= 1048576),
    CONSTRAINT discussions_state_check CHECK (state IN ('open', 'closed')),
    CONSTRAINT discussions_closed_check CHECK (
        (state = 'closed' AND closed_at IS NOT NULL)
        OR (state = 'open' AND closed_at IS NULL)
    )
);

CREATE INDEX discussions_repository_updated_idx
    ON discussions (repository_id, pinned DESC, updated_at DESC, id DESC);

CREATE INDEX discussions_repository_category_idx
    ON discussions (repository_id, category_id, updated_at DESC);

CREATE TABLE discussion_comments (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL,
    discussion_id uuid NOT NULL,
    parent_id uuid,
    author_id uuid NOT NULL REFERENCES users (id),
    body text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    edited_at timestamptz,
    archived_at timestamptz,
    CONSTRAINT discussion_comments_id_discussion_unique UNIQUE (id, discussion_id),
    CONSTRAINT discussion_comments_id_repository_unique UNIQUE (id, repository_id),
    CONSTRAINT discussion_comments_discussion_boundary_fk
        FOREIGN KEY (discussion_id, repository_id)
        REFERENCES discussions (id, repository_id) ON DELETE CASCADE,
    CONSTRAINT discussion_comments_parent_boundary_fk
        FOREIGN KEY (parent_id, discussion_id)
        REFERENCES discussion_comments (id, discussion_id) ON DELETE CASCADE,
    CONSTRAINT discussion_comments_body_check CHECK (
        char_length(btrim(body)) > 0 AND octet_length(body) <= 262144
    )
);

CREATE INDEX discussion_comments_discussion_created_idx
    ON discussion_comments (discussion_id, created_at, id)
    WHERE archived_at IS NULL;

ALTER TABLE discussions
    ADD CONSTRAINT discussions_answer_boundary_fk
    FOREIGN KEY (answered_comment_id, id)
    REFERENCES discussion_comments (id, discussion_id) ON DELETE SET NULL (answered_comment_id);

CREATE TABLE discussion_votes (
    discussion_id uuid NOT NULL REFERENCES discussions (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (discussion_id, user_id)
);

CREATE INDEX discussion_votes_user_idx
    ON discussion_votes (user_id, discussion_id);

CREATE FUNCTION create_default_discussion_categories()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO discussion_categories (
        id, repository_id, slug, name, description, format, created_by
    ) VALUES
        (
            gen_random_uuid(), NEW.id, 'general', 'General',
            'Share updates and start conversations.', 'discussion', NEW.created_by
        ),
        (
            gen_random_uuid(), NEW.id, 'ideas', 'Ideas',
            'Suggest and discuss improvements.', 'discussion', NEW.created_by
        ),
        (
            gen_random_uuid(), NEW.id, 'questions', 'Q&A',
            'Ask questions and mark helpful replies as answers.', 'question', NEW.created_by
        );
    RETURN NEW;
END;
$$;

CREATE TRIGGER repositories_default_discussion_categories
AFTER INSERT ON repositories
FOR EACH ROW EXECUTE FUNCTION create_default_discussion_categories();

INSERT INTO discussion_categories (
    id, repository_id, slug, name, description, format
)
SELECT gen_random_uuid(), repository.id, template.slug, template.name, template.description, template.format
FROM repositories repository
CROSS JOIN (VALUES
    ('general', 'General', 'Share updates and start conversations.', 'discussion'),
    ('ideas', 'Ideas', 'Suggest and discuss improvements.', 'discussion'),
    ('questions', 'Q&A', 'Ask questions and mark helpful replies as answers.', 'question')
) AS template(slug, name, description, format);

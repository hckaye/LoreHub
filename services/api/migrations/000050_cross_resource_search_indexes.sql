CREATE INDEX IF NOT EXISTS repositories_trigram_search_idx
    ON repositories USING gin (
        lower(
            COALESCE(slug, '') || ' ' || COALESCE(display_name, '') || ' ' ||
            COALESCE(description, '')
        ) gin_trgm_ops
    );

CREATE INDEX IF NOT EXISTS organizations_trigram_search_idx
    ON organizations USING gin (
        lower(
            COALESCE(slug, '') || ' ' || COALESCE(display_name, '') || ' ' ||
            COALESCE(description, '')
        ) gin_trgm_ops
    );

CREATE INDEX IF NOT EXISTS users_trigram_search_idx
    ON users USING gin (
        lower(
            COALESCE(username, '') || ' ' || COALESCE(display_name, '') || ' ' ||
            COALESCE(bio, '') || ' ' || COALESCE(company, '') || ' ' ||
            COALESCE(location, '')
        ) gin_trgm_ops
    );

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX issues_trigram_search_idx
    ON issues USING gin ((title || ' ' || body) gin_trgm_ops);

CREATE INDEX merge_requests_trigram_search_idx
    ON merge_requests USING gin ((title || ' ' || body) gin_trgm_ops);

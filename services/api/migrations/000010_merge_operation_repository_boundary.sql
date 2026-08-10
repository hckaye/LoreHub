ALTER TABLE merge_requests
    ADD CONSTRAINT merge_requests_id_repository_unique UNIQUE (id, repository_id);

ALTER TABLE merge_operations
    ADD CONSTRAINT merge_operations_request_repository_fk
    FOREIGN KEY (merge_request_id, repository_id)
    REFERENCES merge_requests (id, repository_id)
    ON DELETE CASCADE;

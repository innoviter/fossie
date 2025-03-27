ALTER TABLE apps
    ADD COLUMN first_commit TIMESTAMP,
    ADD COLUMN last_commit TIMESTAMP;
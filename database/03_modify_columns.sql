ALTER TABLE apps
    RENAME COLUMN country TO country_code;
ALTER TABLE apps
    ALTER COLUMN country_code TYPE VARCHAR(2);
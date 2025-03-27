-- Erweiterung für UUIDs aktivieren (falls noch nicht geschehen)
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Haupttabelle: apps
CREATE TABLE apps (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    source_url VARCHAR(255) NOT NULL,
    license VARCHAR(100),
    language VARCHAR(50),
    last_activity TIMESTAMP,
    created_at TIMESTAMP DEFAULT now(),
    homepage VARCHAR(255),
    maintainer VARCHAR(255),
    country VARCHAR(100),
    status VARCHAR(50),
    stars INT,
    forks INT,
    issues_open INT,
    docker_image VARCHAR(255),
    demo_url VARCHAR(255)
);

-- Tag-Tabelle für Normalisierung
CREATE TABLE app_tags (
    app_id UUID REFERENCES apps(id) ON DELETE CASCADE,
    tag VARCHAR(50),
    PRIMARY KEY (app_id, tag)
);

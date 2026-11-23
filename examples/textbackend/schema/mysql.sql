CREATE TABLE IF NOT EXISTS text_backend_profiles (
    name VARCHAR(64) NOT NULL,
    output_prefix VARCHAR(255) NOT NULL,
    PRIMARY KEY (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- Create feeds table
CREATE TABLE IF NOT EXISTS feeds (
    id SERIAL PRIMARY KEY,
    url VARCHAR(2048) NOT NULL UNIQUE,
    title VARCHAR(500),
    last_fetched_at TIMESTAMP,
    fetch_interval_minutes INT DEFAULT 60,
    etag VARCHAR(500),
    last_modified VARCHAR(500),
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Create articles table
CREATE TABLE IF NOT EXISTS articles (
    id SERIAL PRIMARY KEY,
    feed_id INT REFERENCES feeds(id) ON DELETE CASCADE,
    title VARCHAR(500) NOT NULL,
    url VARCHAR(2048) NOT NULL,
    content TEXT,
    published_at TIMESTAMP,
    summary_brief TEXT,
    summary_detailed TEXT,
    fetched_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(feed_id, url)
);

-- Create user_preferences table
CREATE TABLE IF NOT EXISTS user_preferences (
    id SERIAL PRIMARY KEY,
    article_id INT REFERENCES articles(id) ON DELETE CASCADE,
    signal_type VARCHAR(50) NOT NULL,
    signal_value FLOAT DEFAULT 1.0,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_user_preferences_article_id ON user_preferences(article_id);
CREATE INDEX idx_user_preferences_signal_type ON user_preferences(signal_type);

-- Create interest_topics table
CREATE TABLE IF NOT EXISTS interest_topics (
    id SERIAL PRIMARY KEY,
    topic VARCHAR(200) NOT NULL UNIQUE,
    weight FLOAT DEFAULT 1.0,
    last_reinforced_at TIMESTAMP DEFAULT NOW()
);

-- Create reading_progress table
CREATE TABLE IF NOT EXISTS reading_progress (
    id SERIAL PRIMARY KEY,
    article_id INT REFERENCES articles(id) ON DELETE CASCADE UNIQUE,
    scroll_position FLOAT DEFAULT 0.0,
    last_read_at TIMESTAMP DEFAULT NOW(),
    is_completed BOOLEAN DEFAULT false
);

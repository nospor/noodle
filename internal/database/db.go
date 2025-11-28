package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Feed struct {
	ID              int64
	URL             string
	Title           string
	LastFetched     *time.Time
	LastArticleAdded *time.Time
}

type Article struct {
	ID         int64
	FeedID     int64
	Title      string
	Link       string
	Content    string
	PublishedAt *time.Time
	IsRead     bool
	IsFavorite bool
	CreatedAt  time.Time
}

var db *sql.DB

func GetDBPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".config", "noodle", "noodle.db"), nil
}

func InitDB() error {
	dbPath, err := GetDBPath()
	if err != nil {
		return err
	}

	// Create directory if it doesn't exist
	dbDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return fmt.Errorf("failed to create database directory: %w", err)
	}

	database, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	db = database

	// Create tables
	schema := `
	CREATE TABLE IF NOT EXISTS feeds (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		url TEXT UNIQUE NOT NULL,
		title TEXT NOT NULL,
		last_fetched DATETIME,
		last_article_added DATETIME
	);

	CREATE TABLE IF NOT EXISTS articles (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		feed_id INTEGER NOT NULL,
		title TEXT NOT NULL,
		link TEXT NOT NULL,
		content TEXT,
		published_at DATETIME,
		is_read INTEGER DEFAULT 0,
		is_favorite INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(feed_id, link),
		FOREIGN KEY (feed_id) REFERENCES feeds(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_articles_feed_id ON articles(feed_id);
	CREATE INDEX IF NOT EXISTS idx_articles_is_read ON articles(is_read);
	CREATE INDEX IF NOT EXISTS idx_articles_is_favorite ON articles(is_favorite);
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Migration: Add last_article_added column to feeds if it doesn't exist
	var columnExists int
	checkColumnQuery := `
		SELECT COUNT(*) FROM pragma_table_info('feeds') 
		WHERE name = 'last_article_added'
	`
	err = db.QueryRow(checkColumnQuery).Scan(&columnExists)
	if err == nil && columnExists == 0 {
		migrationQuery := `ALTER TABLE feeds ADD COLUMN last_article_added DATETIME`
		if _, err := db.Exec(migrationQuery); err != nil {
			// Ignore error if column already exists
		}
	}

	// Migration: Remove is_deleted column from articles if it exists
	checkDeletedColumnQuery := `
		SELECT COUNT(*) FROM pragma_table_info('articles') 
		WHERE name = 'is_deleted'
	`
	err = db.QueryRow(checkDeletedColumnQuery).Scan(&columnExists)
	if err == nil && columnExists > 0 {
		// SQLite doesn't support DROP COLUMN directly, so we'll need to recreate the table
		// For now, we'll just ignore it and filter in queries
		// In production, you might want to do a proper migration
	}

	return nil
}

func GetDB() *sql.DB {
	return db
}

func SaveFeed(feed *Feed) error {
	query := `
		INSERT INTO feeds (url, title, last_fetched, last_article_added)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(url) DO UPDATE SET
			title = excluded.title,
			last_fetched = excluded.last_fetched,
			last_article_added = COALESCE(excluded.last_article_added, feeds.last_article_added)
	`
	result, err := db.Exec(query, feed.URL, feed.Title, feed.LastFetched, feed.LastArticleAdded)
	if err != nil {
		return fmt.Errorf("failed to save feed: %w", err)
	}

	if feed.ID == 0 {
		id, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("failed to get feed ID: %w", err)
		}
		feed.ID = id
	}

	return nil
}

func GetFeedByURL(url string) (*Feed, error) {
	query := `SELECT id, url, title, last_fetched, last_article_added FROM feeds WHERE url = ?`
	row := db.QueryRow(query, url)

	var feed Feed
	var lastFetched, lastArticleAdded sql.NullTime
	err := row.Scan(&feed.ID, &feed.URL, &feed.Title, &lastFetched, &lastArticleAdded)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get feed: %w", err)
	}

	if lastFetched.Valid {
		feed.LastFetched = &lastFetched.Time
	}
	if lastArticleAdded.Valid {
		feed.LastArticleAdded = &lastArticleAdded.Time
	}

	return &feed, nil
}

func GetAllFeeds() ([]Feed, error) {
	query := `SELECT id, url, title, last_fetched, last_article_added FROM feeds ORDER BY title`
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query feeds: %w", err)
	}
	defer rows.Close()

	var feeds []Feed
	for rows.Next() {
		var feed Feed
		var lastFetched, lastArticleAdded sql.NullTime
		if err := rows.Scan(&feed.ID, &feed.URL, &feed.Title, &lastFetched, &lastArticleAdded); err != nil {
			return nil, fmt.Errorf("failed to scan feed: %w", err)
		}
		if lastFetched.Valid {
			feed.LastFetched = &lastFetched.Time
		}
		if lastArticleAdded.Valid {
			feed.LastArticleAdded = &lastArticleAdded.Time
		}
		feeds = append(feeds, feed)
	}

	return feeds, nil
}

func DeleteFeedByURL(url string) error {
	query := `DELETE FROM feeds WHERE url = ?`
	_, err := db.Exec(query, url)
	if err != nil {
		return fmt.Errorf("failed to delete feed: %w", err)
	}
	return nil
}

func SaveArticles(feedID int64, articles []Article) error {
	// Get the feed to check last_article_added timestamp
	feedQuery := `SELECT last_article_added FROM feeds WHERE id = ?`
	var lastArticleAdded sql.NullTime
	err := db.QueryRow(feedQuery, feedID).Scan(&lastArticleAdded)
	if err != nil {
		return fmt.Errorf("failed to get feed: %w", err)
	}

	var cutoffTime *time.Time
	if lastArticleAdded.Valid {
		cutoffTime = &lastArticleAdded.Time
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	query := `
		INSERT INTO articles (feed_id, title, link, content, published_at, is_read, is_favorite)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(feed_id, link) DO UPDATE SET
			title = excluded.title,
			content = excluded.content,
			published_at = excluded.published_at
	`

	stmt, err := tx.Prepare(query)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	var newestArticleTime *time.Time
	for _, article := range articles {
		article.FeedID = feedID
		
		// Determine article time (use published_at if available, otherwise current time)
		var articleTime time.Time
		if article.PublishedAt != nil {
			articleTime = *article.PublishedAt
		} else {
			articleTime = time.Now()
		}
		
		// Skip articles older than or equal to last_article_added
		if cutoffTime != nil && !articleTime.After(*cutoffTime) {
			continue
		}
		
		// Track the newest article time
		if newestArticleTime == nil || articleTime.After(*newestArticleTime) {
			newestArticleTime = &articleTime
		}
		
		_, err := stmt.Exec(
			article.FeedID,
			article.Title,
			article.Link,
			article.Content,
			article.PublishedAt,
			article.IsRead,
			article.IsFavorite,
		)
		if err != nil {
			return fmt.Errorf("failed to insert article: %w", err)
		}
	}

	// Update last_article_added timestamp if we added any new articles
	if newestArticleTime != nil {
		updateQuery := `UPDATE feeds SET last_article_added = ? WHERE id = ?`
		_, err = tx.Exec(updateQuery, newestArticleTime, feedID)
		if err != nil {
			return fmt.Errorf("failed to update last_article_added: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func GetArticles(feedID int64) ([]Article, error) {
	query := `
		SELECT id, feed_id, title, link, content, published_at, is_read, is_favorite, created_at
		FROM articles
		WHERE feed_id = ?
		ORDER BY published_at DESC, created_at DESC
	`
	rows, err := db.Query(query, feedID)
	if err != nil {
		return nil, fmt.Errorf("failed to query articles: %w", err)
	}
	defer rows.Close()

	var articles []Article
	for rows.Next() {
		var article Article
		var publishedAt sql.NullTime
		if err := rows.Scan(
			&article.ID,
			&article.FeedID,
			&article.Title,
			&article.Link,
			&article.Content,
			&publishedAt,
			&article.IsRead,
			&article.IsFavorite,
			&article.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan article: %w", err)
		}
		if publishedAt.Valid {
			article.PublishedAt = &publishedAt.Time
		}
		articles = append(articles, article)
	}

	return articles, nil
}

func GetUnreadCount(feedID int64) (int, error) {
	query := `SELECT COUNT(*) FROM articles WHERE feed_id = ? AND is_read = 0`
	var count int
	err := db.QueryRow(query, feedID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get unread count: %w", err)
	}
	return count, nil
}

func GetTotalCount(feedID int64) (int, error) {
	query := `SELECT COUNT(*) FROM articles WHERE feed_id = ?`
	var count int
	err := db.QueryRow(query, feedID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get total count: %w", err)
	}
	return count, nil
}

func MarkRead(articleID int64, isRead bool) error {
	query := `UPDATE articles SET is_read = ? WHERE id = ?`
	_, err := db.Exec(query, isRead, articleID)
	if err != nil {
		return fmt.Errorf("failed to mark read: %w", err)
	}
	return nil
}

func MarkFavorite(articleID int64, isFavorite bool) error {
	query := `UPDATE articles SET is_favorite = ? WHERE id = ?`
	_, err := db.Exec(query, isFavorite, articleID)
	if err != nil {
		return fmt.Errorf("failed to mark favorite: %w", err)
	}
	return nil
}

func DeleteArticle(articleID int64) error {
	query := `DELETE FROM articles WHERE id = ?`
	_, err := db.Exec(query, articleID)
	if err != nil {
		return fmt.Errorf("failed to delete article: %w", err)
	}
	return nil
}

func DeleteNonFavoriteArticles(feedID int64) (int, error) {
	query := `DELETE FROM articles WHERE feed_id = ? AND is_favorite = 0`
	result, err := db.Exec(query, feedID)
	if err != nil {
		return 0, fmt.Errorf("failed to delete non-favorite articles: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}
	return int(rowsAffected), nil
}

func CloseDB() error {
	if db != nil {
		return db.Close()
	}
	return nil
}


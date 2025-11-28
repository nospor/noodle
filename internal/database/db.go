package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Feed struct {
	ID          int64
	URL         string
	Title       string
	LastFetched *time.Time
}

type Article struct {
	ID          int64
	FeedID      int64
	Title       string
	Link        string
	Content     string
	PublishedAt *time.Time
	IsRead      bool
	IsFavorite  bool
	IsDeleted   bool
	CreatedAt   time.Time
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
		last_fetched DATETIME
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
		is_deleted INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(feed_id, link),
		FOREIGN KEY (feed_id) REFERENCES feeds(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_articles_feed_id ON articles(feed_id);
	CREATE INDEX IF NOT EXISTS idx_articles_is_read ON articles(is_read);
	CREATE INDEX IF NOT EXISTS idx_articles_is_favorite ON articles(is_favorite);
	CREATE INDEX IF NOT EXISTS idx_articles_is_deleted ON articles(is_deleted);
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Migration: Add is_deleted column to articles if it doesn't exist
	var columnExists int
	checkDeletedColumnQuery := `
		SELECT COUNT(*) FROM pragma_table_info('articles') 
		WHERE name = 'is_deleted'
	`
	err = db.QueryRow(checkDeletedColumnQuery).Scan(&columnExists)
	if err == nil && columnExists == 0 {
		migrationQuery := `ALTER TABLE articles ADD COLUMN is_deleted INTEGER DEFAULT 0`
		if _, err := db.Exec(migrationQuery); err != nil {
			// Ignore error if column already exists
		}
	}

	return nil
}

func GetDB() *sql.DB {
	return db
}

func SaveFeed(feed *Feed) error {
	query := `
		INSERT INTO feeds (url, title, last_fetched)
		VALUES (?, ?, ?)
		ON CONFLICT(url) DO UPDATE SET
			title = excluded.title,
			last_fetched = excluded.last_fetched
	`
	result, err := db.Exec(query, feed.URL, feed.Title, feed.LastFetched)
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
	query := `SELECT id, url, title, last_fetched FROM feeds WHERE url = ?`
	row := db.QueryRow(query, url)

	var feed Feed
	var lastFetched sql.NullTime
	err := row.Scan(&feed.ID, &feed.URL, &feed.Title, &lastFetched)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get feed: %w", err)
	}

	if lastFetched.Valid {
		feed.LastFetched = &lastFetched.Time
	}

	return &feed, nil
}

func GetAllFeeds() ([]Feed, error) {
	query := `SELECT id, url, title, last_fetched FROM feeds ORDER BY title`
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query feeds: %w", err)
	}
	defer rows.Close()

	var feeds []Feed
	for rows.Next() {
		var feed Feed
		var lastFetched sql.NullTime
		if err := rows.Scan(&feed.ID, &feed.URL, &feed.Title, &lastFetched); err != nil {
			return nil, fmt.Errorf("failed to scan feed: %w", err)
		}
		if lastFetched.Valid {
			feed.LastFetched = &lastFetched.Time
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
	if len(articles) == 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Build a map of existing articles by link for efficient lookup
	existingMap := make(map[string]struct {
		id         int64
		isDeleted  int
	})
	
	// Deduplicate articles by link (keep first occurrence)
	uniqueArticles := make([]Article, 0, len(articles))
	seenLinks := make(map[string]bool)
	for _, article := range articles {
		article.FeedID = feedID
		if !seenLinks[article.Link] {
			uniqueArticles = append(uniqueArticles, article)
			seenLinks[article.Link] = true
		}
	}
	
	// Collect all unique links to check
	links := make([]string, 0, len(uniqueArticles))
	for _, article := range uniqueArticles {
		links = append(links, article.Link)
	}

	// Batch check for existing articles
	placeholders := make([]string, len(links))
	args := make([]interface{}, 0, len(links)+1)
	args = append(args, feedID)
	for i, link := range links {
		placeholders[i] = "?"
		args = append(args, link)
	}

	query := fmt.Sprintf(`
		SELECT link, id, is_deleted 
		FROM articles 
		WHERE feed_id = ? AND link IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := tx.Query(query, args...)
	if err != nil {
		return fmt.Errorf("failed to query existing articles: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var link string
		var id int64
		var isDeleted int
		if err := rows.Scan(&link, &id, &isDeleted); err != nil {
			return fmt.Errorf("failed to scan existing article: %w", err)
		}
		existingMap[link] = struct {
			id        int64
			isDeleted int
		}{id: id, isDeleted: isDeleted}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating existing articles: %w", err)
	}

	// Prepare statements
	insertStmt, err := tx.Prepare(`
		INSERT INTO articles (feed_id, title, link, content, published_at, is_read, is_favorite, is_deleted)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer insertStmt.Close()

	updateStmt, err := tx.Prepare(`
		UPDATE articles SET
			title = ?,
			content = ?,
			published_at = ?,
			is_deleted = CASE 
				WHEN is_deleted = 1 THEN 1 
				ELSE ? 
			END
		WHERE feed_id = ? AND link = ?
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare update statement: %w", err)
	}
	defer updateStmt.Close()

	// Process articles: insert new ones, update existing ones
	for _, article := range uniqueArticles {
		if existing, exists := existingMap[article.Link]; exists {
			// Article exists, update it (preserve is_deleted if it's 1)
			var newIsDeleted int
			if existing.isDeleted == 1 {
				newIsDeleted = 1
			} else {
				newIsDeleted = 0
			}
			
			_, err := updateStmt.Exec(
				article.Title,
				article.Content,
				article.PublishedAt,
				newIsDeleted,
				feedID,
				article.Link,
			)
			if err != nil {
				return fmt.Errorf("failed to update article: %w", err)
			}
		} else {
			// Article doesn't exist, insert it
			_, err := insertStmt.Exec(
				article.FeedID,
				article.Title,
				article.Link,
				article.Content,
				article.PublishedAt,
				article.IsRead,
				article.IsFavorite,
				article.IsDeleted,
			)
			if err != nil {
				// If UNIQUE constraint error, article was inserted between check and insert
				// Fall back to update instead
				if strings.Contains(err.Error(), "UNIQUE constraint") {
					// Re-check if article exists now and update it
					var existingIsDeleted int
					checkErr := tx.QueryRow(
						`SELECT is_deleted FROM articles WHERE feed_id = ? AND link = ?`,
						feedID, article.Link,
					).Scan(&existingIsDeleted)
					
					if checkErr == nil {
						// Article exists now, update it
						var newIsDeleted int
						if existingIsDeleted == 1 {
							newIsDeleted = 1
						} else {
							newIsDeleted = 0
						}
						
						_, updateErr := updateStmt.Exec(
							article.Title,
							article.Content,
							article.PublishedAt,
							newIsDeleted,
							feedID,
							article.Link,
						)
						if updateErr != nil {
							return fmt.Errorf("failed to update article after constraint error: %w", updateErr)
						}
						// Successfully handled the race condition
						continue
					}
				}
				return fmt.Errorf("failed to insert article: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func GetArticles(feedID int64) ([]Article, error) {
	query := `
		SELECT id, feed_id, title, link, content, published_at, is_read, is_favorite, is_deleted, created_at
		FROM articles
		WHERE feed_id = ? AND is_deleted = 0
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
		var isDeleted int
		if err := rows.Scan(
			&article.ID,
			&article.FeedID,
			&article.Title,
			&article.Link,
			&article.Content,
			&publishedAt,
			&article.IsRead,
			&article.IsFavorite,
			&isDeleted,
			&article.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan article: %w", err)
		}
		if publishedAt.Valid {
			article.PublishedAt = &publishedAt.Time
		}
		article.IsDeleted = isDeleted != 0
		articles = append(articles, article)
	}

	return articles, nil
}

func GetUnreadCount(feedID int64) (int, error) {
	query := `SELECT COUNT(*) FROM articles WHERE feed_id = ? AND is_read = 0 AND is_deleted = 0`
	var count int
	err := db.QueryRow(query, feedID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get unread count: %w", err)
	}
	return count, nil
}

func GetTotalCount(feedID int64) (int, error) {
	query := `SELECT COUNT(*) FROM articles WHERE feed_id = ? AND is_deleted = 0`
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
	query := `UPDATE articles SET is_deleted = 1 WHERE id = ?`
	_, err := db.Exec(query, articleID)
	if err != nil {
		return fmt.Errorf("failed to delete article: %w", err)
	}
	return nil
}

func DeleteNonFavoriteArticles(feedID int64) (int, error) {
	query := `UPDATE articles SET is_deleted = 1 WHERE feed_id = ? AND is_favorite = 0 AND is_deleted = 0`
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

func CleanupDeletedArticles(feedID int64, days int) (int, error) {
	cutoffTime := time.Now().AddDate(0, 0, -days)
	query := `DELETE FROM articles WHERE feed_id = ? AND is_deleted = 1 AND created_at < ?`
	result, err := db.Exec(query, feedID, cutoffTime)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup deleted articles: %w", err)
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


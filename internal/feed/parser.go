package feed

import (
	"fmt"
	"time"

	"github.com/mmcdole/gofeed"
	"noodle/internal/database"
)

func FetchAndParseFeed(url string) (*gofeed.Feed, error) {
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("failed to parse feed: %w", err)
	}
	return feed, nil
}

func ConvertFeedToDBFeed(feed *gofeed.Feed, url string, customTitle string) *database.Feed {
	title := customTitle
	if title == "" {
		title = feed.Title
	}
	if title == "" {
		title = url
	}

	now := time.Now()
	return &database.Feed{
		URL:        url,
		Title:      title,
		LastFetched: &now,
	}
}

func ConvertItemsToArticles(feedItems []*gofeed.Item) []database.Article {
	articles := make([]database.Article, 0, len(feedItems))
	for _, item := range feedItems {
		article := database.Article{
			Title:   item.Title,
			Link:    item.Link,
			Content: getContent(item),
			IsRead:  false,
			IsFavorite: false,
		}

		if item.PublishedParsed != nil {
			article.PublishedAt = item.PublishedParsed
		} else if item.UpdatedParsed != nil {
			article.PublishedAt = item.UpdatedParsed
		}

		articles = append(articles, article)
	}
	return articles
}

func getContent(item *gofeed.Item) string {
	if item.Content != "" {
		return item.Content
	}
	if item.Description != "" {
		return item.Description
	}
	return ""
}


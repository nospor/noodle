package ui

import (
	"noodle/internal/config"
	"noodle/internal/database"
)

type View int

const (
	MainView View = iota
	ArticleView
	AddFeedView
	EditFeedView
)

type AppState struct {
	View                 View
	Config               *config.Config
	Feeds                []database.Feed
	Articles             []database.Article
	SelectedFeedIndex    int
	SelectedArticleIndex int
	CurrentFeedID        int64
	CurrentArticle       *database.Article
	Error                string
	Message              string
	Width                int
	Height               int
}

package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"noodle/internal/config"
	"noodle/internal/database"
	"noodle/internal/feed"
)

var (
	titleStyle        = lipgloss.NewStyle().MarginLeft(2)
	itemStyle         = lipgloss.NewStyle().PaddingLeft(4)
	selectedItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))
	paneStyle         = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("62")).Padding(1, 2)
	errorStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Margin(1)
	messageStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Margin(1)
)

type feedItem struct {
	feed database.Feed
	unreadCount int
}

func (i feedItem) FilterValue() string { return i.feed.Title }
func (i feedItem) Title() string {
	if i.unreadCount > 0 {
		return fmt.Sprintf("%s (%d)", i.feed.Title, i.unreadCount)
	}
	return i.feed.Title
}
func (i feedItem) Description() string { return i.feed.URL }

type articleItem struct {
	article database.Article
}

func (i articleItem) FilterValue() string { return i.article.Title }
func (i articleItem) Title() string {
	title := i.article.Title
	if i.article.IsFavorite {
		title = "★ " + title
	}
	if !i.article.IsRead {
		title = "● " + title
	}
	return title
}
func (i articleItem) Description() string {
	if i.article.PublishedAt != nil {
		return i.article.PublishedAt.Format("2006-01-02 15:04")
	}
	return ""
}

type MainModel struct {
	feedsList    list.Model
	articlesList list.Model
	state        *AppState
	width        int
	height       int
	activePane   string // "feeds" or "articles"
}

type refreshFeedMsg struct {
	feedURL string
	err     error
}

type loadArticlesMsg struct {
	articles []database.Article
	err      error
}

type loadFeedsMsg struct {
	feeds []database.Feed
	err   error
}

func NewMainModel(state *AppState) *MainModel {
	m := &MainModel{
		state:      state,
		activePane: "feeds",
		width:      state.Width,
		height:     state.Height,
	}

	// Initialize feeds list
	feedsDelegate := list.NewDefaultDelegate()
	feedsDelegate.Styles.SelectedTitle = selectedItemStyle
	feedsDelegate.Styles.SelectedDesc = selectedItemStyle

	m.feedsList = list.New([]list.Item{}, feedsDelegate, 0, 0)
	m.feedsList.Title = "Feeds"
	m.feedsList.SetShowStatusBar(false)
	m.feedsList.SetFilteringEnabled(false)

	// Initialize articles list
	articlesDelegate := list.NewDefaultDelegate()
	articlesDelegate.Styles.SelectedTitle = selectedItemStyle
	articlesDelegate.Styles.SelectedDesc = selectedItemStyle

	m.articlesList = list.New([]list.Item{}, articlesDelegate, 0, 0)
	m.articlesList.Title = "Articles"
	m.articlesList.SetShowStatusBar(false)
	m.articlesList.SetFilteringEnabled(false)

	// Set width/height if available
	if state.Width > 0 && state.Height > 0 {
		leftWidth := state.Width / 2
		rightWidth := state.Width - leftWidth - 4
		availableHeight := state.Height - 6
		m.feedsList.SetWidth(leftWidth - 6)
		m.feedsList.SetHeight(availableHeight)
		m.articlesList.SetWidth(rightWidth - 6)
		m.articlesList.SetHeight(availableHeight)
	}

	return m
}

func (m *MainModel) Init() tea.Cmd {
	return tea.Batch(m.loadFeeds(), m.loadArticles())
}

func (m *MainModel) loadFeeds() tea.Cmd {
	return func() tea.Msg {
		feeds, err := database.GetAllFeeds()
		if err != nil {
			return loadFeedsMsg{err: err}
		}
		return loadFeedsMsg{feeds: feeds}
	}
}

func (m *MainModel) loadArticles() tea.Cmd {
	return func() tea.Msg {
		if len(m.state.Feeds) == 0 || m.state.SelectedFeedIndex >= len(m.state.Feeds) {
			return loadArticlesMsg{articles: []database.Article{}}
		}
		feed := m.state.Feeds[m.state.SelectedFeedIndex]
		articles, err := database.GetArticles(feed.ID)
		if err != nil {
			return loadArticlesMsg{err: err}
		}
		return loadArticlesMsg{articles: articles}
	}
}

func (m *MainModel) refreshFeed(feedURL string) tea.Cmd {
	return func() tea.Msg {
		parsedFeed, err := feed.FetchAndParseFeed(feedURL)
		if err != nil {
			return refreshFeedMsg{feedURL: feedURL, err: err}
		}

		// Find feed in config
		var configFeed *config.Feed
		for i := range m.state.Config.Feeds {
			if m.state.Config.Feeds[i].URL == feedURL {
				configFeed = &m.state.Config.Feeds[i]
				break
			}
		}

		if configFeed == nil {
			return refreshFeedMsg{feedURL: feedURL, err: fmt.Errorf("feed not found in config")}
		}

		// Convert and save feed
		dbFeed := feed.ConvertFeedToDBFeed(parsedFeed, feedURL, configFeed.Title)
		if err := database.SaveFeed(dbFeed); err != nil {
			return refreshFeedMsg{feedURL: feedURL, err: err}
		}

		// Convert and save articles
		articles := feed.ConvertItemsToArticles(parsedFeed.Items)
		if err := database.SaveArticles(dbFeed.ID, articles); err != nil {
			return refreshFeedMsg{feedURL: feedURL, err: err}
		}

		return refreshFeedMsg{feedURL: feedURL}
	}
}

func (m *MainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.state.Width = msg.Width
		m.state.Height = msg.Height
		leftWidth := msg.Width / 2
		rightWidth := msg.Width - leftWidth - 4
		// Account for borders, padding, title, and help text
		availableHeight := msg.Height - 6
		m.feedsList.SetWidth(leftWidth - 6)
		m.feedsList.SetHeight(availableHeight)
		m.articlesList.SetWidth(rightWidth - 6)
		m.articlesList.SetHeight(availableHeight)
		return m, nil

	case loadFeedsMsg:
		if msg.err != nil {
			m.state.Error = fmt.Sprintf("Failed to load feeds: %v", msg.err)
			return m, nil
		}
		m.state.Feeds = msg.feeds
		items := make([]list.Item, len(msg.feeds))
		for i, f := range msg.feeds {
			unread, _ := database.GetUnreadCount(f.ID)
			items[i] = feedItem{feed: f, unreadCount: unread}
		}
		m.feedsList.SetItems(items)
		if len(msg.feeds) > 0 && m.state.SelectedFeedIndex < len(msg.feeds) {
			m.feedsList.Select(m.state.SelectedFeedIndex)
		}
		return m, m.loadArticles()

	case loadArticlesMsg:
		if msg.err != nil {
			m.state.Error = fmt.Sprintf("Failed to load articles: %v", msg.err)
			return m, nil
		}
		m.state.Articles = msg.articles
		items := make([]list.Item, len(msg.articles))
		for i, a := range msg.articles {
			items[i] = articleItem{article: a}
		}
		m.articlesList.SetItems(items)
		// Don't select any article in main view - selection only happens in article view
		m.articlesList.Select(-1)
		return m, nil

	case refreshFeedMsg:
		if msg.err != nil {
			m.state.Error = fmt.Sprintf("Failed to refresh feed: %v", msg.err)
		} else {
			m.state.Message = "Feed refreshed successfully"
			go func() {
				time.Sleep(2 * time.Second)
			}()
		}
		return m, tea.Batch(m.loadFeeds(), m.loadArticles())

	case FeedAddedMsg, FeedUpdatedMsg:
		return m, tea.Batch(m.loadFeeds(), m.loadArticles())

	case feedDeletedMsg:
		return m, tea.Batch(m.loadFeeds(), m.loadArticles())

	case tea.KeyMsg:
		if m.state.Error != "" {
			m.state.Error = ""
			return m, nil
		}
		if m.state.Message != "" {
			m.state.Message = ""
			return m, nil
		}

		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, keys.Up), msg.String() == "k":
			if m.activePane == "feeds" {
				m.feedsList, _ = m.feedsList.Update(msg)
				if m.feedsList.Index() < len(m.state.Feeds) {
					m.state.SelectedFeedIndex = m.feedsList.Index()
					return m, m.loadArticles()
				}
			}
			// Articles list navigation disabled in main view
			return m, nil

		case key.Matches(msg, keys.Down), msg.String() == "j":
			if m.activePane == "feeds" {
				m.feedsList, _ = m.feedsList.Update(msg)
				if m.feedsList.Index() < len(m.state.Feeds) {
					m.state.SelectedFeedIndex = m.feedsList.Index()
					return m, m.loadArticles()
				}
			}
			// Articles list navigation disabled in main view
			return m, nil

		case key.Matches(msg, keys.Right), msg.String() == "l":
			// Switch to article view (articles on left, content on right)
			if len(m.state.Feeds) > 0 && m.state.SelectedFeedIndex < len(m.state.Feeds) && len(m.state.Articles) > 0 {
				m.state.CurrentFeedID = m.state.Feeds[m.state.SelectedFeedIndex].ID
				// Reset article selection - will be set to first article in article view
				m.state.SelectedArticleIndex = 0
				m.state.View = ArticleView
				return NewArticleModel(m.state), nil
			}

		case key.Matches(msg, keys.Left), msg.String() == "h":
			// In main view, h doesn't do anything special (already on feeds pane)
			return m, nil

		case msg.String() == "enter":
			if m.activePane == "articles" && len(m.state.Articles) > 0 {
				// Switch to article view
				m.state.CurrentFeedID = m.state.Feeds[m.state.SelectedFeedIndex].ID
				// Reset article selection - will be set to first article in article view
				m.state.SelectedArticleIndex = 0
				m.state.View = ArticleView
				return NewArticleModel(m.state), nil
			}
			// Enter does nothing in feeds pane
			return m, nil

		case msg.String() == "r":
			if m.activePane == "feeds" && len(m.state.Feeds) > 0 {
				feed := m.state.Feeds[m.state.SelectedFeedIndex]
				return m, m.refreshFeed(feed.URL)
			} else if m.activePane == "articles" && len(m.state.Articles) > 0 {
				article := m.state.Articles[m.state.SelectedArticleIndex]
				return m, m.markRead(article.ID, true)
			}

		case msg.String() == "u":
			if m.activePane == "articles" && len(m.state.Articles) > 0 {
				article := m.state.Articles[m.state.SelectedArticleIndex]
				return m, m.markRead(article.ID, false)
			}

		case msg.String() == "f":
			if m.activePane == "articles" && len(m.state.Articles) > 0 {
				article := m.state.Articles[m.state.SelectedArticleIndex]
				return m, m.toggleFavorite(article.ID, !article.IsFavorite)
			}

		case msg.String() == "x":
			if m.activePane == "articles" && len(m.state.Articles) > 0 {
				article := m.state.Articles[m.state.SelectedArticleIndex]
				return m, m.deleteArticle(article.ID)
			}

		case msg.String() == "a":
			// Add feed - will be handled by parent
			return m, nil

		case msg.String() == "e":
			if m.activePane == "feeds" && len(m.state.Feeds) > 0 {
				// Edit feed - will be handled by parent
				return m, nil
			}

		case msg.String() == "d":
			if m.activePane == "feeds" && len(m.state.Feeds) > 0 {
				feed := m.state.Feeds[m.state.SelectedFeedIndex]
				return m, m.deleteFeed(feed.URL)
			}
		}

		// Update lists - only update feeds list in main view
		if m.activePane == "feeds" {
			m.feedsList, _ = m.feedsList.Update(msg)
		}
		// Articles list is read-only in main view
		return m, nil
	}

	return m, nil
}

func (m *MainModel) markRead(articleID int64, isRead bool) tea.Cmd {
	return func() tea.Msg {
		if err := database.MarkRead(articleID, isRead); err != nil {
			return errorMsg{err: err}
		}
		return nil
	}
}

func (m *MainModel) toggleFavorite(articleID int64, isFavorite bool) tea.Cmd {
	return func() tea.Msg {
		if err := database.MarkFavorite(articleID, isFavorite); err != nil {
			return errorMsg{err: err}
		}
		return nil
	}
}

func (m *MainModel) deleteArticle(articleID int64) tea.Cmd {
	return func() tea.Msg {
		if err := database.DeleteArticle(articleID); err != nil {
			return errorMsg{err: err}
		}
		return nil
	}
}

func (m *MainModel) deleteFeed(feedURL string) tea.Cmd {
	return func() tea.Msg {
		// Delete from database
		if err := database.DeleteFeedByURL(feedURL); err != nil {
			return errorMsg{err: err}
		}

		// Delete from config
		for i, f := range m.state.Config.Feeds {
			if f.URL == feedURL {
				if err := config.DeleteFeed(m.state.Config, i); err != nil {
					return errorMsg{err: err}
				}
				break
			}
		}

		return feedDeletedMsg{}
	}
}

func (m *MainModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var s strings.Builder

	// Feeds pane
	feedsView := m.feedsList.View()
	feedsTitle := fmt.Sprintf("Feeds (%d)", len(m.state.Feeds))
	if m.activePane == "feeds" {
		feedsTitle += " [ACTIVE]"
	}
	feedsPane := paneStyle.Width(m.width/2 - 2).Render(feedsTitle + "\n" + feedsView)

	// Articles pane
	articlesView := m.articlesList.View()
	articlesTitle := "Articles"
	if m.activePane == "articles" {
		articlesTitle += " [ACTIVE]"
	}
	if len(m.state.Feeds) > 0 && m.state.SelectedFeedIndex < len(m.state.Feeds) {
		feed := m.state.Feeds[m.state.SelectedFeedIndex]
		unread, _ := database.GetUnreadCount(feed.ID)
		articlesTitle += fmt.Sprintf(" (%d unread)", unread)
	}
	articlesPane := paneStyle.Width(m.width/2 - 2).Render(articlesTitle + "\n" + articlesView)

	// Combine panes
	s.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, feedsPane, articlesPane))

	// Error and message
	if m.state.Error != "" {
		s.WriteString("\n" + errorStyle.Render("Error: " + m.state.Error))
	}
	if m.state.Message != "" {
		s.WriteString("\n" + messageStyle.Render(m.state.Message))
	}

	// Help text
	help := "\n" + helpStyle.Render("j/k: navigate feeds | l: view articles | r: refresh | a: add feed | e: edit | d: delete feed | q: quit")
	s.WriteString(help)

	return s.String()
}

type errorMsg struct {
	err error
}

type feedDeletedMsg struct{}
type FeedAddedMsg struct{}
type FeedUpdatedMsg struct{}

var keys = keyMap{
	Up:    key.NewBinding(key.WithKeys("up", "k")),
	Down:  key.NewBinding(key.WithKeys("down", "j")),
	Left:  key.NewBinding(key.WithKeys("left", "h")),
	Right: key.NewBinding(key.WithKeys("right", "l")),
	Quit:  key.NewBinding(key.WithKeys("q", "ctrl+c")),
}

type keyMap struct {
	Up    key.Binding
	Down  key.Binding
	Left  key.Binding
	Right key.Binding
	Quit  key.Binding
}

var helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Margin(1)


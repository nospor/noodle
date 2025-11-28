package ui

import (
	"fmt"
	"io"
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
	selectedItemStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("170"))
	normalItemStyle   = lipgloss.NewStyle() // No padding, default color
	paneStyle         = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("62")).Padding(1, 2)
	errorStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	messageStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("46"))
	confirmStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true) // Orange/bold for confirmation
	// Styles for read/unread articles
	unreadArticleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("39")) // Bright blue for unread
	readArticleStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244")) // Gray for read
)

type feedItem struct {
	feed database.Feed
	unreadCount int
	totalCount  int
}

func (i feedItem) FilterValue() string { return i.feed.Title }
func (i feedItem) Title() string {
	if i.totalCount > 0 {
		return fmt.Sprintf("%s (%d/%d)", i.feed.Title, i.unreadCount, i.totalCount)
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

// articleDelegate is a custom delegate that styles articles differently based on read status
type articleDelegate struct {
	list.DefaultDelegate
}

func newArticleDelegate() *articleDelegate {
	d := &articleDelegate{
		DefaultDelegate: list.NewDefaultDelegate(),
	}
	d.Styles.SelectedTitle = selectedItemStyle
	d.Styles.SelectedDesc = selectedItemStyle
	return d
}

func (d *articleDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	articleItem, ok := item.(articleItem)
	if !ok {
		d.DefaultDelegate.Render(w, m, index, item)
		return
	}

	var titleStyle, descStyle lipgloss.Style
	if index == m.Index() {
		// Selected item - use selected style (color 170, no extra padding beyond default)
		titleStyle = selectedItemStyle
		descStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	} else {
		// Unselected item - color based on read status
		if !articleItem.article.IsRead {
			titleStyle = unreadArticleStyle
		} else {
			titleStyle = readArticleStyle
		}
		descStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	}

	title := articleItem.Title()
	desc := articleItem.Description()

	titleText := titleStyle.Render(title)
	if desc != "" {
		output := lipgloss.JoinVertical(lipgloss.Left, titleText, descStyle.Render(desc))
		fmt.Fprint(w, output)
	} else {
		fmt.Fprint(w, titleText)
	}
}

type MainModel struct {
	feedsList    list.Model
	articlesList list.Model
	state        *AppState
	width        int
	height       int
	activePane   string // "feeds" or "articles"
	confirmDelete bool  // true when waiting for delete confirmation
	initialLoadDone bool // true after first load completes
}

type refreshFeedMsg struct {
	feedURL string
	err     error
}

type refreshAllFeedsMsg struct {
	trigger bool // true if this is just a trigger, false if refresh completed
}

type loadArticlesMsg struct {
	articles []database.Article
	err      error
}

type loadFeedsMsg struct {
	feeds []database.Feed
	err   error
}

type autoRefreshTickMsg struct{}

type clearMessageMsg struct{}

func NewMainModel(state *AppState) *MainModel {
	m := &MainModel{
		state:      state,
		activePane: "feeds",
		width:      state.Width,
		height:     state.Height,
	}

	// Initialize feeds list
	feedsDelegate := list.NewDefaultDelegate()
	// Selected feeds: pink color, no padding
	feedsDelegate.Styles.SelectedTitle = selectedItemStyle
	feedsDelegate.Styles.SelectedDesc = selectedItemStyle
	// Unselected feeds: default color, no padding (same as selected for consistent padding)
	feedsDelegate.Styles.NormalTitle = normalItemStyle
	feedsDelegate.Styles.NormalDesc = normalItemStyle

	m.feedsList = list.New([]list.Item{}, feedsDelegate, 0, 0)
	m.feedsList.Title = "" // Title is shown in pane header instead
	m.feedsList.SetShowTitle(false) // Hide the title area completely
	m.feedsList.SetShowStatusBar(false)
	m.feedsList.SetShowHelp(false)
	m.feedsList.SetFilteringEnabled(false)

	// Initialize articles list with custom delegate for read/unread styling
	articlesDelegate := newArticleDelegate()
	m.articlesList = list.New([]list.Item{}, articlesDelegate, 0, 0)
	m.articlesList.Title = "" // Title is shown in pane header instead
	m.articlesList.SetShowTitle(false) // Hide the title area completely
	m.articlesList.SetShowStatusBar(false)
	m.articlesList.SetFilteringEnabled(false)

	// Set width/height if available
	if state.Width > 0 && state.Height > 0 {
		leftWidth := state.Width / 2
		rightWidth := state.Width - leftWidth - 4
		// Reserve space for help text (1 line) and message area (1 line) = 2 lines
		availableHeight := state.Height - 8
		m.feedsList.SetWidth(leftWidth - 6)
		m.feedsList.SetHeight(availableHeight)
		m.articlesList.SetWidth(rightWidth - 6)
		m.articlesList.SetHeight(availableHeight)
	}

	return m
}

func (m *MainModel) Init() tea.Cmd {
	return tea.Batch(m.loadFeeds(), m.loadArticles(), m.startAutoRefresh())
}

func (m *MainModel) loadFeeds() tea.Cmd {
	return func() tea.Msg {
		feeds, err := database.GetAllFeeds()
		if err != nil {
			return loadFeedsMsg{err: err}
		}
		
		// Sort feeds according to config.json order
		sortedFeeds := sortFeedsByConfigOrder(feeds, m.state.Config.Feeds)
		
		return loadFeedsMsg{feeds: sortedFeeds}
	}
}

// sortFeedsByConfigOrder sorts feeds according to the order in config.json
// Feeds not in config are appended at the end
func sortFeedsByConfigOrder(dbFeeds []database.Feed, configFeeds []config.Feed) []database.Feed {
	// Create a map for quick lookup
	feedMap := make(map[string]database.Feed)
	for _, feed := range dbFeeds {
		feedMap[feed.URL] = feed
	}
	
	// Track which feeds we've added
	added := make(map[string]bool)
	var sorted []database.Feed
	
	// Add feeds in config order
	for _, configFeed := range configFeeds {
		if dbFeed, exists := feedMap[configFeed.URL]; exists {
			sorted = append(sorted, dbFeed)
			added[configFeed.URL] = true
		}
	}
	
	// Append any feeds not in config at the end
	for _, dbFeed := range dbFeeds {
		if !added[dbFeed.URL] {
			sorted = append(sorted, dbFeed)
		}
	}
	
	return sorted
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

func (m *MainModel) startAutoRefresh() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg {
		return autoRefreshTickMsg{}
	})
}

func (m *MainModel) clearMessageAfter(duration time.Duration) tea.Cmd {
	return tea.Tick(duration, func(time.Time) tea.Msg {
		return clearMessageMsg{}
	})
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

		// Get existing feed from database to preserve its ID
		existingFeed, err := database.GetFeedByURL(feedURL)
		if err != nil {
			return refreshFeedMsg{feedURL: feedURL, err: fmt.Errorf("failed to get existing feed: %w", err)}
		}

		// Convert and save feed
		dbFeed := feed.ConvertFeedToDBFeed(parsedFeed, feedURL, configFeed.Title)
		// Preserve the existing feed ID if it exists
		if existingFeed != nil {
			dbFeed.ID = existingFeed.ID
		}
		if err := database.SaveFeed(dbFeed); err != nil {
			return refreshFeedMsg{feedURL: feedURL, err: err}
		}

		// If ID is still 0 after SaveFeed, get it from the database
		if dbFeed.ID == 0 {
			updatedFeed, err := database.GetFeedByURL(feedURL)
			if err != nil {
				return refreshFeedMsg{feedURL: feedURL, err: fmt.Errorf("failed to get feed ID after save: %w", err)}
			}
			if updatedFeed != nil {
				dbFeed.ID = updatedFeed.ID
			}
		}

		// Convert and save articles
		articles := feed.ConvertItemsToArticles(parsedFeed.Items)
		if err := database.SaveArticles(dbFeed.ID, articles); err != nil {
			return refreshFeedMsg{feedURL: feedURL, err: err}
		}

		return refreshFeedMsg{feedURL: feedURL}
	}
}

func (m *MainModel) refreshAllFeeds() tea.Cmd {
	return func() tea.Msg {
		// Refresh all feeds from config
		// Use the same logic as refreshFeed for consistency
		for _, configFeed := range m.state.Config.Feeds {
			parsedFeed, err := feed.FetchAndParseFeed(configFeed.URL)
			if err != nil {
				continue // Skip feeds that fail to fetch
			}

			// Get existing feed from database to preserve its ID
			existingFeed, err := database.GetFeedByURL(configFeed.URL)
			if err != nil {
				continue
			}

			if existingFeed == nil {
				// Feed doesn't exist yet, skip (should have been created during sync)
				continue
			}

			// Convert and save feed
			dbFeed := feed.ConvertFeedToDBFeed(parsedFeed, configFeed.URL, configFeed.Title)
			// Preserve the existing feed ID
			dbFeed.ID = existingFeed.ID
			if err := database.SaveFeed(dbFeed); err != nil {
				continue
			}

			// If ID is still 0 after SaveFeed, get it from the database
			if dbFeed.ID == 0 {
				updatedFeed, err := database.GetFeedByURL(configFeed.URL)
				if err != nil {
					continue
				}
				if updatedFeed != nil {
					dbFeed.ID = updatedFeed.ID
				}
			}

			// Convert and save articles
			articles := feed.ConvertItemsToArticles(parsedFeed.Items)
			if err := database.SaveArticles(dbFeed.ID, articles); err != nil {
				// Continue with other feeds even if one fails
				continue
			}
		}
		// Return message to trigger reload
		return refreshAllFeedsMsg{trigger: false}
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
		// Account for borders, padding, title, help text (1 line), and message area (1 line) = 8 lines total
		availableHeight := msg.Height - 8
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
			total, _ := database.GetTotalCount(f.ID)
			items[i] = feedItem{feed: f, unreadCount: unread, totalCount: total}
		}
		m.feedsList.SetItems(items)
		if len(msg.feeds) > 0 {
			// Ensure we have a valid selection index
			if m.state.SelectedFeedIndex < 0 || m.state.SelectedFeedIndex >= len(msg.feeds) {
				m.state.SelectedFeedIndex = 0
			}
			// Ensure list is properly sized before selecting
			if m.width > 0 && m.height > 0 {
				leftWidth := m.width / 2
				availableHeight := m.height - 8
				m.feedsList.SetWidth(leftWidth - 6)
				m.feedsList.SetHeight(availableHeight)
			}
			// For index 0, simulate a down then up key press to properly position the list
			// This works around the scrolling issue with Select(0)
			if m.state.SelectedFeedIndex == 0 {
				// Simulate navigation to position list correctly
				downKey := tea.KeyMsg{Type: tea.KeyDown}
				upKey := tea.KeyMsg{Type: tea.KeyUp}
				m.feedsList, _ = m.feedsList.Update(downKey)
				m.feedsList, _ = m.feedsList.Update(upKey)
				// Now we should be at index 0 with proper positioning
			} else {
				m.feedsList.Select(m.state.SelectedFeedIndex)
			}
		}
		// After loading feeds, trigger refresh if this is the first load
		if !m.initialLoadDone {
			m.initialLoadDone = true
			// This is the initial load, trigger refresh after loading articles
			return m, tea.Batch(m.loadArticles(), m.refreshAllFeeds())
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
			// Clear message after 2 seconds
			return m, tea.Batch(m.loadFeeds(), m.loadArticles(), m.clearMessageAfter(2*time.Second))
		}
		return m, tea.Batch(m.loadFeeds(), m.loadArticles())

	case refreshAllFeedsMsg:
		// Reload feeds and articles after startup refresh
		return m, tea.Batch(m.loadFeeds(), m.loadArticles())

	case FeedAddedMsg, FeedUpdatedMsg:
		return m, tea.Batch(m.loadFeeds(), m.loadArticles())

	case feedDeletedMsg:
		return m, tea.Batch(m.loadFeeds(), m.loadArticles())

	case autoRefreshTickMsg:
		// Periodically reload feeds and articles to pick up background refresh updates
		return m, tea.Batch(m.loadFeeds(), m.loadArticles(), m.startAutoRefresh())

	case clearMessageMsg:
		m.state.Message = ""
		return m, nil

	case tea.KeyMsg:
		// If in delete confirmation mode, only handle y/n and escape
		if m.confirmDelete {
			switch msg.String() {
			case "y", "Y":
				if m.activePane == "feeds" && len(m.state.Feeds) > 0 {
					feed := m.state.Feeds[m.state.SelectedFeedIndex]
					m.confirmDelete = false
					return m, m.deleteFeed(feed.URL)
				}
			case "n", "N", "esc":
				m.confirmDelete = false
				return m, nil
			}
			return m, nil
		}

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
				// Don't reset SelectedArticleIndex - preserve it if switching back
				// Only reset if it's invalid
				if m.state.SelectedArticleIndex < 0 || m.state.SelectedArticleIndex >= len(m.state.Articles) {
					m.state.SelectedArticleIndex = 0
				}
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
				// Start delete confirmation
				m.confirmDelete = true
				return m, nil
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
	feedsTitle := fmt.Sprintf("Noodle - Feeds (%d)", len(m.state.Feeds))
	feedsPane := paneStyle.Width(m.width/2 - 2).Render(feedsTitle + "\n\n" + feedsView)

	// Articles pane
	articlesView := m.articlesList.View()
	articlesTitle := "Articles"
	if len(m.state.Feeds) > 0 && m.state.SelectedFeedIndex < len(m.state.Feeds) {
		feed := m.state.Feeds[m.state.SelectedFeedIndex]
		unread, _ := database.GetUnreadCount(feed.ID)
		articlesTitle += fmt.Sprintf(" (%d unread)", unread)
	}
	articlesPane := paneStyle.Width(m.width/2 - 2).Render(articlesTitle + "\n\n" + articlesView)

	// Combine panes
	s.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, feedsPane, articlesPane))

	// Footer area: help text + message area
	// Always render both to maintain consistent height
	var help string
	if m.confirmDelete {
		helpText := "Are you sure you want to delete this feed? [y]es / [n]o"
		help = "\n" + confirmStyle.Render(helpText)
	} else {
		helpText := "j/k: navigate feeds | l: view articles | r: refresh | a: add feed | e: edit | d: delete feed | q: quit"
		help = "\n" + helpStyle.Render(helpText)
	}
	s.WriteString(help)

	// Message area - always render exactly one line to prevent layout jumps
	messageLine := ""
	if m.state.Error != "" {
		errorText := "Error: " + m.state.Error
		// Truncate to fit width (account for ANSI color codes)
		maxLen := m.width - 10 // Leave some margin
		if len(errorText) > maxLen {
			errorText = errorText[:maxLen-3] + "..."
		}
		messageLine = errorStyle.Render(errorText)
	} else if m.state.Message != "" {
		messageText := m.state.Message
		// Truncate to fit width (account for ANSI color codes)
		maxLen := m.width - 10 // Leave some margin
		if len(messageText) > maxLen {
			messageText = messageText[:maxLen-3] + "..."
		}
		messageLine = messageStyle.Render(messageText)
	}
	// Always add the message line (empty string when no message, but still takes up space)
	// The helpStyle margin already provides spacing, so we just add the message line
	s.WriteString("\n" + messageLine)

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

var helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))


package ui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"noodle/internal/database"
)

type ArticleModel struct {
	articlesList         list.Model
	contentViewport      viewport.Model
	state                *AppState
	width                int
	height               int
	articleViewStartTime *time.Time // Track when article was first viewed
	timerArticleID       int64      // Track which article the timer is for
	confirmDelete        bool       // true when waiting for delete confirmation for favorite article
	confirmBulkDelete    bool       // true when waiting for bulk delete confirmation
}

type loadArticleContentMsg struct {
	article *database.Article
}

type favoriteToggledMsg struct {
	articleID  int64
	isFavorite bool
}

type autoMarkReadCheckMsg struct{}

type bulkDeleteCompletedMsg struct {
	deletedCount int
}

func NewArticleModel(state *AppState) *ArticleModel {
	m := &ArticleModel{
		state:          state,
		width:          state.Width,
		height:         state.Height,
		timerArticleID: -1, // Initialize to invalid ID
	}

	// Initialize articles list with custom delegate for read/unread styling
	articlesDelegate := newArticleDelegate()
	m.articlesList = list.New([]list.Item{}, articlesDelegate, 0, 0)
	m.articlesList.Title = ""          // Title is shown in pane header instead
	m.articlesList.SetShowTitle(false) // Hide the title area completely
	m.articlesList.SetShowStatusBar(false)
	m.articlesList.SetShowHelp(false) // Hide built-in help text
	m.articlesList.SetFilteringEnabled(false)

	// Initialize content viewport
	m.contentViewport = viewport.New(0, 0)

	// Set width/height if available
	if state.Width > 0 && state.Height > 0 {
		leftWidth := state.Width / 2
		rightWidth := state.Width - leftWidth - 4
		contentHeight := state.Height - 6
		m.articlesList.SetWidth(leftWidth - 4)
		m.articlesList.SetHeight(contentHeight)
		m.contentViewport.Width = rightWidth - 6
		m.contentViewport.Height = contentHeight
	}

	// Load articles for current feed
	articles, err := database.GetArticles(state.CurrentFeedID)
	if err == nil {
		state.Articles = articles
		items := make([]list.Item, len(articles))
		for i, a := range articles {
			items[i] = articleItem{article: a}
		}
		m.articlesList.SetItems(items)
		if len(articles) > 0 {
			// Ensure we have a valid selected index
			if state.SelectedArticleIndex < 0 || state.SelectedArticleIndex >= len(articles) {
				state.SelectedArticleIndex = 0
			}
			m.articlesList.Select(state.SelectedArticleIndex)
			// Set initial article - timer will be started in Init() or via explicit call
			article := articles[state.SelectedArticleIndex]
			m.state.CurrentArticle = &article
			m.updateContentViewport()
		} else {
			m.contentViewport.SetContent("No articles available")
		}
	} else {
		m.contentViewport.SetContent("No article selected")
	}

	return m
}

func (m *ArticleModel) Init() tea.Cmd {
	// Always set up and start the timer for the current article
	if len(m.state.Articles) > 0 && m.state.SelectedArticleIndex < len(m.state.Articles) {
		// Ensure CurrentArticle is set
		if m.state.CurrentArticle == nil {
			m.state.CurrentArticle = &m.state.Articles[m.state.SelectedArticleIndex]
			m.updateContentViewport()
		}
		// Always reset timer fields and start the timer
		if m.state.CurrentArticle != nil {
			now := time.Now()
			m.articleViewStartTime = &now
			m.timerArticleID = m.state.CurrentArticle.ID
			return m.startAutoMarkReadCheck()
		}
	}
	return nil
}

func (m *ArticleModel) loadArticleContent(article database.Article) tea.Cmd {
	now := time.Now()
	m.articleViewStartTime = &now
	m.timerArticleID = article.ID // Track which article the timer is for
	m.state.CurrentArticle = &article
	m.updateContentViewport()
	// Start periodic check timer (every 1 second) to check if 5 seconds have passed
	return tea.Batch(
		func() tea.Msg {
			return loadArticleContentMsg{article: &article}
		},
		m.startAutoMarkReadCheck(),
	)
}

func (m *ArticleModel) startAutoMarkReadCheck() tea.Cmd {
	return tea.Tick(1*time.Second, func(time.Time) tea.Msg {
		return autoMarkReadCheckMsg{}
	})
}

func (m *ArticleModel) updateContentViewport() {
	if m.state.CurrentArticle != nil {
		content := renderArticleContent(m.state.CurrentArticle)
		m.contentViewport.SetContent(content)
		m.contentViewport.GotoTop()
	} else {
		m.contentViewport.SetContent("No article selected")
	}
}

func (m *ArticleModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.state.Width = msg.Width
		m.state.Height = msg.Height
		leftWidth := msg.Width / 2
		rightWidth := msg.Width - leftWidth - 4
		contentHeight := msg.Height - 6
		m.articlesList.SetWidth(leftWidth - 4)
		m.articlesList.SetHeight(contentHeight)
		m.contentViewport.Width = rightWidth - 6
		m.contentViewport.Height = contentHeight
		m.updateContentViewport()
		return m, nil

	case loadArticleContentMsg:
		return m, nil

	case autoMarkReadCheckMsg:
		// Check if we should mark the current article as read
		// Only mark as read if:
		// 1. We have a current article
		// 2. The article is not already read
		// 3. We're still viewing the same article (timerArticleID matches)
		// 4. At least the configured time has passed since we started viewing it
		if m.state.CurrentArticle != nil &&
			m.articleViewStartTime != nil &&
			m.state.CurrentArticle.ID == m.timerArticleID &&
			!m.state.CurrentArticle.IsRead {
			elapsed := time.Since(*m.articleViewStartTime)
			// Get configured time (default 5 seconds if not set)
			setAsReadAfter := 5
			if m.state.Config != nil && m.state.Config.SetAsReadAfter > 0 {
				setAsReadAfter = m.state.Config.SetAsReadAfter
			}
			if elapsed >= time.Duration(setAsReadAfter)*time.Second {
				// Mark as read and stop the periodic check
				return m, m.markRead(m.state.CurrentArticle.ID, true)
			}
			// Continue checking every second
			return m, m.startAutoMarkReadCheck()
		}
		// If article changed or already read, stop checking
		return m, nil

	case favoriteToggledMsg:
		// Update the current article's favorite status immediately
		if m.state.CurrentArticle != nil && m.state.CurrentArticle.ID == msg.articleID {
			m.state.CurrentArticle.IsFavorite = msg.isFavorite
			// Also update it in the articles slice
			if m.state.SelectedArticleIndex < len(m.state.Articles) {
				for i := range m.state.Articles {
					if m.state.Articles[i].ID == msg.articleID {
						m.state.Articles[i].IsFavorite = msg.isFavorite
						break
					}
				}
				// Update the list items to reflect the change
				items := make([]list.Item, len(m.state.Articles))
				for i, a := range m.state.Articles {
					items[i] = articleItem{article: a}
				}
				m.articlesList.SetItems(items)
				m.articlesList.Select(m.state.SelectedArticleIndex)
			}
			// Refresh the content viewport to show the updated star
			m.updateContentViewport()
		}
		return m, nil

	case readStatusChangedMsg:
		// Update the current article's read status immediately
		if m.state.CurrentArticle != nil && m.state.CurrentArticle.ID == msg.articleID {
			m.state.CurrentArticle.IsRead = msg.isRead
			// Also update it in the articles slice
			if m.state.SelectedArticleIndex < len(m.state.Articles) {
				for i := range m.state.Articles {
					if m.state.Articles[i].ID == msg.articleID {
						m.state.Articles[i].IsRead = msg.isRead
						break
					}
				}
				// Update the list items to reflect the change
				items := make([]list.Item, len(m.state.Articles))
				for i, a := range m.state.Articles {
					items[i] = articleItem{article: a}
				}
				m.articlesList.SetItems(items)
				m.articlesList.Select(m.state.SelectedArticleIndex)
			}
			// Refresh the content viewport to show the updated read status
			m.updateContentViewport()
		}
		return m, nil

	case articleDeletedMsg:
		// Reload articles from database and update the list immediately
		articles, _ := database.GetArticles(m.state.CurrentFeedID)
		m.state.Articles = articles
		items := make([]list.Item, len(articles))
		for i, a := range articles {
			items[i] = articleItem{article: a}
		}
		m.articlesList.SetItems(items)
		if len(articles) > 0 {
			// Adjust selected index if needed
			if m.state.SelectedArticleIndex >= len(articles) {
				m.state.SelectedArticleIndex = len(articles) - 1
			}
			m.articlesList.Select(m.state.SelectedArticleIndex)
			// Update current article
			if m.state.SelectedArticleIndex < len(articles) {
				m.state.CurrentArticle = &articles[m.state.SelectedArticleIndex]
				m.updateContentViewport()
			} else {
				m.state.CurrentArticle = nil
				m.contentViewport.SetContent("No article selected")
			}
		} else {
			m.state.SelectedArticleIndex = -1
			m.state.CurrentArticle = nil
			m.contentViewport.SetContent("No articles available")
		}
		return m, nil

	case bulkDeleteCompletedMsg:
		// Reload articles from database and update the list immediately
		articles, _ := database.GetArticles(m.state.CurrentFeedID)
		m.state.Articles = articles
		items := make([]list.Item, len(articles))
		for i, a := range articles {
			items[i] = articleItem{article: a}
		}
		m.articlesList.SetItems(items)
		if len(articles) > 0 {
			// Adjust selected index if needed
			if m.state.SelectedArticleIndex >= len(articles) {
				m.state.SelectedArticleIndex = len(articles) - 1
			}
			m.articlesList.Select(m.state.SelectedArticleIndex)
			// Update current article
			if m.state.SelectedArticleIndex < len(articles) {
				m.state.CurrentArticle = &articles[m.state.SelectedArticleIndex]
				m.updateContentViewport()
			} else {
				m.state.CurrentArticle = nil
				m.contentViewport.SetContent("No article selected")
			}
		} else {
			m.state.SelectedArticleIndex = -1
			m.state.CurrentArticle = nil
			m.contentViewport.SetContent("No articles available")
		}
		return m, nil

	case tea.KeyMsg:
		// If in delete confirmation mode, only handle y/n and escape
		if m.confirmDelete {
			switch msg.String() {
			case "y", "Y":
				if m.state.CurrentArticle != nil {
					m.confirmDelete = false
					return m, m.deleteArticle(m.state.CurrentArticle.ID)
				}
			case "n", "N", "esc":
				m.confirmDelete = false
				return m, nil
			}
			return m, nil
		}

		// If in bulk delete confirmation mode, only handle y/n and escape
		if m.confirmBulkDelete {
			switch msg.String() {
			case "y", "Y":
				m.confirmBulkDelete = false
				return m, m.deleteNonFavoriteArticles()
			case "n", "N", "esc":
				m.confirmBulkDelete = false
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
			m.articlesList, _ = m.articlesList.Update(msg)
			if m.articlesList.Index() < len(m.state.Articles) {
				m.state.SelectedArticleIndex = m.articlesList.Index()
				article := m.state.Articles[m.state.SelectedArticleIndex]
				return m, m.loadArticleContent(article)
			}
			return m, nil

		case key.Matches(msg, keys.Down), msg.String() == "j":
			m.articlesList, _ = m.articlesList.Update(msg)
			if m.articlesList.Index() < len(m.state.Articles) {
				m.state.SelectedArticleIndex = m.articlesList.Index()
				article := m.state.Articles[m.state.SelectedArticleIndex]
				return m, m.loadArticleContent(article)
			}
			return m, nil

		case key.Matches(msg, keys.Left), msg.String() == "h", msg.String() == "esc":
			// Return to main view - preserve SelectedArticleIndex for when we come back
			m.state.View = MainView
			mainModel := NewMainModel(m.state)
			return mainModel, mainModel.Init()

		case key.Matches(msg, keys.Right), msg.String() == "l":
			// 'l' and right arrow do nothing in article view
			return m, nil

		case msg.String() == "enter":
			// Enter doesn't do anything special in article view
			return m, nil

		case msg.String() == "o":
			if m.state.CurrentArticle != nil {
				// Open in browser and mark as read
				return m, tea.Batch(
					m.openBrowser(m.state.CurrentArticle.Link),
					m.markRead(m.state.CurrentArticle.ID, true),
				)
			}

		case msg.String() == "r":
			if m.state.CurrentArticle != nil {
				return m, m.markRead(m.state.CurrentArticle.ID, true)
			}

		case msg.String() == "u":
			if m.state.CurrentArticle != nil {
				return m, m.markRead(m.state.CurrentArticle.ID, false)
			}

		case msg.String() == "f":
			if m.state.CurrentArticle != nil {
				return m, m.toggleFavorite(m.state.CurrentArticle.ID, !m.state.CurrentArticle.IsFavorite)
			}

		case msg.String() == "d":
			if m.state.CurrentArticle != nil {
				// If article is favorite, ask for confirmation
				if m.state.CurrentArticle.IsFavorite {
					m.confirmDelete = true
					return m, nil
				}
				// Otherwise, delete immediately
				return m, m.deleteArticle(m.state.CurrentArticle.ID)
			}

		case msg.String() == "D":
			// Delete all non-favorite articles from current feed
			m.confirmBulkDelete = true
			return m, nil

		case msg.String() == "H":
			// Jump to previous page
			m.articlesList.PrevPage()
			// Update selected article after page change
			if m.articlesList.Index() < len(m.state.Articles) {
				m.state.SelectedArticleIndex = m.articlesList.Index()
				article := m.state.Articles[m.state.SelectedArticleIndex]
				return m, m.loadArticleContent(article)
			}
			return m, nil

		case msg.String() == "L":
			// Jump to next page
			m.articlesList.NextPage()
			// Update selected article after page change
			if m.articlesList.Index() < len(m.state.Articles) {
				m.state.SelectedArticleIndex = m.articlesList.Index()
				article := m.state.Articles[m.state.SelectedArticleIndex]
				return m, m.loadArticleContent(article)
			}
			return m, nil
		}

		// Handle PgUp/PgDn - scroll content viewport
		if msg.String() == "pgup" || msg.String() == "pgdown" {
			var cmd tea.Cmd
			m.contentViewport, cmd = m.contentViewport.Update(msg)
			return m, cmd
		}

		// Update list for other keys
		m.articlesList, _ = m.articlesList.Update(msg)

		// If list index changed, update selected article
		if m.articlesList.Index() < len(m.state.Articles) && m.articlesList.Index() != m.state.SelectedArticleIndex {
			m.state.SelectedArticleIndex = m.articlesList.Index()
			article := m.state.Articles[m.state.SelectedArticleIndex]
			return m, m.loadArticleContent(article)
		}

		return m, nil
	}

	return m, nil
}

type readStatusChangedMsg struct {
	articleID int64
	isRead    bool
}

type articleDeletedMsg struct {
	articleID int64
}

func (m *ArticleModel) markRead(articleID int64, isRead bool) tea.Cmd {
	return func() tea.Msg {
		if err := database.MarkRead(articleID, isRead); err != nil {
			return errorMsg{err: err}
		}
		// Return a message to update the UI immediately
		return readStatusChangedMsg{articleID: articleID, isRead: isRead}
	}
}

func (m *ArticleModel) toggleFavorite(articleID int64, isFavorite bool) tea.Cmd {
	return func() tea.Msg {
		if err := database.MarkFavorite(articleID, isFavorite); err != nil {
			return errorMsg{err: err}
		}
		// Return a message to update the UI immediately
		return favoriteToggledMsg{articleID: articleID, isFavorite: isFavorite}
	}
}

func (m *ArticleModel) deleteArticle(articleID int64) tea.Cmd {
	return func() tea.Msg {
		if err := database.DeleteArticle(articleID); err != nil {
			return errorMsg{err: err}
		}
		// Return a message to update the UI immediately
		return articleDeletedMsg{articleID: articleID}
	}
}

func (m *ArticleModel) deleteNonFavoriteArticles() tea.Cmd {
	return func() tea.Msg {
		deletedCount, err := database.DeleteNonFavoriteArticles(m.state.CurrentFeedID)
		if err != nil {
			return errorMsg{err: err}
		}
		// Return a message to update the UI immediately
		return bulkDeleteCompletedMsg{deletedCount: deletedCount}
	}
}

func (m *ArticleModel) openBrowser(url string) tea.Cmd {
	return func() tea.Msg {
		if err := openBrowser(url); err != nil {
			return errorMsg{err: fmt.Errorf("failed to open browser: %w", err)}
		}
		return nil
	}
}

func (m *ArticleModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var s strings.Builder

	// Articles pane
	articlesView := m.articlesList.View()
	articlesTitle := "Articles"
	articlesPane := paneStyle.Width(m.width/2 - 2).Render(articlesTitle + "\n\n" + articlesView)

	// Content pane - fixed height matching articles pane
	contentTitle := "Content"
	contentPane := paneStyle.Width(m.width/2 - 2).Height(m.height - 6).Render(contentTitle + "\n\n" + m.contentViewport.View())

	// Combine panes
	s.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, articlesPane, contentPane))

	// Error and message
	if m.state.Error != "" {
		s.WriteString("\n" + errorStyle.Render("Error: "+m.state.Error))
	}
	if m.state.Message != "" {
		s.WriteString("\n" + messageStyle.Render(m.state.Message))
	}

	// Help text or confirmation message
	var help string
	if m.confirmDelete {
		helpText := "Are you sure you want to delete this favorite article? [y]es / [n]o"
		help = "\n" + confirmStyle.Render(helpText)
	} else if m.confirmBulkDelete {
		helpText := "Are you sure you want to delete all non-favorite articles from this feed? [y]es / [n]o"
		help = "\n" + confirmStyle.Render(helpText)
	} else {
		helpText1 := "j/k: navigate articles | H/L: prev/next page | PgUp/PgDn: scroll content | g/home: go to start | G/end: go to end | o: open in browser"
		helpText2 := "r: mark read | u: mark unread | f: toggle favorite | d: delete | D: delete all non-favorites | h/Esc: back | q: quit"
		help = "\n" + helpStyle.Render(helpText1) + "\n" + helpStyle.Render(helpText2)
	}
	s.WriteString(help)

	return s.String()
}

func renderArticleContent(article *database.Article) string {
	var s strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170")).MarginBottom(1)
	s.WriteString(titleStyle.Render(article.Title))
	s.WriteString("\n\n")

	// Link
	linkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Underline(true)
	s.WriteString(linkStyle.Render(article.Link))
	s.WriteString("\n\n")

	// Published date
	if article.PublishedAt != nil {
		dateStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
		s.WriteString(dateStyle.Render("Published: " + article.PublishedAt.Format("2006-01-02 15:04:05")))
		s.WriteString("\n\n")
	}

	// Status
	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	status := ""
	if article.IsRead {
		status += "✓ Read "
	}
	if article.IsFavorite {
		status += "★ Favorite"
	}
	if status != "" {
		s.WriteString(statusStyle.Render(status))
		s.WriteString("\n\n")
	}

	// Content
	// Strip HTML tags for now (simple approach)
	content := stripHTML(article.Content)
	contentStyle := lipgloss.NewStyle().Width(80)
	s.WriteString(contentStyle.Render(content))

	return s.String()
}

func stripHTML(html string) string {
	// Simple HTML tag removal
	// For a production app, you'd want to use a proper HTML parser
	var result strings.Builder
	inTag := false
	for _, r := range html {
		if r == '<' {
			inTag = true
		} else if r == '>' {
			inTag = false
		} else if !inTag {
			result.WriteRune(r)
		}
	}
	return strings.TrimSpace(result.String())
}

func openBrowser(url string) error {
	cmd := exec.Command("xdg-open", url)
	return cmd.Run()
}

package ui

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"noodle/internal/database"
)

type ArticleModel struct {
	articlesList list.Model
	state        *AppState
	width        int
	height       int
}

type loadArticleContentMsg struct {
	article *database.Article
}

func NewArticleModel(state *AppState) *ArticleModel {
	m := &ArticleModel{
		state:  state,
		width:  state.Width,
		height: state.Height,
	}

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
		m.articlesList.SetWidth(leftWidth - 4)
		m.articlesList.SetHeight(state.Height - 6)
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
			// Load the selected article's content
			m.state.CurrentArticle = &articles[state.SelectedArticleIndex]
		}
	}

	return m
}

func (m *ArticleModel) Init() tea.Cmd {
	// If we have articles and a selected article, ensure content is loaded
	if len(m.state.Articles) > 0 && m.state.SelectedArticleIndex < len(m.state.Articles) {
		if m.state.CurrentArticle == nil {
			m.state.CurrentArticle = &m.state.Articles[m.state.SelectedArticleIndex]
		}
	}
	return nil
}

func (m *ArticleModel) loadArticleContent(article database.Article) tea.Cmd {
	return func() tea.Msg {
		m.state.CurrentArticle = &article
		return loadArticleContentMsg{article: &article}
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
		m.articlesList.SetWidth(leftWidth - 4)
		m.articlesList.SetHeight(msg.Height - 6)
		return m, nil

	case loadArticleContentMsg:
		return m, nil

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
			// Return to main view
			m.state.View = MainView
			mainModel := NewMainModel(m.state)
			return mainModel, mainModel.Init()

		case msg.String() == "enter":
			// Enter doesn't do anything special in article view
			return m, nil

		case msg.String() == "o":
			if m.state.CurrentArticle != nil {
				return m, m.openBrowser(m.state.CurrentArticle.Link)
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

		case msg.String() == "x":
			if m.state.CurrentArticle != nil {
				return m, m.deleteArticle(m.state.CurrentArticle.ID)
			}
		}

		// Ignore 'l' and right arrow - do nothing
		if msg.String() == "l" || key.Matches(msg, keys.Right) {
			return m, nil
		}

		// Update list for other keys (including PgUp/PgDn which bubbles handles)
		m.articlesList, _ = m.articlesList.Update(msg)
		
		// If list index changed (e.g., from PgUp/PgDn), update selected article
		if m.articlesList.Index() < len(m.state.Articles) && m.articlesList.Index() != m.state.SelectedArticleIndex {
			m.state.SelectedArticleIndex = m.articlesList.Index()
			article := m.state.Articles[m.state.SelectedArticleIndex]
			return m, m.loadArticleContent(article)
		}
		
		return m, nil
	}

	return m, nil
}

func (m *ArticleModel) markRead(articleID int64, isRead bool) tea.Cmd {
	return func() tea.Msg {
		if err := database.MarkRead(articleID, isRead); err != nil {
			return errorMsg{err: err}
		}
		// Reload articles
		articles, _ := database.GetArticles(m.state.CurrentFeedID)
		m.state.Articles = articles
		items := make([]list.Item, len(articles))
		for i, a := range articles {
			items[i] = articleItem{article: a}
		}
		m.articlesList.SetItems(items)
		if m.state.SelectedArticleIndex < len(articles) {
			m.articlesList.Select(m.state.SelectedArticleIndex)
		}
		return nil
	}
}

func (m *ArticleModel) toggleFavorite(articleID int64, isFavorite bool) tea.Cmd {
	return func() tea.Msg {
		if err := database.MarkFavorite(articleID, isFavorite); err != nil {
			return errorMsg{err: err}
		}
		// Reload articles
		articles, _ := database.GetArticles(m.state.CurrentFeedID)
		m.state.Articles = articles
		items := make([]list.Item, len(articles))
		for i, a := range articles {
			items[i] = articleItem{article: a}
		}
		m.articlesList.SetItems(items)
		if m.state.SelectedArticleIndex < len(articles) {
			m.articlesList.Select(m.state.SelectedArticleIndex)
		}
		return nil
	}
}

func (m *ArticleModel) deleteArticle(articleID int64) tea.Cmd {
	return func() tea.Msg {
		if err := database.DeleteArticle(articleID); err != nil {
			return errorMsg{err: err}
		}
		// Reload articles
		articles, _ := database.GetArticles(m.state.CurrentFeedID)
		m.state.Articles = articles
		items := make([]list.Item, len(articles))
		for i, a := range articles {
			items[i] = articleItem{article: a}
		}
		m.articlesList.SetItems(items)
		if len(articles) > 0 {
			if m.state.SelectedArticleIndex >= len(articles) {
				m.state.SelectedArticleIndex = len(articles) - 1
			}
			m.articlesList.Select(m.state.SelectedArticleIndex)
			if m.state.SelectedArticleIndex < len(articles) {
				m.loadArticleContent(articles[m.state.SelectedArticleIndex])
			}
		} else {
			m.state.CurrentArticle = nil
		}
		return nil
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
	articlesPane := paneStyle.Width(m.width/2 - 2).Render(articlesTitle + "\n" + articlesView)

	// Content pane
	contentTitle := "Content"
	content := ""
	if m.state.CurrentArticle != nil {
		content = renderArticleContent(m.state.CurrentArticle)
	} else {
		content = "No article selected"
	}
	contentPane := paneStyle.Width(m.width/2 - 2).Render(contentTitle + "\n" + content)

	// Combine panes
	s.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, articlesPane, contentPane))

	// Error and message
	if m.state.Error != "" {
		s.WriteString("\n" + errorStyle.Render("Error: "+m.state.Error))
	}
	if m.state.Message != "" {
		s.WriteString("\n" + messageStyle.Render(m.state.Message))
	}

	// Help text
	help := "\n" + helpStyle.Render("j/k: navigate | PgUp/PgDn: page | o: open in browser | r: mark read | u: unread | f: favorite | x: delete | h/Esc: back | q: quit")
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


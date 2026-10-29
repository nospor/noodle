package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"noodle/internal/wykop"
)

// loadWykopPage is the comments fetch used by the article view.
// Comments are kept on the model only and are never written to the database.
var loadWykopPage = wykop.FetchPage

type commentsPageMsg struct {
	gen         int
	articleLink string
	appendPage  bool
	page        wykop.Page
	err         error
}

func (m *ArticleModel) isWykopFeed() bool {
	if m.state == nil {
		return false
	}
	for _, feed := range m.state.Feeds {
		if feed.ID == m.state.CurrentFeedID {
			return wykop.IsSite(feed.URL)
		}
	}
	return false
}

func (m *ArticleModel) openComments() tea.Cmd {
	m.commentsGen++
	m.commentsOpen = true
	m.commentsLoading = false
	m.comments = nil
	m.commentsNextURL = ""
	m.commentsErr = ""
	m.commentsHint = ""
	m.resizeCommentsViewport()

	if m.state.CurrentArticle == nil {
		m.commentsErr = "No article selected"
		m.syncCommentsView(true)
		return nil
	}
	pageURL, ok := wykop.LinkEntryURL(m.state.CurrentArticle.Link)
	if !ok {
		m.commentsErr = "This entry has no Wykop comment page"
		m.syncCommentsView(true)
		return nil
	}
	m.commentsLoading = true
	m.syncCommentsView(true)
	return m.fetchComments(pageURL, false)
}

func (m *ArticleModel) closeComments() {
	m.commentsGen++
	m.commentsOpen = false
	m.commentsLoading = false
	m.comments = nil
	m.commentsNextURL = ""
	m.commentsErr = ""
	m.commentsHint = ""
	m.commentsViewport.SetContent("")
}

func (m *ArticleModel) fetchComments(pageURL string, appendPage bool) tea.Cmd {
	gen := m.commentsGen
	link := ""
	if m.state.CurrentArticle != nil {
		link = m.state.CurrentArticle.Link
	}
	return func() tea.Msg {
		page, err := loadWykopPage(pageURL)
		return commentsPageMsg{
			gen:         gen,
			articleLink: link,
			appendPage:  appendPage,
			page:        page,
			err:         err,
		}
	}
}

func (m *ArticleModel) loadNextComments() tea.Cmd {
	if m.commentsLoading || m.commentsNextURL == "" {
		return nil
	}
	if !m.commentsViewport.AtBottom() {
		m.commentsHint = "Scroll to the bottom first"
		return nil
	}
	m.commentsHint = ""
	m.commentsLoading = true
	return m.fetchComments(m.commentsNextURL, true)
}

func (m *ArticleModel) handleCommentsPage(msg commentsPageMsg) {
	if !m.commentsOpen || msg.gen != m.commentsGen {
		return
	}
	if m.state.CurrentArticle == nil || m.state.CurrentArticle.Link != msg.articleLink {
		return
	}
	m.commentsLoading = false
	if msg.err != nil {
		if len(m.comments) == 0 {
			m.commentsErr = msg.err.Error()
		} else {
			m.commentsHint = "Couldn't load the next page"
		}
		m.syncCommentsView(false)
		return
	}
	if msg.appendPage {
		m.comments = appendComments(m.comments, msg.page.Comments)
		if len(msg.page.Comments) == 0 {
			m.commentsNextURL = ""
		} else {
			m.commentsNextURL = msg.page.NextURL
		}
	} else {
		m.comments = msg.page.Comments
		m.commentsNextURL = msg.page.NextURL
	}
	m.commentsErr = ""
	m.syncCommentsView(!msg.appendPage)
}

func appendComments(dst, src []wykop.Comment) []wykop.Comment {
	seen := make(map[string]bool, len(dst))
	for _, comment := range dst {
		if comment.ID != "" {
			seen[comment.ID] = true
		}
	}
	for _, comment := range src {
		if comment.ID != "" && seen[comment.ID] {
			continue
		}
		dst = append(dst, comment)
	}
	return dst
}

func (m *ArticleModel) updateCommentsKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case msg.String() == "c" || msg.String() == "esc":
		m.closeComments()
		return m, nil
	case msg.String() == "pgup":
		m.commentsViewport.HalfPageUp()
		m.commentsHint = ""
		return m, nil
	case msg.String() == "pgdown":
		m.commentsViewport.HalfPageDown()
		m.commentsHint = ""
		return m, nil
	case msg.String() == "j" || msg.String() == "down":
		m.commentsViewport.LineDown(1)
		m.commentsHint = ""
		return m, nil
	case msg.String() == "k" || msg.String() == "up":
		m.commentsViewport.LineUp(1)
		m.commentsHint = ""
		return m, nil
	case msg.String() == "g" || msg.String() == "home":
		m.commentsViewport.GotoTop()
		m.commentsHint = ""
		return m, nil
	case msg.String() == "G" || msg.String() == "end":
		m.commentsViewport.GotoBottom()
		m.commentsHint = ""
		return m, nil
	case msg.String() == "m":
		return m, m.loadNextComments()
	default:
		return m, nil
	}
}

func (m *ArticleModel) resizeCommentsViewport() {
	_, viewHeight, viewWidth := m.commentsBox()
	if viewWidth < 20 {
		viewWidth = 20
	}
	if viewHeight < 3 {
		viewHeight = 3
	}
	m.commentsViewport.Width = viewWidth
	m.commentsViewport.Height = viewHeight
}

// commentsBox returns the popup content width, the viewport height, and the
// viewport width. The viewport sits under a one-line title inside the pane,
// and the pane's border and padding sit outside that text.
func (m *ArticleModel) commentsBox() (boxWidth, viewHeight, viewWidth int) {
	boxWidth = m.width * 4 / 5
	if boxWidth > m.width-2 {
		boxWidth = m.width - 2
	}
	if boxWidth < 24 {
		boxWidth = 24
	}
	// pane Width includes horizontal padding (2+2). Border adds 2 columns.
	viewWidth = boxWidth - 4
	// Title line, blank line, padding (2), and border (2) sit around the viewport.
	// Help under the popup takes 2 more rows.
	viewHeight = m.height - 10
	return boxWidth, viewHeight, viewWidth
}

func (m *ArticleModel) syncCommentsView(resetScroll bool) {
	m.resizeCommentsViewport()
	var content string
	switch {
	case m.commentsErr != "" && len(m.comments) == 0:
		content = m.commentsErr
	case len(m.comments) == 0 && m.commentsLoading:
		content = "Loading comments…"
	case len(m.comments) == 0:
		content = "No comments"
	default:
		content = renderComments(m.comments, m.commentsViewport.Width)
	}
	m.commentsViewport.SetContent(content)
	if resetScroll {
		m.commentsViewport.GotoTop()
	}
}

func (m *ArticleModel) commentsTitle() string {
	title := "Comments"
	if n := countComments(m.comments); n > 0 {
		title = fmt.Sprintf("Comments (%d)", n)
	}
	switch {
	case m.commentsLoading:
		title += "  loading…"
	case m.commentsNextURL != "" && m.commentsViewport.AtBottom():
		title += "  · m: next page"
	case m.commentsHint != "":
		title += "  " + m.commentsHint
	case m.commentsNextURL == "" && len(m.comments) > 0:
		title += "  · end"
	}
	return title
}

func (m *ArticleModel) commentsPopupView() string {
	boxWidth, _, _ := m.commentsBox()
	title := commentTitleStyle.Render(m.commentsTitle())
	body := title + "\n\n" + m.commentsViewport.View()
	box := paneStyle.
		BorderForeground(lipgloss.Color("170")).
		Width(boxWidth).
		Render(body)
	// Leave two rows under the popup for the help line.
	canvasHeight := m.height - 2
	if canvasHeight < 1 {
		canvasHeight = 1
	}
	placed := lipgloss.Place(m.width, canvasHeight, lipgloss.Center, lipgloss.Center, box)
	help := "\n" + helpStyle.Render("j/k: scroll | PgUp/PgDn: half page | G: bottom | m: next page | c/Esc: close")
	return placed + help
}

func countComments(comments []wykop.Comment) int {
	n := 0
	for _, comment := range comments {
		n += 1 + countComments(comment.Replies)
	}
	return n
}

func renderComments(comments []wykop.Comment, width int) string {
	if width < 20 {
		width = 20
	}
	var b strings.Builder
	for i, comment := range comments {
		if i > 0 {
			b.WriteByte('\n')
		}
		writeComment(&b, comment, 0, width)
	}
	return strings.TrimRight(b.String(), "\n")
}

func writeComment(b *strings.Builder, comment wykop.Comment, depth, width int) {
	indent := strings.Repeat("  ", depth)
	avail := width - len(indent)
	if avail < 10 {
		avail = 10
	}

	header := commentAuthorStyle.Render(comment.Author)
	meta := ""
	if comment.Score != "" {
		score := comment.Score
		if score[0] != '+' && score[0] != '-' {
			score = "+" + score
		}
		meta += "  " + score
	}
	if comment.When != "" {
		meta += "  " + comment.When
	}
	if meta != "" {
		header += commentMetaStyle.Render(meta)
	}
	b.WriteString(indent)
	b.WriteString(header)
	b.WriteByte('\n')

	if comment.Body != "" {
		wrapped := lipgloss.NewStyle().Width(avail).Render(comment.Body)
		for _, line := range strings.Split(wrapped, "\n") {
			b.WriteString(indent)
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	for _, reply := range comment.Replies {
		b.WriteByte('\n')
		writeComment(b, reply, depth+1, width)
	}
}

var (
	commentTitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170"))
	commentAuthorStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170"))
	commentMetaStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

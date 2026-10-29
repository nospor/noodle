package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"noodle/internal/database"
	"noodle/internal/wykop"
)

func wykopArticleModel() *ArticleModel {
	state := &AppState{
		CurrentFeedID: 1,
		Width:         100,
		Height:        32,
		Feeds: []database.Feed{{
			ID:    1,
			URL:   "https://wykop.pl/rss",
			Title: "Wykop",
		}},
		CurrentArticle: &database.Article{
			ID:    7,
			Title: "Entry",
			Link:  "https://wykop.pl/link/9/slug",
		},
	}
	return &ArticleModel{
		state:            state,
		width:            state.Width,
		height:           state.Height,
		commentsViewport: viewport.New(40, 10),
	}
}

func keyMsg(s string) tea.KeyMsg {
	if s == "pgdown" {
		return tea.KeyMsg{Type: tea.KeyPgDown}
	}
	if s == "esc" {
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestCommentsPopupLoadsAndForgets(t *testing.T) {
	orig := loadWykopPage
	t.Cleanup(func() { loadWykopPage = orig })

	var fetched []string
	loadWykopPage = func(pageURL string) (wykop.Page, error) {
		fetched = append(fetched, pageURL)
		if strings.Contains(pageURL, "/strona/2") {
			return wykop.Page{
				Comments: []wykop.Comment{{ID: "2", Author: "bob", Body: "next page"}},
			}, nil
		}
		return wykop.Page{
			Comments: []wykop.Comment{{ID: "1", Author: "alice", Body: strings.Repeat("line\n", 40)}},
			NextURL:  "https://wykop.pl/link/9/slug/strona/2",
		}, nil
	}

	m := wykopArticleModel()
	updated, cmd := m.Update(keyMsg("c"))
	model := updated.(*ArticleModel)
	if !model.commentsOpen || cmd == nil {
		t.Fatal("expected comments to open and start loading")
	}
	updated, _ = model.Update(cmd())
	model = updated.(*ArticleModel)
	if model.commentsLoading || len(model.comments) != 1 || model.comments[0].Author != "alice" {
		t.Fatalf("page 1 = %+v loading=%v", model.comments, model.commentsLoading)
	}
	view := model.View()
	if !strings.Contains(view, "alice") {
		t.Fatalf("popup view missing comment:\n%s", view)
	}
	if h := lipgloss.Height(view); h > model.height {
		t.Fatalf("popup height %d exceeds terminal %d", h, model.height)
	}

	// Not at the bottom yet, so m must not fetch.
	updated, cmd = model.Update(keyMsg("m"))
	model = updated.(*ArticleModel)
	if cmd != nil || model.commentsHint == "" {
		t.Fatalf("m above the bottom should wait, hint=%q cmd=%v", model.commentsHint, cmd != nil)
	}

	model.commentsViewport.GotoBottom()
	if !strings.Contains(model.View(), "m: next page") {
		t.Fatal("at the bottom the popup should offer the next page")
	}
	updated, cmd = model.Update(keyMsg("m"))
	model = updated.(*ArticleModel)
	if cmd == nil || !model.commentsLoading {
		t.Fatal("m at the bottom should load the next page")
	}
	updated, _ = model.Update(cmd())
	model = updated.(*ArticleModel)
	if len(model.comments) != 2 || model.comments[1].Author != "bob" {
		t.Fatalf("appended comments = %+v", model.comments)
	}
	if len(fetched) != 2 {
		t.Fatalf("fetched %v", fetched)
	}

	updated, _ = model.Update(keyMsg("esc"))
	model = updated.(*ArticleModel)
	if model.commentsOpen || model.comments != nil {
		t.Fatal("closing the popup should drop comments")
	}

	// A late response from the closed popup must not come back.
	updated, _ = model.Update(commentsPageMsg{
		gen:         1,
		articleLink: model.state.CurrentArticle.Link,
		page:        wykop.Page{Comments: []wykop.Comment{{Author: "ghost"}}},
	})
	model = updated.(*ArticleModel)
	if model.comments != nil {
		t.Fatalf("stale comments stored: %+v", model.comments)
	}
}

func TestCommentsKeyIgnoredForOtherFeeds(t *testing.T) {
	m := wykopArticleModel()
	m.state.Feeds[0].URL = "https://example.com/feed"
	updated, cmd := m.Update(keyMsg("c"))
	model := updated.(*ArticleModel)
	if model.commentsOpen || cmd != nil {
		t.Fatal("c should do nothing when the feed is not wykop.pl")
	}
}

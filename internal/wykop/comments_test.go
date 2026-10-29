package wykop

import (
	"os"
	"strings"
	"testing"

	"github.com/mmcdole/gofeed"
)

const samplePage = `<!DOCTYPE html><html><body>
<section class="stream link-comments" id="link-comments-stream">
  <div class="content thin-scrollbar">
    <section id="comment-1" class="entry stream-link">
      <a class="username"><span>alice</span></a>
      <time class="date" title="2026-10-28 10:41:59">4 godz. temu</time>
      <ul><li class="plus">85</li></ul>
      <section class="entry-content"><div class="wrapper">Hello<br>world</div></section>
      <div class="comments">
        <section class="stream entry-subcomments">
          <div class="content thin-scrollbar">
            <section id="comment-2" class="entry reply">
              <a class="username"><span>bob</span></a>
              <time class="date" title="2026-10-28 11:00:00">1 godz. temu</time>
              <ul><li class="plus">3</li></ul>
              <section class="entry-content"><div class="wrapper"><blockquote>Hello</blockquote><br>@<a href="/ludzie/alice">alice</a>: reply text<button class="more">Pokaż całość</button></div></section>
            </section>
          </div>
        </section>
      </div>
    </section>
    <section class="pub-slot-wrapper stream-1"></section>
    <section id="comment-3" class="entry low-rated stream-link">
      <a class="username"><span>
        cara</span></a>
      <time class="date">teraz</time>
      <section class="entry-content"><div class="wrapper">Second &amp; last</div></section>
    </section>
  </div>
  <footer>
    <nav class="new-pagination number"><ul>
      <li class="next"><a href="/link/1/slug/strona/2">&gt;</a></li>
    </ul></nav>
  </footer>
</section>
</body></html>`

func TestParseHTML(t *testing.T) {
	page, err := ParseHTML(strings.NewReader(samplePage), "https://wykop.pl/link/1/slug")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Comments) != 2 {
		t.Fatalf("got %d top-level comments", len(page.Comments))
	}
	first := page.Comments[0]
	if first.Author != "alice" || first.Score != "85" || first.When != "2026-10-28 10:41:59" {
		t.Fatalf("first comment header = %+v", first)
	}
	if first.Body != "Hello\nworld" {
		t.Fatalf("body = %q", first.Body)
	}
	if len(first.Replies) != 1 || first.Replies[0].Author != "bob" {
		t.Fatalf("replies = %+v", first.Replies)
	}
	if first.Replies[0].Body != "Hello\n\n@alice: reply text…" {
		t.Fatalf("reply body = %q", first.Replies[0].Body)
	}
	if page.Comments[1].Author != "cara" || page.Comments[1].Body != "Second & last" {
		t.Fatalf("second = %+v", page.Comments[1])
	}
	if page.NextURL != "https://wykop.pl/link/1/slug/strona/2" {
		t.Fatalf("next = %q", page.NextURL)
	}
}

func TestParseHTMLNoNextPage(t *testing.T) {
	html := strings.Replace(samplePage, `<li class="next"><a href="/link/1/slug/strona/2">&gt;</a></li>`, `<li class="next disabled"><span>&gt;</span></li>`, 1)
	page, err := ParseHTML(strings.NewReader(html), "https://wykop.pl/link/1/slug")
	if err != nil {
		t.Fatal(err)
	}
	if page.NextURL != "" {
		t.Fatalf("next = %q", page.NextURL)
	}
}

func TestLinkEntryURL(t *testing.T) {
	if _, ok := LinkEntryURL("https://example.com/link/1/slug"); ok {
		t.Fatal("accepted a non-wykop url")
	}
	got, ok := LinkEntryURL("https://wykop.pl/link/9/slug")
	if !ok || got != "https://wykop.pl/link/9/slug" {
		t.Fatalf("got %q %v", got, ok)
	}
	if !IsSite("https://wykop.pl/rss") {
		t.Fatal("rss should be a wykop url")
	}
	if IsSite("https://evilwykop.pl/rss") {
		t.Fatal("lookalike host should be rejected")
	}
}

func TestParseSavedLinkPage(t *testing.T) {
	path := os.Getenv("WYKOP_HTML")
	if path == "" {
		path = "/tmp/wykop-link.html"
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Skip(err)
	}
	page, err := ParseHTML(strings.NewReader(string(body)), "https://wykop.pl/link/8037371/slug")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Comments) < 5 {
		t.Fatalf("expected several comments, got %d", len(page.Comments))
	}
	if page.Comments[0].Author == "" || page.Comments[0].Body == "" {
		t.Fatalf("empty first comment: %+v", page.Comments[0])
	}
	replies := 0
	for _, comment := range page.Comments {
		replies += len(comment.Replies)
		assertPlainComment(t, comment)
	}
	if replies == 0 {
		t.Fatal("expected replies nested under a comment")
	}
	if !strings.Contains(page.NextURL, "/strona/") {
		t.Fatalf("next = %q", page.NextURL)
	}
}

func TestFetchLive(t *testing.T) {
	if os.Getenv("WYKOP_LIVE") == "" {
		t.Skip("set WYKOP_LIVE=1 to fetch a real comments page")
	}
	feed, err := gofeed.NewParser().ParseURL("https://wykop.pl/rss")
	if err != nil {
		t.Fatal(err)
	}
	var link string
	for _, item := range feed.Items {
		if entry, ok := LinkEntryURL(item.Link); ok {
			link = entry
			break
		}
	}
	if link == "" {
		t.Fatal("rss had no link entries")
	}
	page, err := FetchPage(link)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Comments) == 0 || page.Comments[0].Author == "" {
		t.Fatalf("comments=%d link=%s", len(page.Comments), link)
	}
	for _, comment := range page.Comments {
		assertPlainComment(t, comment)
	}
	if page.NextURL == "" {
		return
	}
	next, err := FetchPage(page.NextURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Comments) == 0 {
		t.Fatal("page 2 was empty")
	}
	seen := map[string]bool{}
	for _, comment := range page.Comments {
		if comment.ID != "" {
			seen[comment.ID] = true
		}
	}
	overlap := 0
	for _, comment := range next.Comments {
		if comment.ID != "" && seen[comment.ID] {
			overlap++
		}
	}
	if overlap == len(next.Comments) {
		t.Fatal("page 2 repeated page 1")
	}
}

func assertPlainComment(t *testing.T, comment Comment) {
	t.Helper()
	if strings.Contains(comment.Body, "<") || strings.Contains(comment.Body, "Pokaż całość") {
		t.Fatalf("comment %s still has markup: %q", comment.ID, comment.Body)
	}
	for _, reply := range comment.Replies {
		assertPlainComment(t, reply)
	}
}

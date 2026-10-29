package wykop

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// Comment is one Wykop comment, including replies that were already on the page.
// Nothing here is persisted.
type Comment struct {
	ID      string
	Author  string
	When    string
	Score   string
	Body    string
	Replies []Comment
}

// Page is a single screen of comments from a Wykop link.
type Page struct {
	Comments []Comment
	NextURL  string
}

var wykopClient = &http.Client{
	Timeout: 20 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		if !isWykopHost(req.URL.Hostname()) {
			return fmt.Errorf("redirected away from wykop.pl")
		}
		return nil
	},
}

var (
	brRe         = regexp.MustCompile(`(?i)<br\s*/?>`)
	newLineRe    = regexp.MustCompile(`(?i)<section\b[^>]*\bnew-line\b[^>]*>\s*</section>`)
	blockquoteRe = regexp.MustCompile(`(?i)</?blockquote\b[^>]*>`)
	tagRe        = regexp.MustCompile(`(?s)<[^>]*>`)
)

// IsSite reports whether rawURL points at wykop.pl.
func IsSite(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Hostname() == "" {
		return false
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return false
	}
	return isWykopHost(u.Hostname())
}

func isWykopHost(host string) bool {
	host = strings.ToLower(host)
	return host == "wykop.pl" || strings.HasSuffix(host, ".wykop.pl")
}

// LinkEntryURL returns the article URL when it is a Wykop link entry.
// Comment pages are served on that URL, then at /strona/N.
func LinkEntryURL(rawURL string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || !isWykopHost(u.Hostname()) {
		return "", false
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", false
	}
	if !strings.HasPrefix(u.Path, "/link/") {
		return "", false
	}
	return u.String(), true
}

// FetchPage downloads one comments page and parses it.
// The result is meant to be shown and discarded.
func FetchPage(pageURL string) (Page, error) {
	if !IsSite(pageURL) {
		return Page{}, fmt.Errorf("refusing to load comments from %s", pageURL)
	}

	req, err := http.NewRequest(http.MethodGet, pageURL, nil)
	if err != nil {
		return Page{}, fmt.Errorf("failed to load comments: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Noodle/1.0)")
	req.Header.Set("Accept", "text/html")

	resp, err := wykopClient.Do(req)
	if err != nil {
		return Page{}, fmt.Errorf("failed to load comments: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Page{}, fmt.Errorf("failed to load comments: %s", resp.Status)
	}

	return ParseHTML(io.LimitReader(resp.Body, 8<<20), pageURL)
}

// ParseHTML reads comments from a Wykop link page.
func ParseHTML(r io.Reader, pageURL string) (Page, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return Page{}, fmt.Errorf("failed to read comments page: %w", err)
	}

	stream := doc.Find("section.stream.link-comments").First()
	if stream.Length() == 0 {
		return Page{}, fmt.Errorf("comments not found")
	}

	var comments []Comment
	stream.Find("div.content").First().ChildrenFiltered("section.entry").Each(func(_ int, s *goquery.Selection) {
		comments = append(comments, parseComment(s))
	})

	page := Page{Comments: comments}
	if href, ok := stream.Find("nav.new-pagination li.next a").Attr("href"); ok && strings.TrimSpace(href) != "" {
		page.NextURL = resolveURL(pageURL, href)
	}
	return page, nil
}

func parseComment(s *goquery.Selection) Comment {
	c := Comment{
		Author: text(s.Find("a.username").First()),
		Score:  text(s.Find("li.plus").First()),
		Body:   commentBody(s),
	}
	if id, ok := s.Attr("id"); ok {
		c.ID = strings.TrimPrefix(id, "comment-")
	}
	when := s.Find("time.date").First()
	if title, ok := when.Attr("title"); ok && strings.TrimSpace(title) != "" {
		c.When = strings.TrimSpace(title)
	} else {
		c.When = text(when)
	}

	replies := s.Find("section.entry-subcomments").First().Find("div.content").First()
	replies.ChildrenFiltered("section.entry").Each(func(_ int, reply *goquery.Selection) {
		c.Replies = append(c.Replies, parseComment(reply))
	})
	return c
}

func commentBody(s *goquery.Selection) string {
	wrapper := s.Find("section.entry-content").First().Find("div.wrapper").First()
	if wrapper.Length() == 0 {
		return ""
	}

	truncated := wrapper.Find("button.more").Length() > 0
	wrapper.Find("button.more").Remove()

	var images []string
	wrapper.Find("img").Each(func(_ int, img *goquery.Selection) {
		if src, ok := img.Attr("src"); ok && strings.TrimSpace(src) != "" {
			images = append(images, strings.TrimSpace(src))
		}
	})

	raw, err := wrapper.Html()
	if err != nil {
		return text(wrapper)
	}
	raw = brRe.ReplaceAllString(raw, "\n")
	raw = newLineRe.ReplaceAllString(raw, "\n")
	raw = blockquoteRe.ReplaceAllString(raw, "\n")
	raw = tagRe.ReplaceAllString(raw, "")
	body := collapseLines(html.UnescapeString(raw))
	if len(images) > 0 {
		if body != "" {
			body += "\n"
		}
		body += strings.Join(images, "\n")
	}
	if truncated && body != "" {
		body += "…"
	}
	return body
}

func text(s *goquery.Selection) string {
	return strings.Join(strings.Fields(s.Text()), " ")
}

func collapseLines(s string) string {
	lines := strings.Split(s, "\n")
	var cleaned []string
	blank := 0
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			blank++
			if blank <= 1 && len(cleaned) > 0 {
				cleaned = append(cleaned, "")
			}
			continue
		}
		blank = 0
		cleaned = append(cleaned, line)
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

func resolveURL(baseRaw, href string) string {
	base, err := url.Parse(baseRaw)
	if err != nil {
		return ""
	}
	ref, err := url.Parse(href)
	if err != nil {
		return ""
	}
	resolved := base.ResolveReference(ref)
	if !isWykopHost(resolved.Hostname()) {
		return ""
	}
	if resolved.Scheme != "https" && resolved.Scheme != "http" {
		return ""
	}
	return resolved.String()
}

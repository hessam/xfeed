package feed

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"xfeed/server/internal/store"
)

type Item struct {
	Source string `json:"source"`
	Text   string `json:"text"`
	Time   string `json:"time"`
}

type Service struct {
	client      *http.Client
	channels    []string
	nitterFeeds []string
	st          *store.Store
}

// NewService creates the feed service backed by a SQLite store at dbPath.
func NewService(channelsFile, nitterFile, dbPath string) (*Service, error) {
	channels, err := readList(channelsFile)
	if err != nil {
		return nil, err
	}
	nitter, err := readList(nitterFile)
	if err != nil {
		return nil, err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	s := &Service{
		client:      &http.Client{Timeout: 10 * time.Second},
		channels:    channels,
		nitterFeeds: nitter,
		st:          st,
	}
	s.refreshCache()
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			s.refreshCache()
		}
	}()
	return s, nil
}

func (s *Service) refreshCache() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	all := append(s.channels, s.nitterFeeds...)
	sem := make(chan struct{}, 10) // max 10 concurrent fetches
	var wg sync.WaitGroup

	for _, src := range all {
		src := src
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			s.ingestSource(ctx, src)
		}()
	}
	wg.Wait()
}

func (s *Service) ingestSource(ctx context.Context, src string) {
	cursor := s.st.GetCursor(src)

	var posts []store.Post
	var newest string

	if strings.HasPrefix(src, "@") || (!strings.Contains(src, "/") && !strings.HasPrefix(src, "http")) {
		// Telegram scrape (legacy, currently channels.txt is empty)
		item, err := s.fetchTelegram(ctx, src)
		if err == nil && item.Text != "" && item.Time > cursor {
			posts = []store.Post{{Source: item.Source, Text: item.Text, PubDate: item.Time}}
			newest = item.Time
		}
	} else {
		// RSS/RSSHub
		items, err := s.fetchRSS(ctx, src)
		if err != nil {
			return
		}
		for _, it := range items {
			if cursor == "" || it.Time > cursor {
				posts = append(posts, store.Post{Source: it.Source, Text: it.Text, PubDate: it.Time})
				if it.Time > newest {
					newest = it.Time
				}
			}
		}
	}

	if len(posts) == 0 {
		return
	}
	n, err := s.st.InsertBatch(posts)
	if err != nil {
		return
	}
	if newest > cursor {
		_ = s.st.SetCursor(src, newest)
	}
	log.Printf("Ingested %d new posts from %s", n, src)
}

func (s *Service) Latest(_ context.Context, limit int) ([]Item, error) {
	if limit < 1 {
		limit = 30
	}
	posts, err := s.st.Latest(limit)
	if err != nil {
		return nil, err
	}
	if len(posts) == 0 {
		return nil, fmt.Errorf("no feed items available")
	}
	items := make([]Item, len(posts))
	for i, p := range posts {
		items[i] = Item{Source: p.Source, Text: p.Text, Time: p.PubDate}
	}
	return items, nil
}

// --- fetchers ---

func (s *Service) fetchTelegram(ctx context.Context, channel string) (Item, error) {
	url := fmt.Sprintf("https://t.me/s/%s", strings.TrimPrefix(channel, "@"))
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := s.client.Do(req)
	if err != nil {
		return Item{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Item{}, fmt.Errorf("telegram status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	text := firstMatch(string(body), `tgme_widget_message_text[^>]*>(?s:(.*?))</div>`)
	text = stripTags(text)
	if text == "" {
		return Item{}, fmt.Errorf("telegram message not found")
	}
	return Item{
		Source: "telegram:" + channel,
		Text:   text,
		Time:   time.Now().UTC().Format(time.RFC3339),
	}, nil
}

type rssFeed struct {
	Channel struct {
		Items []struct {
			Title       string `xml:"title"`
			Description string `xml:"description"`
			PubDate     string `xml:"pubDate"`
		} `xml:"item"`
	} `xml:"channel"`
}

func (s *Service) fetchRSS(ctx context.Context, url string) ([]Item, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rss status %d", resp.StatusCode)
	}
	var feed rssFeed
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&feed); err != nil {
		return nil, err
	}
	if len(feed.Channel.Items) == 0 {
		return nil, fmt.Errorf("rss empty")
	}
	out := make([]Item, 0, len(feed.Channel.Items))
	for _, it := range feed.Channel.Items {
		text := stripTags(it.Description)
		if text == "" {
			text = it.Title
		}
		out = append(out, Item{
			Source: urlToSourceLabel(url),
			Text:   text,
			Time:   parseRSSDate(it.PubDate),
		})
	}
	return out, nil
}

func parseRSSDate(date string) string {
	date = strings.TrimSpace(date)
	formats := []string{
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
		time.ANSIC,
		time.UnixDate,
		time.RubyDate,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z07:00",
	}
	for _, f := range formats {
		t, err := time.Parse(f, date)
		if err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return date
}

// --- helpers ---

func urlToSourceLabel(u string) string {
	if strings.Contains(u, "/twitter/user/") {
		parts := strings.Split(u, "/twitter/user/")
		if len(parts) > 1 {
			return "x:" + parts[1]
		}
	}
	if strings.Contains(u, "/telegram/channel/") {
		parts := strings.Split(u, "/telegram/channel/")
		if len(parts) > 1 {
			return "tg:" + parts[1]
		}
	}
	return "rss"
}

func readList(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(b), "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		out = append(out, l)
	}
	return out, nil
}

func firstMatch(s, pattern string) string {
	re := regexp.MustCompile(pattern)
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func stripTags(s string) string {
	re := regexp.MustCompile(`<[^>]+>`)
	return strings.TrimSpace(re.ReplaceAllString(s, " "))
}

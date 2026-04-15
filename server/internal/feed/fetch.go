package feed

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
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

	mu    sync.RWMutex
	cache []Item
}

func NewService(channelsFile, nitterFile string) (*Service, error) {
	channels, err := readList(channelsFile)
	if err != nil {
		return nil, err
	}
	nitter, err := readList(nitterFile)
	if err != nil {
		return nil, err
	}
	s := &Service{
		client:      &http.Client{Timeout: 6 * time.Second},
		channels:    channels,
		nitterFeeds: nitter,
	}
	// Populate cache immediately at startup.
	s.refreshCache()
	// Background refresh every 60 seconds.
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	items := s.fetchAll(ctx, 6)
	if len(items) > 0 {
		s.mu.Lock()
		s.cache = items
		s.mu.Unlock()
	}
}

func (s *Service) Latest(_ context.Context, limit int) ([]Item, error) {
	if limit < 1 {
		limit = 6
	}
	s.mu.RLock()
	cached := s.cache
	s.mu.RUnlock()
	if len(cached) == 0 {
		return nil, errors.New("no feed items available")
	}
	if len(cached) > limit {
		cached = cached[:limit]
	}
	return cached, nil
}

func (s *Service) fetchAll(ctx context.Context, limit int) []Item {
	out := make([]Item, 0, limit)
	for _, ch := range s.channels {
		if len(out) >= limit {
			break
		}
		item, err := s.fetchTelegram(ctx, ch)
		if err == nil && item.Text != "" {
			out = append(out, item)
		}
	}
	for _, rss := range s.nitterFeeds {
		if len(out) >= limit {
			break
		}
		items, err := s.fetchRSS(ctx, rss)
		if err == nil {
			for _, item := range items {
				if len(out) >= limit {
					break
				}
				if item.Text != "" {
					out = append(out, item)
				}
			}
		}
	}
	return out
}

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
		return Item{}, errors.New("telegram message not found")
	}
	return Item{
		Source: "telegram:" + channel,
		Text:   truncateSpace(text, 1200),
		Time:   time.Now().UTC().Format(time.RFC3339),
	}, nil
}

type rssFeed struct {
	Channel struct {
		Items []struct {
			Title   string `xml:"title"`
			PubDate string `xml:"pubDate"`
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
		return nil, errors.New("rss empty")
	}

	var out []Item
	for i, it := range feed.Channel.Items {
		if i >= 5 {
			break // Limit to 5 last posts per source
		}
		out = append(out, Item{
			Source: urlToSourceLabel(url),
			Text:   truncateSpace(it.Title, 1200),
			Time:   it.PubDate,
		})
	}
	return out, nil
}

func urlToSourceLabel(u string) string {
	if strings.Contains(u, "twitter/user/") {
		parts := strings.Split(u, "twitter/user/")
		if len(parts) > 1 {
			return "x:" + parts[1]
		}
	}
	if strings.Contains(u, "telegram/channel/") {
		parts := strings.Split(u, "telegram/channel/")
		if len(parts) > 1 {
			return "telegram:" + parts[1]
		}
	}
	return "xrss"
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

func truncateSpace(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	return s[:max]
}

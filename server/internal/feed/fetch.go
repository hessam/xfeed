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
	return &Service{
		client:      &http.Client{Timeout: 6 * time.Second},
		channels:    channels,
		nitterFeeds: nitter,
	}, nil
}

func (s *Service) Latest(ctx context.Context, limit int) ([]Item, error) {
	if limit < 1 {
		limit = 6
	}
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
		item, err := s.fetchRSS(ctx, rss)
		if err == nil && item.Text != "" {
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no feed items available")
	}
	return out, nil
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

func (s *Service) fetchRSS(ctx context.Context, url string) (Item, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := s.client.Do(req)
	if err != nil {
		return Item{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Item{}, fmt.Errorf("rss status %d", resp.StatusCode)
	}
	var feed rssFeed
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&feed); err != nil {
		return Item{}, err
	}
	if len(feed.Channel.Items) == 0 {
		return Item{}, errors.New("rss empty")
	}
	it := feed.Channel.Items[0]
	return Item{
		Source: "xrss",
		Text:   truncateSpace(it.Title, 1200),
		Time:   it.PubDate,
	}, nil
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

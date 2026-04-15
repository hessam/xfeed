package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
)

func main() {
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://t.me/s/durov", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	re := regexp.MustCompile(`tgme_widget_message_text[^>]*>(?s:(.*?))</div>`)
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		fmt.Println("NOT FOUND. Body preview:")
		fmt.Println(s[:500])
	} else {
		fmt.Println("FOUND:", m[1][:50])
	}
}

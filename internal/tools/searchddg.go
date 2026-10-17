package tools

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

const ddgBase = "https://html.duckduckgo.com/html/"

// DDG searches DuckDuckGo's HTML endpoint. It needs no API key.
// The host is fixed; redirects still go through Guard.
type DDG struct {
	Guard *Guard
	// Base overrides the endpoint. Empty uses the public HTML endpoint.
	Base string
}

func (d DDG) Search(ctx context.Context, query string, n int) ([]Hit, error) {
	base := d.Base
	if base == "" {
		base = ddgBase
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", query)
	u.RawQuery = q.Encode()
	g := d.Guard
	if g == nil {
		g = &Guard{}
	}
	res, err := g.Fetch(ctx, u.String())
	if err != nil {
		return nil, err
	}
	if res.Status < 200 || res.Status >= 300 {
		return nil, fmt.Errorf("ddg status %d", res.Status)
	}
	hits := parseDDG(string(res.Body))
	if len(hits) > n {
		hits = hits[:n]
	}
	return hits, nil
}

// parseDDG reads result anchors and the following snippet from DuckDuckGo HTML.
func parseDDG(src string) []Hit {
	var out []Hit
	lower := strings.ToLower(src)
	from := 0
	for {
		i := strings.Index(lower[from:], "result__a")
		if i < 0 {
			break
		}
		i += from
		tagStart := strings.LastIndex(src[:i], "<")
		if tagStart < 0 {
			from = i + len("result__a")
			continue
		}
		tagEnd := strings.IndexByte(src[tagStart:], '>')
		if tagEnd < 0 {
			break
		}
		tag := src[tagStart : tagStart+tagEnd]
		href := attr(tag, "href")
		innerStart := tagStart + tagEnd + 1
		close := strings.Index(lower[innerStart:], "</a>")
		if close < 0 {
			break
		}
		title := HTMLToText(src[innerStart : innerStart+close])
		restFrom := innerStart + close
		snippet := ""
		if j := strings.Index(lower[restFrom:], "result__snippet"); j >= 0 {
			s := restFrom + j
			tagAt := strings.LastIndex(src[:s], "<")
			if tagAt >= 0 {
				gt := strings.IndexByte(src[tagAt:], '>')
				if gt >= 0 {
					body := src[tagAt+gt+1:]
					end := strings.Index(strings.ToLower(body), "</")
					if end >= 0 {
						snippet = HTMLToText(body[:end])
					}
				}
			}
		}
		link := cleanDDGLink(href)
		if title != "" && link != "" {
			out = append(out, Hit{Title: title, URL: link, Snippet: snippet})
		}
		from = restFrom
		if from >= len(src) {
			break
		}
	}
	return out
}

func cleanDDGLink(href string) string {
	href = htmlUnescape(href)
	if href == "" {
		return ""
	}
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if uddg := u.Query().Get("uddg"); uddg != "" {
		if decoded, err := url.QueryUnescape(uddg); err == nil {
			href = decoded
		}
	}
	u, err = url.Parse(href)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return u.String()
}

func htmlUnescape(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	return s
}

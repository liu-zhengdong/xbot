// Package bm25 ranks short documents by keyword overlap. It is not a vector index.
package bm25

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

const (
	k1 = 1.2
	b  = 0.75
)

// Hit is a document index and its score.
type Hit struct {
	Index int
	Score float64
}

// Top returns up to limit documents with a positive score, highest first.
// Equal scores keep the earlier document.
func Top(query string, docs []string, limit int) []Hit {
	q := tokens(query)
	if len(q) == 0 || len(docs) == 0 || limit == 0 {
		return nil
	}
	if limit < 0 {
		limit = len(docs)
	}
	docTokens := make([][]string, len(docs))
	df := map[string]int{}
	var total int
	for i, doc := range docs {
		toks := tokens(doc)
		docTokens[i] = toks
		total += len(toks)
		seen := map[string]bool{}
		for _, t := range toks {
			if seen[t] {
				continue
			}
			seen[t] = true
			df[t]++
		}
	}
	avg := float64(total) / float64(len(docs))
	if avg == 0 {
		avg = 1
	}
	n := float64(len(docs))
	var hits []Hit
	for i, toks := range docTokens {
		if len(toks) == 0 {
			continue
		}
		tf := map[string]int{}
		for _, t := range toks {
			tf[t]++
		}
		var score float64
		for _, term := range q {
			f := float64(tf[term])
			if f == 0 {
				continue
			}
			idf := math.Log(1 + (n-float64(df[term])+0.5)/(float64(df[term])+0.5))
			denom := f + k1*(1-b+b*float64(len(toks))/avg)
			score += idf * (f * (k1 + 1) / denom)
		}
		if score > 0 {
			hits = append(hits, Hit{Index: i, Score: score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

func tokens(s string) []string {
	var out []string
	var run []rune
	han := false
	flush := func() {
		if han && len(run) > 1 {
			for i := 1; i < len(run); i++ {
				out = append(out, string(run[i-1:i+1]))
			}
		} else if len(run) > 0 {
			out = append(out, string(run))
		}
		run = run[:0]
	}
	for _, r := range strings.ToLower(s) {
		isHan := unicode.Is(unicode.Han, r)
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		if len(run) > 0 && han != isHan {
			flush()
		}
		han = isHan
		run = append(run, r)
	}
	flush()
	return out
}

package bm25

import "testing"

func TestTopFindsKeyword(t *testing.T) {
	docs := []string{
		"the cat sat on the mat",
		"a purple finch visited the feeder",
		"cats and dogs",
	}
	hits := Top("purple finch", docs, 3)
	if len(hits) == 0 || hits[0].Index != 1 {
		t.Fatalf("%+v", hits)
	}
	if len(Top("", docs, 3)) != 0 {
		t.Fatal("empty query should not hit")
	}
}

func TestHanKeywords(t *testing.T) {
	if hits := Top("时区", []string{"用户的时区是上海"}, 1); len(hits) != 1 {
		t.Fatal(hits)
	}
	if got := tokens("Go42时区海 ABC"); len(got) != 4 || got[0] != "go42" || got[1] != "时区" || got[2] != "区海" || got[3] != "abc" {
		t.Fatal(got)
	}
	if got := tokens("海"); len(got) != 1 || got[0] != "海" {
		t.Fatal(got)
	}
}

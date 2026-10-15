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

package acmelsp

import "testing"

func TestLevenshtein(t *testing.T) {
	if d := LevenshteinDistance("AAA", "AAB"); d != 1 {
		t.Error("unexpected distance: want 1 but have", d)
	}

	if d := LevenshteinDistance("AAA", "AA"); d != 1 {
		t.Error("unexpected distance: want 1 but have", d)
	}

	if d := LevenshteinDistance("AAA", "AAAB"); d != 1 {
		t.Error("unexpected distance: want 1 but have", d)
	}

	if d := LevenshteinDistance("ABA", "AAB"); d != 2{
		t.Error("unexpected distance: want 2 but have", d)
	}

	if d := LevenshteinDistance("f", "features"); d != 7 {
		t.Error("unexpected distance: want 7 but have", d)
	}

	if d := LevenshteinDistance("eat", "features"); d != 5 {
		t.Error("unexpected distance: want 5 but have", d)
	}

	if d := LevenshteinDistance("f", "kind"); d != 4 {
		t.Error("unexpected distance: want 4 but have", d)
	}
}

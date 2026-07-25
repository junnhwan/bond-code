package memory

import (
	"strings"
	"testing"
	"time"
)

func TestAgeAndFreshness(t *testing.T) {
	now := time.Now().UnixMilli()
	if AgeText(now) != "today" {
		t.Fatalf("today: %s", AgeText(now))
	}
	if FreshnessText(now) != "" {
		t.Fatal("fresh today should have no caveat")
	}
	old := time.Now().Add(-48 * time.Hour).UnixMilli()
	if !strings.Contains(AgeText(old), "days ago") && AgeText(old) != "yesterday" {
		// 48h can be 1 or 2 days depending on floor; either is fine as long as not today
		if AgeDays(old) < 1 {
			t.Fatalf("expected aged memory, got %s days=%d", AgeText(old), AgeDays(old))
		}
	}
	older := time.Now().Add(-10 * 24 * time.Hour).UnixMilli()
	if note := FreshnessText(older); !strings.Contains(note, "days old") {
		t.Fatalf("expected staleness note, got %q", note)
	}
}

package medication

import (
	"path/filepath"
	"testing"
	"time"
)

func TestBedtimeBelongsToPreviousScheduleDay(t *testing.T) {
	cfg, e := Load(filepath.Join("..", "..", "config", "config.yaml"))
	if e != nil {
		t.Fatal(e)
	}
	day := time.Date(2026, 9, 27, 0, 0, 0, 0, cfg.Location)
	bedtime, _ := cfg.Find("bedtime")
	want := time.Date(2026, 9, 28, 1, 30, 0, 0, cfg.Location)
	if got := cfg.Scheduled(day, bedtime); !got.Equal(want) {
		t.Fatalf("scheduled %s, want %s", got, want)
	}
	before := time.Date(2026, 9, 28, 1, 0, 0, 0, cfg.Location)
	if got := cfg.CurrentDay(before); !got.Equal(day) {
		t.Fatalf("day %s, want %s", got, day)
	}
	morning := time.Date(2026, 9, 28, 8, 0, 0, 0, cfg.Location)
	if got := cfg.CurrentDay(morning); !got.Equal(day.AddDate(0, 0, 1)) {
		t.Fatalf("day %s", got)
	}
}

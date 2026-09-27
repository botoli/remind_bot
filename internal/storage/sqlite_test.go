package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestOldDatabaseGetsTakenTimeColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE intake_log (id INTEGER PRIMARY KEY AUTOINCREMENT,reminder_id TEXT NOT NULL,scheduled_at TEXT NOT NULL,confirmed_at TEXT,status TEXT NOT NULL DEFAULT 'pending',initial_sent INTEGER NOT NULL DEFAULT 0,snoozed_until TEXT,snooze_count INTEGER NOT NULL DEFAULT 0,followup_sent INTEGER NOT NULL DEFAULT 0,UNIQUE(reminder_id,scheduled_at))`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	at := time.Date(2026, 9, 27, 20, 0, 0, 0, time.UTC)
	if err = s.Ensure("evening", at); err != nil {
		t.Fatal(err)
	}
	item, err := s.ByOccurrence("evening", at)
	if err != nil {
		t.Fatal(err)
	}
	if item.TakenAt != nil {
		t.Fatalf("unexpected taken time: %v", item.TakenAt)
	}
}

func TestIntakeIsIdempotentAndCanConfirmMissed(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	at := time.Date(2026, 9, 27, 20, 0, 0, 0, time.UTC)
	if e = s.Ensure("evening", at); e != nil {
		t.Fatal(e)
	}
	if e = s.Ensure("evening", at); e != nil {
		t.Fatal(e)
	}
	item, e := s.ByOccurrence("evening", at)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkSent(item.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.MarkFollowup(item.ID); e != nil {
		t.Fatal(e)
	}
	confirmed := at.Add(47 * time.Minute)
	changed, e := s.MarkTaken(item.ID, confirmed)
	if e != nil || !changed {
		t.Fatalf("first confirmation: %v %v", changed, e)
	}
	changed, e = s.MarkTaken(item.ID, confirmed.Add(time.Minute))
	if e != nil || changed {
		t.Fatalf("duplicate confirmation: %v %v", changed, e)
	}
	item, e = s.Get(item.ID)
	if e != nil {
		t.Fatal(e)
	}
	if item.Status != "taken" || item.ConfirmedAt == nil || !item.ConfirmedAt.Equal(confirmed) {
		t.Fatalf("saved intake: %+v", item)
	}
	actual := at.Add(3 * time.Minute)
	changed, e = s.CorrectTakenTime(item.ID, actual)
	if e != nil || !changed {
		t.Fatalf("correct time: %v %v", changed, e)
	}
	item, e = s.Get(item.ID)
	if e != nil {
		t.Fatal(e)
	}
	if item.TakenAt == nil || !item.TakenAt.Equal(actual) || !item.ConfirmedAt.Equal(confirmed) {
		t.Fatalf("correction changed confirmation: %+v", item)
	}
}

func TestSnoozeCannotBeQueuedTwice(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	at := time.Date(2026, 9, 27, 20, 0, 0, 0, time.UTC)
	if e := s.Ensure("evening", at); e != nil {
		t.Fatal(e)
	}
	item, e := s.ByOccurrence("evening", at)
	if e != nil {
		t.Fatal(e)
	}
	until := at.Add(15 * time.Minute)
	ok, e := s.Snooze(item.ID, until, 3)
	if e != nil || !ok {
		t.Fatalf("first snooze: %v %v", ok, e)
	}
	ok, e = s.Snooze(item.ID, until.Add(time.Minute), 3)
	if e != nil || ok {
		t.Fatalf("duplicate snooze: %v %v", ok, e)
	}
	item, e = s.Get(item.ID)
	if e != nil {
		t.Fatal(e)
	}
	if item.SnoozeCount != 1 || item.SnoozedUntil == nil || !item.SnoozedUntil.Equal(until) {
		t.Fatalf("saved snooze: %+v", item)
	}
}

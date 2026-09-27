package bot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"remind_bot/internal/medication"
	"remind_bot/internal/storage"
)

func TestManualTimeUsesScheduleDate(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	scheduled := time.Date(2026, 9, 28, 1, 30, 0, 0, loc)
	now := time.Date(2026, 9, 28, 8, 0, 0, 0, loc)
	got, err := parseTakenTime("00:45", scheduled, now, loc)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 9, 28, 0, 45, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
	evening := time.Date(2026, 9, 27, 20, 0, 0, 0, loc)
	got, err = parseTakenTime("00:30", evening, now, loc)
	if err != nil {
		t.Fatal(err)
	}
	want = time.Date(2026, 9, 28, 0, 30, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
	if _, err = parseTakenTime("25:00", evening, now, loc); err == nil {
		t.Fatal("accepted invalid hour")
	}
}

func TestButtonFlowDeletesInputAndEditsDashboard(t *testing.T) {
	ctx := context.Background()
	cfg, err := medication.Load(filepath.Join("..", "..", "config", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	calls := map[string]int{}
	nextID := int64(100)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := strings.TrimPrefix(r.URL.Path, "/")
		calls[method]++
		w.Header().Set("Content-Type", "application/json")
		if method == "sendMessage" {
			nextID++
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": nextID}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": true})
	}))
	defer server.Close()
	api := NewAPI("test")
	api.baseURL = server.URL
	b := &Bot{API: api, Store: db, Config: cfg, Allowed: 42}
	msg := &Message{MessageID: 1, From: User{42}, Chat: Chat{ID: 42, Type: "private"}, Text: "/start"}
	if err = b.handleUI(ctx, Update{Message: msg}); err != nil {
		t.Fatal(err)
	}
	dashboard := b.dashboardID
	msg = &Message{MessageID: 2, From: User{42}, Chat: Chat{ID: 42, Type: "private"}, Text: menuMark}
	if err = b.handleUI(ctx, Update{Message: msg}); err != nil {
		t.Fatal(err)
	}
	day := cfg.CurrentDay(time.Now())
	item, err := db.ByOccurrence("morning", cfg.Scheduled(day, cfg.Reminders[0]))
	if err != nil {
		t.Fatal(err)
	}
	callback := func(data string) Update {
		return Update{CallbackQuery: &Callback{ID: "q", From: User{42}, Message: Message{MessageID: dashboard, Chat: Chat{ID: 42, Type: "private"}}, Data: data}}
	}
	if err = b.handleUI(ctx, callback("slot:"+strconv.FormatInt(item.ID, 10))); err != nil {
		t.Fatal(err)
	}
	if err = b.handleUI(ctx, callback("15:"+strconv.FormatInt(item.ID, 10))); err != nil {
		t.Fatal(err)
	}
	item, err = db.Get(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != "taken" || item.TakenAt == nil {
		t.Fatalf("not recorded: %+v", item)
	}
	if calls["deleteMessage"] != 2 {
		t.Fatalf("deleted %d user messages", calls["deleteMessage"])
	}
	if calls["sendMessage"] != 2 {
		t.Fatalf("sent %d bot messages", calls["sendMessage"])
	}
	if calls["editMessageText"] < 3 {
		t.Fatalf("edited dashboard %d times", calls["editMessageText"])
	}
}

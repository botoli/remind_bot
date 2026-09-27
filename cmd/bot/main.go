package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	_ "time/tzdata"

	"remind_bot/internal/bot"
	"remind_bot/internal/medication"
	"remind_bot/internal/storage"
)

func main() {
	if e := run(); e != nil {
		log.Fatal(e)
	}
}
func run() error {
	if e := loadEnv(".env"); e != nil {
		return e
	}
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	allowed, e := strconv.ParseInt(os.Getenv("ALLOWED_USER_ID"), 10, 64)
	if e != nil || allowed <= 0 {
		return fmt.Errorf("ALLOWED_USER_ID must be a positive integer")
	}
	configPath := os.Getenv("MEDBOT_CONFIG")
	if configPath == "" {
		configPath = "config/config.yaml"
	}
	cfg, e := medication.Load(configPath)
	if e != nil {
		return fmt.Errorf("config: %w", e)
	}
	dbPath := os.Getenv("MEDBOT_DB")
	if dbPath == "" {
		dbPath = "data/medbot.db"
	}
	if e = os.MkdirAll(filepath.Dir(dbPath), 0700); e != nil {
		return e
	}
	store, e := storage.Open(dbPath)
	if e != nil {
		return e
	}
	defer store.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("medbot running with %d reminders in %s", len(cfg.Reminders), cfg.Timezone)
	return (&bot.Bot{API: bot.NewAPI(token), Store: store, Config: cfg, Allowed: allowed}).Run(ctx)
}
func loadEnv(path string) error {
	f, e := os.Open(path)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("invalid .env line")
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if _, set := os.LookupEnv(k); !set {
			if e = os.Setenv(k, v); e != nil {
				return e
			}
		}
	}
	return sc.Err()
}

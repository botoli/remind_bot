package bot

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"remind_bot/internal/medication"
	"remind_bot/internal/storage"
)

type Bot struct {
	API          *API
	Store        *storage.Store
	Config       *medication.Config
	Allowed      int64
	dashboardID  int64
	awaitingID   int64
	awaitingEdit bool
}

func (b *Bot) Run(ctx context.Context) error {
	updates := make(chan Update, 32)
	go b.API.Poll(ctx, updates)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	if e := b.tick(ctx, time.Now()); e != nil {
		log.Printf("scheduler: %v", e)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case u := <-updates:
			if e := b.handleUI(ctx, u); e != nil {
				log.Printf("update: %v", e)
			}
		case now := <-ticker.C:
			if e := b.tick(ctx, now); e != nil {
				log.Printf("scheduler: %v", e)
			}
		}
	}
}
func buttons(id int64, snooze bool) [][]button {
	row := []button{{"✅ Выпил сейчас", "t:" + strconv.FormatInt(id, 10)}}
	row = append(row, button{"🕒 Указать время", "slot:" + strconv.FormatInt(id, 10)})
	if snooze {
		return [][]button{row, {{"⏰ Через 15 мин", "s:" + strconv.FormatInt(id, 10)}}}
	}
	return [][]button{row}
}
func (b *Bot) tick(ctx context.Context, now time.Time) error {
	chat, e := b.Store.ChatID()
	if e != nil {
		return e
	}
	if chat == 0 {
		return nil
	}
	day := b.Config.CurrentDay(now)
	for _, d := range []time.Time{day.AddDate(0, 0, -1), day} {
		for _, r := range b.Config.Reminders {
			at := b.Config.Scheduled(d, r)
			if now.Before(at) || now.Sub(at) > 24*time.Hour {
				continue
			}
			if e = b.Store.Ensure(r.ID, at); e != nil {
				return e
			}
			item, e := b.Store.ByOccurrence(r.ID, at)
			if e != nil {
				return e
			}
			if item.Status == "pending" && !item.InitialSent && now.Sub(at) < 45*time.Minute {
				msg := fmt.Sprintf("💊 Время лекарств — %s\n\n%s", r.Time, medication.Details(r))
				if e = b.API.Send(ctx, chat, msg, buttons(item.ID, true)); e != nil {
					return e
				}
				if e = b.Store.MarkSent(item.ID); e != nil {
					return e
				}
				item.InitialSent = true
			}
			if item.Status != "pending" {
				continue
			}
			if item.SnoozedUntil != nil && !now.Before(*item.SnoozedUntil) {
				msg := "⏰ Ты просил напомнить ещё раз.\n\n" + medication.Details(r)
				if e = b.API.Send(ctx, chat, msg, buttons(item.ID, item.SnoozeCount < 3)); e != nil {
					return e
				}
				if e = b.Store.ClearSnooze(item.ID); e != nil {
					return e
				}
			}
			if item.InitialSent && item.SnoozeCount == 0 && !item.FollowupSent && now.Sub(at) >= 45*time.Minute {
				msg := fmt.Sprintf("⚠️ Приём за %s не отмечен.\n\nЕсли ты уже принял лекарства — отметь это.", r.Time)
				if e = b.API.Send(ctx, chat, msg, buttons(item.ID, false)); e != nil {
					return e
				}
				if e = b.Store.MarkFollowup(item.ID); e != nil {
					return e
				}
			}
		}
	}
	return nil
}
func (b *Bot) scheduleText() string {
	var s strings.Builder
	s.WriteString("💊 Расписание (" + b.Config.Timezone + ")\n\n")
	for _, r := range b.Config.Reminders {
		fmt.Fprintf(&s, "%s — %s\n%s\n\n", r.Time, medication.Names(r), medication.Details(r))
	}
	return strings.TrimSpace(s.String())
}
func (b *Bot) dayText(day time.Time) (string, error) {
	var s strings.Builder
	fmt.Fprintf(&s, "💊 %s\n\n", day.In(b.Config.Location).Format("02.01.2006"))
	count := 0
	for _, r := range b.Config.Reminders {
		at := b.Config.Scheduled(day, r)
		i, e := b.Store.ByOccurrence(r.ID, at)
		status := "⏳"
		if e == nil && i.Status == "taken" {
			if i.TakenAt != nil {
				status = "✅ " + i.TakenAt.In(b.Config.Location).Format("02.01 15:04")
			} else if i.ConfirmedAt != nil {
				status = "✅ время не указано (отмечено " + i.ConfirmedAt.In(b.Config.Location).Format("15:04") + ")"
			}
			count++
		} else if e == nil && i.Status == "missed" {
			status = "⚠️ не отмечено"
		} else if e != nil && e != sql.ErrNoRows {
			return "", e
		}
		fmt.Fprintf(&s, "%s  %s  %s\n", r.Time, medication.Names(r), status)
	}
	fmt.Fprintf(&s, "\nОтмечено: %d/%d", count, len(b.Config.Reminders))
	return s.String(), nil
}

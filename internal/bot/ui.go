package bot

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"remind_bot/internal/medication"
	"remind_bot/internal/storage"
)

const menuToday = "💊 Сегодня"
const menuMark = "✅ Уже выпил"
const menuHistory = "📖 История"

var takenTimePattern = regexp.MustCompile(`^(?:[0-3][0-9]\.[01][0-9] )?(?:[01][0-9]|2[0-3]):[0-5][0-9]$`)

func (b *Bot) handleUI(ctx context.Context, u Update) error {
	if m := u.Message; m != nil {
		return b.handleMessage(ctx, m)
	}
	if q := u.CallbackQuery; q != nil {
		return b.handleCallback(ctx, q)
	}
	return nil
}
func (b *Bot) handleMessage(ctx context.Context, m *Message) error {
	if m.From.ID != b.Allowed || m.Chat.Type != "private" {
		return nil
	}
	chat := m.Chat.ID
	if strings.HasPrefix(m.Text, "/start") {
		if err := b.Store.SetChatID(chat); err != nil {
			return err
		}
		b.awaitingID = 0
		if err := b.API.Delete(ctx, chat, m.MessageID); err != nil {
			log.Printf("delete command: %v", err)
		}
		if _, err := b.API.SendMenu(ctx, chat); err != nil {
			return err
		}
		return b.showHome(ctx, chat, 0)
	}
	registered, err := b.Store.ChatID()
	if err != nil {
		return err
	}
	if registered != chat {
		return nil
	}
	if err = b.API.Delete(ctx, chat, m.MessageID); err != nil {
		log.Printf("delete input: %v", err)
	}
	if b.awaitingID != 0 && !isMenuText(m.Text) {
		return b.handleTimeInput(ctx, chat, m.Text)
	}
	b.awaitingID = 0
	switch strings.Split(strings.TrimSpace(m.Text), "@")[0] {
	case menuToday, "/today":
		return b.showHome(ctx, chat, 0)
	case menuMark:
		return b.showMarkList(ctx, chat, 0, 0)
	case menuHistory, "/history":
		return b.showHistory(ctx, chat, 0)
	case "/schedule":
		return b.showSchedule(ctx, chat, 0)
	case "/help":
		return b.showHelp(ctx, chat, 0)
	default:
		return b.showHome(ctx, chat, 0)
	}
}
func isMenuText(s string) bool {
	return s == menuToday || s == menuMark || s == menuHistory || strings.HasPrefix(s, "/")
}
func (b *Bot) handleCallback(ctx context.Context, q *Callback) error {
	if q.From.ID != b.Allowed || q.Message.Chat.Type != "private" {
		return nil
	}
	chat, err := b.Store.ChatID()
	if err != nil {
		return err
	}
	if chat == 0 || chat != q.Message.Chat.ID {
		return nil
	}
	defer func() {
		if err := b.API.Answer(ctx, q.ID, ""); err != nil {
			log.Printf("answer callback: %v", err)
		}
	}()
	parts := strings.Split(q.Data, ":")
	if len(parts) == 0 {
		return nil
	}
	messageID := q.Message.MessageID
	switch parts[0] {
	case "home":
		b.awaitingID = 0
		return b.showHome(ctx, chat, messageID)
	case "mark":
		b.awaitingID = 0
		offset := 0
		if len(parts) == 2 && parts[1] == "1" {
			offset = 1
		}
		return b.showMarkList(ctx, chat, offset, messageID)
	case "history":
		b.awaitingID = 0
		return b.showHistory(ctx, chat, messageID)
	case "schedule":
		b.awaitingID = 0
		return b.showSchedule(ctx, chat, messageID)
	case "help":
		b.awaitingID = 0
		return b.showHelp(ctx, chat, messageID)
	}
	if len(parts) != 2 {
		return nil
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil
	}
	item, err := b.Store.Get(id)
	if err == sql.ErrNoRows {
		return b.render(ctx, chat, messageID, "Это напоминание больше не найдено.", homeButtons())
	}
	if err != nil {
		return err
	}
	reminder, ok := b.Config.Find(item.ReminderID)
	if !ok {
		return b.render(ctx, chat, messageID, "Этот приём больше не входит в расписание.", homeButtons())
	}
	switch parts[0] {
	case "slot":
		b.awaitingID = 0
		return b.showSlot(ctx, chat, messageID, item, reminder)
	case "edit":
		b.awaitingID = 0
		return b.showTimeChoices(ctx, chat, messageID, item, reminder, true)
	case "t":
		b.awaitingID = 0
		return b.record(ctx, chat, messageID, item, reminder, time.Now(), false)
	case "now":
		b.awaitingID = 0
		return b.record(ctx, chat, messageID, item, reminder, time.Now(), item.Status == "taken")
	case "15":
		b.awaitingID = 0
		return b.record(ctx, chat, messageID, item, reminder, time.Now().Add(-15*time.Minute), item.Status == "taken")
	case "30":
		b.awaitingID = 0
		return b.record(ctx, chat, messageID, item, reminder, time.Now().Add(-30*time.Minute), item.Status == "taken")
	case "manual":
		b.awaitingID = id
		b.awaitingEdit = item.Status == "taken"
		return b.render(ctx, chat, messageID, "✍️ "+medication.Names(reminder)+"\n\nНапиши время, когда выпил: 20:05\nЕсли это была другая дата: 27.09 20:05\n\nЧасовой пояс: "+b.Config.Timezone, [][]button{{{"◀️ Назад", "slot:" + strconv.FormatInt(id, 10)}}})
	case "s":
		b.awaitingID = 0
		ok, err := b.Store.Snooze(id, time.Now().Add(15*time.Minute), 3)
		if err != nil {
			return err
		}
		if !ok {
			return b.render(ctx, chat, messageID, "Повтор уже запланирован или приём отмечен.", homeButtons())
		}
		return b.render(ctx, chat, messageID, "⏰ Напомню через 15 минут.\n\n"+medication.Names(reminder), homeButtons())
	}
	return nil
}
func homeButtons() [][]button {
	return [][]button{{{"✅ Уже выпил", "mark:0"}, {"📅 Расписание", "schedule"}}, {{"📖 История", "history"}, {"❓ Помощь", "help"}}}
}
func (b *Bot) render(ctx context.Context, chat, id int64, text string, buttons [][]button) error {
	if id != 0 {
		if err := b.API.Edit(ctx, chat, id, text, buttons); err == nil {
			b.dashboardID = id
			return nil
		} else {
			log.Printf("edit message: %v", err)
		}
	}
	if b.dashboardID != 0 && b.dashboardID != id {
		if err := b.API.Edit(ctx, chat, b.dashboardID, text, buttons); err == nil {
			return nil
		}
	}
	msg, err := b.API.SendResult(ctx, chat, text, buttons)
	if err != nil {
		return err
	}
	b.dashboardID = msg.MessageID
	return nil
}
func (b *Bot) showHome(ctx context.Context, chat, id int64) error {
	day := b.Config.CurrentDay(time.Now())
	s, err := b.dayText(day)
	if err != nil {
		return err
	}
	return b.render(ctx, chat, id, s+"\n\nНажми «Уже выпил», чтобы отметить приём и указать время.", homeButtons())
}
func (b *Bot) showMarkList(ctx context.Context, chat int64, offset int, id int64) error {
	day := b.Config.CurrentDay(time.Now()).AddDate(0, 0, -offset)
	var text strings.Builder
	fmt.Fprintf(&text, "✅ Что ты уже выпил?\nДень расписания: %s\n\nВыбери приём:", day.In(b.Config.Location).Format("02.01.2006"))
	rows := make([][]button, 0, len(b.Config.Reminders)+2)
	for _, r := range b.Config.Reminders {
		at := b.Config.Scheduled(day, r)
		if err := b.Store.Ensure(r.ID, at); err != nil {
			return err
		}
		item, err := b.Store.ByOccurrence(r.ID, at)
		if err != nil {
			return err
		}
		label := r.Time + " " + medication.Names(r)
		if item.Status == "taken" {
			label = "✅ " + label
		}
		rows = append(rows, []button{{label, "slot:" + strconv.FormatInt(item.ID, 10)}})
	}
	if offset == 0 {
		rows = append(rows, []button{{"◀️ Вчера", "mark:1"}})
	} else {
		rows = append(rows, []button{{"Сегодня ▶️", "mark:0"}})
	}
	rows = append(rows, []button{{"🏠 Главное", "home"}})
	return b.render(ctx, chat, id, text.String(), rows)
}
func (b *Bot) showSlot(ctx context.Context, chat, id int64, item storage.Intake, r medication.Reminder) error {
	if item.Status == "taken" {
		text := "✅ " + medication.Names(r) + " уже отмечено.\n"
		if item.TakenAt != nil {
			text += "Время приёма: " + item.TakenAt.In(b.Config.Location).Format("02.01 15:04")
		} else {
			text += "Время приёма не указано."
		}
		text += "\n\nЕсли время неверное, исправь его. Вторую дозу бот не добавляет."
		return b.render(ctx, chat, id, text, [][]button{{{"✏️ Исправить время", "edit:" + strconv.FormatInt(item.ID, 10)}}, {{"◀️ К списку", "mark:0"}}})
	}
	return b.showTimeChoices(ctx, chat, id, item, r, false)
}
func (b *Bot) showTimeChoices(ctx context.Context, chat, id int64, item storage.Intake, r medication.Reminder, editing bool) error {
	title := "Когда ты выпил?"
	if editing {
		title = "На какое время исправить отметку?"
	}
	text := fmt.Sprintf("%s\n%s — %s\n\n%s", title, r.Time, medication.Names(r), medication.Details(r))
	key := strconv.FormatInt(item.ID, 10)
	return b.render(ctx, chat, id, text, [][]button{{{"✅ Сейчас", "now:" + key}}, {{"15 минут назад", "15:" + key}, {"30 минут назад", "30:" + key}}, {{"✍️ Ввести время", "manual:" + key}}, {{"◀️ Назад", "mark:0"}}})
}
func (b *Bot) record(ctx context.Context, chat, id int64, item storage.Intake, r medication.Reminder, taken time.Time, edit bool) error {
	var changed bool
	var err error
	if edit {
		changed, err = b.Store.CorrectTakenTime(item.ID, taken)
	} else {
		changed, err = b.Store.RecordTaken(item.ID, taken, time.Now())
	}
	if err != nil {
		return err
	}
	if !changed {
		return b.render(ctx, chat, id, "Этот приём уже отмечен. Выбери его в списке, чтобы увидеть время.", [][]button{{{"✅ К списку", "mark:0"}}})
	}
	b.awaitingID = 0
	label := "✅ Отмечено: "
	if edit {
		label = "✅ Время исправлено: "
	}
	text := label + medication.Names(r) + "\nВыпил: " + taken.In(b.Config.Location).Format("02.01 15:04")
	return b.render(ctx, chat, id, text, [][]button{{{"✅ К списку", "mark:0"}, {"🏠 Главное", "home"}}})
}
func (b *Bot) handleTimeInput(ctx context.Context, chat int64, input string) error {
	item, err := b.Store.Get(b.awaitingID)
	if err != nil {
		b.awaitingID = 0
		return err
	}
	r, ok := b.Config.Find(item.ReminderID)
	if !ok {
		b.awaitingID = 0
		return b.showHome(ctx, chat, 0)
	}
	taken, err := parseTakenTime(input, item.ScheduledAt, time.Now(), b.Config.Location)
	if err != nil {
		return b.render(ctx, chat, 0, "Не понял время. Напиши, например, 20:05 или 27.09 20:05.\n\n"+err.Error(), [][]button{{{"◀️ Назад", "slot:" + strconv.FormatInt(item.ID, 10)}}})
	}
	return b.record(ctx, chat, 0, item, r, taken, b.awaitingEdit)
}
func parseTakenTime(input string, scheduled, now time.Time, loc *time.Location) (time.Time, error) {
	input = strings.TrimSpace(input)
	if !takenTimePattern.MatchString(input) {
		return time.Time{}, fmt.Errorf("нужен формат 20:05 или 27.09 20:05")
	}
	var day, month, hour, minute int
	var hasDate bool
	if n, _ := fmt.Sscanf(input, "%d.%d %d:%d", &day, &month, &hour, &minute); n == 4 {
		hasDate = true
	} else if n, _ := fmt.Sscanf(input, "%d:%d", &hour, &minute); n != 2 {
		return time.Time{}, fmt.Errorf("нужен формат ЧЧ:ММ")
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return time.Time{}, fmt.Errorf("проверь часы и минуты")
	}
	base := scheduled.In(loc)
	y, mo, d := base.Date()
	if hasDate {
		candidate := time.Date(y, time.Month(month), day, hour, minute, 0, 0, loc)
		if candidate.Day() != day || int(candidate.Month()) != month {
			return time.Time{}, fmt.Errorf("проверь дату")
		}
		// A schedule day can cross New Year.
		options := []time.Time{candidate.AddDate(-1, 0, 0), candidate, candidate.AddDate(1, 0, 0)}
		best := options[0]
		for _, v := range options[1:] {
			if absDuration(v.Sub(scheduled)) < absDuration(best.Sub(scheduled)) {
				best = v
			}
		}
		if best.After(now) {
			return time.Time{}, fmt.Errorf("время ещё не наступило")
		}
		return best, nil
	}
	choices := []time.Time{
		time.Date(y, mo, d-1, hour, minute, 0, 0, loc),
		time.Date(y, mo, d, hour, minute, 0, 0, loc),
		time.Date(y, mo, d+1, hour, minute, 0, 0, loc),
	}
	var best time.Time
	for _, v := range choices {
		if v.After(now) {
			continue
		}
		if best.IsZero() || absDuration(v.Sub(scheduled)) < absDuration(best.Sub(scheduled)) {
			best = v
		}
	}
	if best.IsZero() {
		return time.Time{}, fmt.Errorf("время ещё не наступило")
	}
	if absDuration(best.Sub(scheduled)) > 12*time.Hour {
		return time.Time{}, fmt.Errorf("укажи также дату: ДД.ММ ЧЧ:ММ")
	}
	return best, nil
}
func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
func (b *Bot) showHistory(ctx context.Context, chat, id int64) error {
	day := b.Config.CurrentDay(time.Now())
	var text strings.Builder
	text.WriteString("📖 Последние 7 дней\n\n")
	for n := 0; n < 7; n++ {
		d := day.AddDate(0, 0, -n)
		fmt.Fprintf(&text, "%s\n", d.In(b.Config.Location).Format("02.01"))
		for _, r := range b.Config.Reminders {
			item, err := b.Store.ByOccurrence(r.ID, b.Config.Scheduled(d, r))
			if err != nil && err != sql.ErrNoRows {
				return err
			}
			status := "—"
			if err == nil && item.Status == "taken" {
				status = "✅ время не указано"
				if item.TakenAt != nil {
					status = "✅ " + item.TakenAt.In(b.Config.Location).Format("02.01 15:04")
				}
			}
			fmt.Fprintf(&text, "  %s %s %s\n", r.Time, medication.Names(r), status)
		}
		text.WriteString("\n")
	}
	return b.render(ctx, chat, id, text.String(), [][]button{{{"🏠 Главное", "home"}, {"✅ Уже выпил", "mark:0"}}})
}
func (b *Bot) showSchedule(ctx context.Context, chat, id int64) error {
	return b.render(ctx, chat, id, b.scheduleText(), [][]button{{{"🏠 Главное", "home"}}})
}
func (b *Bot) showHelp(ctx context.Context, chat, id int64) error {
	return b.render(ctx, chat, id, "Нажми «Уже выпил» → выбери приём → укажи время.\n\n✅ в списке значит, что ты уже отметил этот приём. При необходимости время можно исправить.\n\nБот хранит твою отметку, а не проверяет факт приёма. Если пропустил дозу, следуй назначению врача.", [][]button{{{"🏠 Главное", "home"}}})
}

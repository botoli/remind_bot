package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

type User struct {
	ID int64 `json:"id"`
}
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}
type Message struct {
	MessageID int64  `json:"message_id"`
	From      User   `json:"from"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
}
type Callback struct {
	ID      string  `json:"id"`
	From    User    `json:"from"`
	Message Message `json:"message"`
	Data    string  `json:"data"`
}
type Update struct {
	UpdateID      int64     `json:"update_id"`
	Message       *Message  `json:"message"`
	CallbackQuery *Callback `json:"callback_query"`
}
type button struct {
	Text string `json:"text"`
	Data string `json:"callback_data"`
}
type markup struct {
	InlineKeyboard [][]button `json:"inline_keyboard"`
}
type replyButton struct {
	Text string `json:"text"`
}
type replyMarkup struct {
	Keyboard   [][]replyButton `json:"keyboard"`
	Resize     bool            `json:"resize_keyboard"`
	Persistent bool            `json:"is_persistent"`
}

type API struct {
	token   string
	client  *http.Client
	baseURL string
}

func NewAPI(token string) *API {
	return &API{token: token, client: &http.Client{Timeout: 40 * time.Second}, baseURL: "https://api.telegram.org/bot" + token}
}
func (a *API) call(ctx context.Context, method string, args any, result any) error {
	body, e := json.Marshal(args)
	if e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/"+method, bytes.NewReader(body))
	if e != nil {
		return fmt.Errorf("Telegram request creation failed")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, e := a.client.Do(req)
	if e != nil {
		return fmt.Errorf("Telegram %s request failed: %T", method, e)
	}
	defer resp.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if e != nil {
		return fmt.Errorf("Telegram %s response read failed", method)
	}
	var envelope struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if e = json.Unmarshal(raw, &envelope); e != nil {
		return fmt.Errorf("Telegram %s returned invalid JSON", method)
	}
	if !envelope.OK {
		if method == "editMessageText" && strings.Contains(envelope.Description, "message is not modified") {
			return nil
		}
		return fmt.Errorf("Telegram %s: %s", method, envelope.Description)
	}
	if result != nil {
		return json.Unmarshal(envelope.Result, result)
	}
	return nil
}
func (a *API) Send(ctx context.Context, chat int64, message string, buttons [][]button) error {
	_, err := a.SendResult(ctx, chat, message, buttons)
	return err
}
func (a *API) SendResult(ctx context.Context, chat int64, message string, buttons [][]button) (Message, error) {
	args := map[string]any{"chat_id": chat, "text": message}
	if len(buttons) > 0 {
		args["reply_markup"] = markup{buttons}
	}
	var result Message
	err := a.call(ctx, "sendMessage", args, &result)
	return result, err
}
func (a *API) SendMenu(ctx context.Context, chat int64) (Message, error) {
	var result Message
	err := a.call(ctx, "sendMessage", map[string]any{"chat_id": chat, "text": "Выбирай кнопку внизу 👇", "reply_markup": replyMarkup{Keyboard: [][]replyButton{{{"💊 Сегодня"}, {"✅ Уже выпил"}, {"📖 История"}}}, Resize: true, Persistent: true}}, &result)
	return result, err
}
func (a *API) Edit(ctx context.Context, chat, id int64, message string, buttons [][]button) error {
	return a.call(ctx, "editMessageText", map[string]any{"chat_id": chat, "message_id": id, "text": message, "reply_markup": markup{buttons}}, nil)
}
func (a *API) Delete(ctx context.Context, chat, id int64) error {
	return a.call(ctx, "deleteMessage", map[string]any{"chat_id": chat, "message_id": id}, nil)
}
func (a *API) Answer(ctx context.Context, id, text string) error {
	return a.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": id, "text": text}, nil)
}
func (a *API) RemoveButtons(ctx context.Context, chat, id int64) error {
	return a.call(ctx, "editMessageReplyMarkup", map[string]any{"chat_id": chat, "message_id": id, "reply_markup": markup{[][]button{}}}, nil)
}
func (a *API) Poll(ctx context.Context, out chan<- Update) {
	var offset int64
	for ctx.Err() == nil {
		var updates []Update
		e := a.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 25, "allowed_updates": []string{"message", "callback_query"}}, &updates)
		if e != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("poll: %v", e)
			select {
			case <-time.After(3 * time.Second):
			case <-ctx.Done():
				return
			}
			continue
		}
		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			select {
			case out <- u:
			case <-ctx.Done():
				return
			}
		}
	}
}

// Moozy Bot API example: polling echo bot, standard library only.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
}
type update struct {
	ID      string `json:"update_id"`
	Message *struct {
		Text string `json:"text"`
		Chat struct {
			ID string `json:"id"`
		} `json:"chat"`
		From struct {
			ID string `json:"id"`
		} `json:"from"`
	} `json:"message"`
}
type client struct {
	base, token string
	http        *http.Client
}

func (c client) call(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API HTTP %d: %s", resp.StatusCode, string(data))
	}
	var env envelope
	if err = json.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	if !env.OK {
		return nil, errors.New("API returned ok=false")
	}
	return env.Result, nil
}
func (c client) send(ctx context.Context, chatID, text string) error {
	_, err := c.call(ctx, http.MethodPost, "/sendMessage", map[string]string{"chat_id": chatID, "text": text})
	return err
}
func main() {
	token := strings.TrimSpace(os.Getenv("MOOZY_BOT_TOKEN"))
	if token == "" {
		log.Fatal("Set MOOZY_BOT_TOKEN")
	}
	base := strings.TrimRight(os.Getenv("MOOZY_API_BASE_URL"), "/")
	if base == "" {
		base = "https://v1.moozy.org/bot"
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	c := client{base: base, token: token, http: &http.Client{Timeout: 20 * time.Second}}
	me, err := c.call(ctx, http.MethodGet, "/getMe", nil)
	if err != nil {
		log.Fatalf("getMe: %v", err)
	}
	log.Printf("Authenticated bot: %s", me)
	log.Println("Polling getUpdates. Do not run a webhook or another poller simultaneously.")
	for ctx.Err() == nil {
		result, err := c.call(ctx, http.MethodGet, "/getUpdates?limit=20", nil)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			log.Printf("getUpdates: %v", err)
			sleep(ctx, 3*time.Second)
			continue
		}
		var updates []update
		if err := json.Unmarshal(result, &updates); err != nil {
			log.Printf("decode updates: %v", err)
			sleep(ctx, 2*time.Second)
			continue
		}
		for _, u := range updates {
			if u.Message == nil {
				continue
			}
			chat := u.Message.Chat.ID
			if chat == "" {
				chat = u.Message.From.ID
			}
			if chat == "" {
				log.Printf("update %s missing chat ID", u.ID)
				continue
			}
			text := strings.TrimSpace(u.Message.Text)
			if text == "" {
				continue
			}
			var answer string
			switch strings.Fields(text)[0] {
			case "/start":
				answer = "Привет! Я пример бота Moozy на Go. Напиши сообщение — повторю его. /help — помощь."
			case "/help":
				answer = "Команды: /start, /help. Другой текст я повторяю."
			default:
				answer = "Echo: " + text
			}
			if err := c.send(ctx, chat, answer); err != nil {
				log.Printf("sendMessage update=%s chat=%s: %v", u.ID, chat, err)
			}
		}
		if len(updates) == 0 {
			sleep(ctx, 2*time.Second)
		}
	}
	log.Println("Bot stopped")
}
func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"jobradar/internal/ent"
)

type Telegram struct {
	client *http.Client
	token  string
}

func NewTelegram(token string) *Telegram {
	return &Telegram{
		client: http.DefaultClient,
		token:  token,
	}
}

const telegramMaxMessage = 4000

func (telegram *Telegram) Send(ctx context.Context, name string, chatIDs []string, jobs []*ent.Job) error {
	messages := formatMessages(name, jobs)

	for _, chatID := range chatIDs {
		if chatID == "" {
			continue
		}
		for _, message := range messages {
			if err := telegram.send(ctx, chatID, message); err != nil {
				return err
			}
		}
	}
	return nil
}

func formatMessages(name string, jobs []*ent.Job) []string {
	header := fmt.Sprintf("%s: %d new job", name, len(jobs))
	if len(jobs) > 1 {
		header += "s"
	}
	header += " found"

	var messages []string
	var message strings.Builder
	message.WriteString(header)

	for _, job := range jobs {
		lines := []string{fmt.Sprintf("%s — %s", job.Company, job.Title)}

		if location := strings.TrimSpace(job.Location); location != "" {
			lines = append(lines, location)
		}
		if fit, ok := job.Details["stack_fit"].(string); ok && fit != "" {
			lines = append(lines, "fit: "+strings.ReplaceAll(fit, "_", " "))
		}

		lines = append(lines, job.URL)
		block := strings.Join(lines, "\n")

		if message.Len() > 0 && message.Len()+len(block) > telegramMaxMessage {
			messages = append(messages, message.String())
			message.Reset()
		}
		if message.Len() > 0 {
			message.WriteString("\n\n")
		}
		message.WriteString(block)
	}
	if message.Len() > 0 {
		messages = append(messages, message.String())
	}

	return messages
}

func (telegram *Telegram) send(ctx context.Context, chatID, text string) error {
	body, err := json.Marshal(map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"disable_web_page_preview": true,
	})
	if err != nil {
		return fmt.Errorf("encode telegram message: %w", err)
	}

	request, _ := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://api.telegram.org/bot"+telegram.token+"/sendMessage",
		bytes.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/json")

	response, err := telegram.client.Do(request)
	if err != nil {
		// The transport error quotes the url, which carries the token.
		return fmt.Errorf("send telegram message: %s", strings.ReplaceAll(err.Error(), telegram.token, "***"))
	}
	defer func() {
		_ = response.Body.Close()
	}()

	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode telegram response: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("send telegram message: %s", result.Description)
	}
	return nil
}

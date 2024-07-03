package tgbot

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func createInputFile(filename string, data []byte, url string) models.InputFile {
	if len(data) > 0 {
		return &models.InputFileUpload{
			Filename: filename,
			Data:     bytes.NewReader(data),
		}
	}

	return &models.InputFileString{Data: url}
}

func getParseMode(textFormatting bool) models.ParseMode {
	if textFormatting {
		return models.ParseModeMarkdown
	}
	// var parseMode models.ParseMode
	// return parseMode
	return models.ParseModeMarkdown
}

func createInlineKeyboard(msg Message) any {
	switch {
	case len(msg.Buttons) > 0:
		var buttons [][]models.InlineKeyboardButton

		for _, button := range msg.Buttons {
			if len(button.Row) > 0 {
				var row []models.InlineKeyboardButton

				for _, btn := range button.Row {
					row = append(row, models.InlineKeyboardButton{
						Text:         btn.Text,
						URL:          btn.URL,
						CallbackData: btn.CallbackData,
					})
				}

				buttons = append(buttons, row)
			} else {
				buttons = append(buttons, []models.InlineKeyboardButton{
					{
						Text:         button.Text,
						URL:          button.URL,
						CallbackData: button.CallbackData,
					},
				})
			}
		}

		return models.InlineKeyboardMarkup{
			InlineKeyboard: buttons,
		}
	}

	return nil
}

func GetCommandArgArray(text string) []string {
	if len(text) > 0 && text[0] != '/' {
		return []string{text}
	}

	s := strings.Split(text, " ")
	if len(s) > 1 {
		return s[1:]
	}

	return s
}

func downloadFileBytes(ctx context.Context, b *bot.Bot, fileID string) ([]byte, error) {
	f, err := b.GetFile(ctx, &bot.GetFileParams{
		FileID: fileID,
	})
	if err != nil {
		return nil, err
	}

	fileURL := b.FileDownloadLink(f)
	resp, err := http.Get(fileURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

func GetCommandArgs(text string) string {
	if len(text) > 0 && text[0] != '/' {
		return text
	}

	re := regexp.MustCompile(`^/\S*`)
	s := re.ReplaceAllString(text, "")
	return strings.TrimSpace(s)
}

var httpClient = &http.Client{
	Timeout: time.Second * 20,
}

func downloadFile(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Read the response body into a byte slice
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return nil, fmt.Errorf("received status code %d from server: %s", resp.StatusCode, body)
	}

	return body, nil
}

package tgbot

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/exp/slog"
)

var (
	botToken = os.Getenv("TELEGRAM_BOT_TOKEN")

	chats = map[string]int64{
		"david": 739125269,
	}
)

func TestEditMessage(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))

	srv, err := NewService(logger, &Config{Token: botToken})
	require.NoError(t, err)

	chatID := chats["david"]

	t.Run("Edit text message", func(t *testing.T) {
		msg, err := srv.Send(chatID, Message{Text: "Initial text message"})
		require.NoError(t, err, "failed to send initial message")

		edited := "Edited text message"
		updatedMsg, err := srv.EditMessage(chatID, msg.ID, Message{Text: edited})
		require.NoError(t, err)
		require.Equal(t, edited, updatedMsg.Text)
	})

	t.Run("Edit caption", func(t *testing.T) {
		msg, err := srv.Send(chatID, Message{
			Text:     "Initial text message",
			ImageURL: "https://i.imgur.com/iCUa1i1.jpeg",
		})
		require.NoError(t, err, "failed to send initial message")

		edited := "Edited text message"
		editedMessage, err := srv.EditMessage(chatID, msg.ID, Message{
			Text: edited,
		})
		require.NoError(t, err)
		require.Equal(t, edited, editedMessage.Caption)
	})

	t.Run("Edit message to change image", func(t *testing.T) {
		msg, err := srv.Send(chatID, Message{
			Text:     "Initial text message",
			ImageURL: "https://i.imgur.com/iCUa1i1.jpeg",
		})
		require.NoError(t, err, "failed to send initial message")

		edited := "Edited text message"
		newImg := "https://i.imgur.com/maksyIC.jpeg"
		editedMessage, err := srv.EditMessage(chatID, msg.ID, Message{Text: edited, ImageURL: newImg})
		require.NoError(t, err)
		require.Equal(t, edited, editedMessage.Caption)
	})
}

func TestDownloadFile(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))

	srv, err := NewService(logger, &Config{Token: botToken})
	require.NoError(t, err)

	fileID := "rc-upload-1728075721688-2"

	data, err := srv.DownloadFile(fileID)
	require.NoError(t, err)

	if len(data) == 0 {
		t.Error("File is empty")
	}

	fmt.Println(data[:50])
}

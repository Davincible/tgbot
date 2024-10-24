package tgbot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"golang.org/x/exp/slog"
)

func (s *Service) DownloadFile(fileID any) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()

	file, err := s.bot.GetFile(ctx, &bot.GetFileParams{
		FileID: fmt.Sprintf("%v", fileID),
	})
	if err != nil {
		return nil, fmt.Errorf("get file: %w", err)
	}

	body, err := s.downloadFile(fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", s.cfg.Token, file.FilePath))
	if err != nil {
		return nil, fmt.Errorf("download file: %w", err)
	}

	return body, nil
}

func (s *Service) GetProfilePhoto(chatID int64) ([]byte, error) {
	var fileID string
	p, err := s.bot.GetUserProfilePhotos(context.Background(), &bot.GetUserProfilePhotosParams{
		UserID: chatID,
		Limit:  1,
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "user not found") {
			return nil, errors.New("user not found")
		}

		// return nil, fmt.Errorf("get user profile photos: %w", err)
		s.logger.Warn("Failed to get user profile photos", slog.String("err", err.Error()))
		chat, err := s.GetChat(chatID)
		if err != nil {
			return nil, fmt.Errorf("get chat: %w", err)
		}

		if chat.Photo == nil {
			return nil, errors.New("no photos found")
		}

		fileID = chat.Photo.BigFileID
	} else {
		if len(p.Photos) == 0 || len(p.Photos[0]) == 0 {
			return nil, errors.New("no photos found")
		}

		getBestPhoto := func(photos [][]models.PhotoSize) *models.PhotoSize {
			var best *models.PhotoSize
			for _, photo := range photos[0] {
				if best == nil || photo.Width > best.Width {
					best = &photo
				}
			}
			return best
		}

		best := getBestPhoto(p.Photos)
		fileID = best.FileID
	}

	if len(fileID) == 0 {
		return nil, errors.New("no picture found")
	}

	return s.DownloadFile(fileID)
}

func (s *Service) downloadURLs(msg Message) error {
	if len(msg.VideoURL) > 0 {
		video, err := s.downloadFile(msg.VideoURL)
		if err != nil {
			return fmt.Errorf("download video: %w", err)
		}

		msg.Video = video
		msg.VideoURL = ""
	}

	if len(msg.AudioURL) > 0 {
		audio, err := s.downloadFile(msg.AudioURL)
		if err != nil {
			return fmt.Errorf("download audio: %w", err)
		}

		msg.Audio = audio
		msg.AudioURL = ""
	}

	if len(msg.ImageURL) > 0 {
		image, err := s.downloadFile(msg.ImageURL)
		if err != nil {
			return fmt.Errorf("download image: %w", err)
		}

		msg.Image = image
		msg.ImageURL = ""
	}

	if len(msg.DocumentURL) > 0 {
		doc, err := s.downloadFile(msg.DocumentURL)
		if err != nil {
			return fmt.Errorf("download document: %w", err)
		}

		msg.Document = doc
		msg.DocumentURL = ""
	}

	return nil
}

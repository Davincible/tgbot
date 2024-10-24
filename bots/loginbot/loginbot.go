package loginbot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dongri/phonenumber"
	tBot "github.com/go-telegram/bot"
	"golang.org/x/exp/slog"

	"github.com/Davincible/tgbot"
)

var (
	reqType2Fa   = "2fa"
	reqTypeCode  = "code"
	reqTypePhone = "phone"
)

var (
	ErrInvalidPhone = errors.New("invalid phone number")
	ErrNoOpenReq    = errors.New("no open login requests")
	ErrTimeout      = errors.New("request timed out")
	ErrCanceled     = errors.New("request canceled")
)

const (
	defaultTimeout  = 24 * 5 * time.Hour
	cleanupInterval = time.Minute
)

type LoginCallback func(code string) error

type Config struct {
	Timeout time.Duration
}

type loginRequest struct {
	reqType  string
	response chan string
	cancel   context.CancelFunc
	created  time.Time
}

type Bot struct {
	logger *slog.Logger
	sender tgbot.Sender
	mutex  sync.RWMutex

	loginRequests map[int64]map[string]*loginRequest
	login2FAIdx   map[int64]int
	timeout       time.Duration
	done          chan struct{} // For graceful shutdown
}

// Create new login bot
func New(logger *slog.Logger, cfg Config) *Bot {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}

	b := &Bot{
		logger:        logger,
		loginRequests: make(map[int64]map[string]*loginRequest),
		login2FAIdx:   make(map[int64]int),
		timeout:       timeout,
		done:          make(chan struct{}),
	}

	go b.cleanupStaleRequests()

	return b
}

// Shutdown gracefully stops the bot and cleans up resources
func (b *Bot) Shutdown(ctx context.Context) error {
	close(b.done)

	b.mutex.Lock()
	defer b.mutex.Unlock()

	// Cancel all pending requests
	for _, requests := range b.loginRequests {
		for _, req := range requests {
			req.cancel()
			close(req.response)
		}
	}

	// Clear maps
	b.loginRequests = make(map[int64]map[string]*loginRequest)
	b.login2FAIdx = make(map[int64]int)

	return nil
}

// Implement Bot interface
func (b *Bot) SetSender(s tgbot.Sender) {
	b.sender = s
}

func (b *Bot) CallBacks() map[string]tgbot.CallBack {
	return map[string]tgbot.CallBack{}
}

func (b *Bot) Middleware() []tBot.Middleware {
	return []tBot.Middleware{
		b.LoginMiddlware(),
	}
}

func (b *Bot) cleanupStaleRequests() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			b.mutex.Lock()
			now := time.Now()

			for chatID, requests := range b.loginRequests {
				for reqType, req := range requests {
					if now.Sub(req.created) > b.timeout {
						req.cancel()
						close(req.response)
						delete(requests, reqType)
					}
				}

				if len(requests) == 0 {
					delete(b.loginRequests, chatID)
					delete(b.login2FAIdx, chatID)
				}
			}
			b.mutex.Unlock()

		case <-b.done:
			return
		}
	}
}

func (b *Bot) createRequest(chatID int64, reqType string) (chan string, context.Context, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	if _, ok := b.loginRequests[chatID]; !ok {
		b.loginRequests[chatID] = make(map[string]*loginRequest)
	}

	if existing, ok := b.loginRequests[chatID][reqType]; ok {
		existing.cancel()
		close(existing.response)
		delete(b.loginRequests[chatID], reqType)
	}

	ctx, cancel := context.WithTimeout(context.Background(), b.timeout)
	req := &loginRequest{
		reqType:  reqType,
		response: make(chan string, 1),
		cancel:   cancel,
		created:  time.Now(),
	}

	b.loginRequests[chatID][reqType] = req

	return req.response, ctx, nil
}

func (b *Bot) getRequest(chatID int64, reqType string) (*loginRequest, bool) {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	chatReqs, ok := b.loginRequests[chatID]
	if !ok {
		return nil, false
	}

	req, ok := chatReqs[reqType]
	return req, ok
}

func (b *Bot) hasAnyRequests(chatID int64) bool {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	m, ok := b.loginRequests[chatID]
	return ok && len(m) > 0
}

func (b *Bot) removeRequest(chatID int64, reqType string) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	if chatReqs, ok := b.loginRequests[chatID]; ok {
		if req, ok := chatReqs[reqType]; ok {
			req.cancel()
			delete(chatReqs, reqType)
		}
		if len(chatReqs) == 0 {
			delete(b.loginRequests, chatID)
			delete(b.login2FAIdx, chatID)
		}
	}
}

// Ask2FACode requests and waits for a 2FA code
func (b *Bot) Ask2FACode(chatID int64, i ...int) (string, error) {
	attemptLeft := 0
	if len(i) > 0 {
		attemptLeft = i[0]
	}

	if attemptLeft > 0 {
		if _, err := b.sender.Send(chatID, tgbot.Message{
			Text:           fmt.Sprintf(msg2FaIncorrect, attemptLeft),
			TextFormatting: true,
		}); err != nil {
			return "", fmt.Errorf("send 2fa incorrect message: %w", err)
		}
		time.Sleep(time.Second)
	}

	if _, err := b.sender.Send(chatID, tgbot.Message{
		Text: twofaCodeMsg,
	}); err != nil {
		return "", fmt.Errorf("failed to send 2fa request: %w", err)
	}

	respChan, ctx, err := b.createRequest(chatID, reqType2Fa)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	b.mutex.Lock()
	b.login2FAIdx[chatID] = attemptLeft + 1
	b.mutex.Unlock()

	select {
	case resp, ok := <-respChan:
		if !ok {
			return "", ErrCanceled
		}
		return resp, nil
	case <-ctx.Done():
		b.removeRequest(chatID, reqType2Fa)
		return "", ErrTimeout
	}
}

// SendCodeRequest requests and waits for a login code
func (b *Bot) SendCodeRequest(chatID int64) (string, error) {
	if _, err := b.sender.Send(chatID, tgbot.Message{
		Text: loginCodeMsg,
	}); err != nil {
		return "", fmt.Errorf("failed to send login code request: %w", err)
	}

	respChan, ctx, err := b.createRequest(chatID, reqTypeCode)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	select {
	case resp, ok := <-respChan:
		if !ok {
			return "", ErrCanceled
		}
		return resp, nil
	case <-ctx.Done():
		b.removeRequest(chatID, reqTypeCode)
		return "", ErrTimeout
	}
}

// AskPhone requests and waits for a phone number
func (b *Bot) AskPhone(chatID int64) (string, error) {
	if _, err := b.sender.Send(chatID, tgbot.Message{
		Text: phoneMsg,
	}); err != nil {
		return "", fmt.Errorf("failed to send phone request: %w", err)
	}

	respChan, ctx, err := b.createRequest(chatID, reqTypePhone)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	select {
	case resp, ok := <-respChan:
		if !ok {
			return "", ErrCanceled
		}
		return resp, nil
	case <-ctx.Done():
		b.removeRequest(chatID, reqTypePhone)
		return "", ErrTimeout
	}
}

// Callback handlers
func (b *Bot) handle2FACallback(chatID int64, text string) {
	req, ok := b.getRequest(chatID, reqType2Fa)
	if !ok {
		b.logger.Error("no open login request",
			slog.Int64("id", chatID),
			slog.String("text", text),
		)
		return
	}

	code := strings.TrimSpace(text)
	if len(code) == 0 {
		if _, err := b.sender.Send(chatID, tgbot.Message{Text: "Invalid 2FA code"}); err != nil {
			b.logger.Error("failed to send login code error", "error", err)
		}
		return
	}

	select {
	case req.response <- code:
		b.removeRequest(chatID, reqType2Fa)
	default:
		b.logger.Error("failed to send response - channel full or closed",
			slog.Int64("id", chatID),
		)
	}
}

func (b *Bot) handleCodeCallback(chatID int64, text string) {
	req, ok := b.getRequest(chatID, reqTypeCode)
	if !ok {
		b.logger.Error("no open login request",
			slog.Int64("id", chatID),
			slog.String("text", text),
		)
		return
	}

	code := extractCode(text)
	if len(code) == 0 {
		if _, err := b.sender.Send(chatID, tgbot.Message{
			Text: "Text message does not contain a code, please try again.",
		}); err != nil {
			b.logger.Error("failed to send login code error", "error", err)
		}
		return
	}

	select {
	case req.response <- code:
		b.removeRequest(chatID, reqTypeCode)
	default:
		b.logger.Error("failed to send response - channel full or closed",
			slog.Int64("id", chatID),
		)
	}
}

func (b *Bot) handlePhoneCallback(chatID int64, text string) {
	req, ok := b.getRequest(chatID, reqTypePhone)
	if !ok {
		b.logger.Error("no open login request",
			slog.Int64("id", chatID),
			slog.String("text", text),
		)
		return
	}

	phone := strings.TrimSpace(text)
	country := phonenumber.GetISO3166ByNumber(phone, false).CountryCode
	phone = phonenumber.Parse(phone, country)

	if len(phone) == 0 {
		if _, err := b.sender.Send(chatID, tgbot.Message{Text: "Invalid phone number"}); err != nil {
			b.logger.Error("failed to send phone error", "error", err)
		}
		return
	}

	if !strings.HasPrefix(phone, "+") {
		phone = "+" + phone
	}

	select {
	case req.response <- phone:
		b.removeRequest(chatID, reqTypePhone)
	default:
		b.logger.Error("failed to send response - channel full or closed",
			slog.Int64("id", chatID),
		)
	}
}

// HasOpenReq checks if there are any open requests for the given chat ID
func (b *Bot) HasOpenReq(chatID int64, param ...string) bool {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	user, ok := b.loginRequests[chatID]
	if !ok {
		return false
	}

	if len(param) == 0 {
		return len(user) > 0
	}

	_, ok = user[param[0]]
	return ok
}

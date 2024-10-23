package mtproto

import (
	"fmt"
	"time"

	"github.com/celestix/gotgproto/generic"
	"github.com/gotd/td/tg"
)

// Message represents a Telegram message with its metadata
type Message struct {
	ID        int64
	Text      string
	FromID    int64
	PeerID    int64
	Timestamp time.Time
	Entities  []MessageEntity
}

// MessageEntity represents a message entity (URL, mention, etc.)
type MessageEntity struct {
	Type     string
	Offset   int
	Length   int
	URL      string
	UserID   int64
	Language string
}

// SendMessageOptions contains options for sending messages
type SendMessageOptions struct {
	DisablePreview      bool
	DisableNotification bool
	ScheduleDate        int
	ClearDraft          bool
	Silent              bool
	Background          bool
	ReplyToMessageID    int
}

// SendMessage sends a message to the specified peer
func (c *Client) SendMessage(peerID int64, text string, opts *SendMessageOptions) (*tg.Message, error) {
	c.mu.RLock()
	if !c.started {
		c.mu.RUnlock()
		return nil, ErrClientNotStarted
	}
	c.mu.RUnlock()

	if opts == nil {
		opts = &SendMessageOptions{}
	}

	var replyTo tg.InputReplyToClass
	if opts.ReplyToMessageID > 0 {
		replyTo = &tg.InputReplyToMessage{ReplyToMsgID: opts.ReplyToMessageID}
	}

	req := &tg.MessagesSendMessageRequest{
		Peer:         &tg.InputPeerUser{UserID: peerID},
		Message:      text,
		NoWebpage:    opts.DisablePreview,
		Silent:       opts.Silent,
		Background:   opts.Background,
		ClearDraft:   opts.ClearDraft,
		ScheduleDate: opts.ScheduleDate,
		ReplyTo:      replyTo,
	}

	randomID, err := c.client.RandInt64()
	if err != nil {
		return nil, fmt.Errorf("generate random_id: %w", err)
	}
	req.RandomID = randomID

	sent, err := generic.SendMessage(c.client.CreateContext(), peerID, req)
	if err != nil {
		return nil, fmt.Errorf("send message: %w", err)
	}

	return sent.Message, nil
}

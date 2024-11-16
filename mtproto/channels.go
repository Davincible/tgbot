package mtproto

import (
	"context"
	"fmt"
	"time"

	"github.com/gotd/td/tg"
	"golang.org/x/exp/slog"
)

// inputChannel, err := s.getChannelInput(name)
// if err != nil {
// 	return nil, fmt.Errorf("get channel input: %w", err)
// }
//
// res, err := s.client.API().ChannelsGetFullChannel(context.Background(), inputChannel)
// if err != nil {
// 	return nil, fmt.Errorf("get full channel: %w", err)
// }
//
// info, ok := res.FullChat.(*tg.ChannelFull)
// if !ok {
// 	return nil, fmt.Errorf("unexpected channel type: %T", res.FullChat)
// }
//
// channel := Channel{
// 	Info: info,
// }

// GetChannelMembers retrieves members of a Telegram channel
func (c *Client) GetChannelMembers(ctx context.Context, channelUsername string, opts *ChannelMembersOptions) ([]*tg.User, error) {
	if opts == nil {
		opts = &ChannelMembersOptions{
			RetryCount: 3,
			RetryDelay: time.Second * 2,
		}
	}

	channel, err := c.getChannelInputByUsername(channelUsername)
	if err != nil {
		return nil, err
	}

	var users []*tg.User
	offset := opts.Offset
	attempt := 0

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		participants, err := c.client.API().ChannelsGetParticipants(ctx, &tg.ChannelsGetParticipantsRequest{
			Channel: channel,
			Filter:  &tg.ChannelParticipantsRecent{},
			Offset:  offset,
			Limit:   100,
		})

		if err != nil {
			if attempt < opts.RetryCount {
				attempt++
				time.Sleep(opts.RetryDelay)
				continue
			}

			return nil, fmt.Errorf("get participants: %w", err)
		}

		details, ok := participants.AsModified()
		if !ok {
			return nil, fmt.Errorf("invalid participants response")
		}

		rawUsers := details.GetUsers()
		if len(rawUsers) == 0 {
			break
		}

		for _, item := range rawUsers {
			if user, ok := item.AsNotEmpty(); ok {
				if opts.ActiveOnly && user.Deleted {
					continue
				}
				users = append(users, user)
			}
		}

		if (opts.MaxPages > 0 && len(users)/100 >= opts.MaxPages) ||
			(opts.MaxUsers > 0 && len(users) >= opts.MaxUsers) ||
			len(users) >= details.Count {
			break
		}

		offset += len(rawUsers)
		time.Sleep(time.Millisecond * 200) // Respect rate limits
	}

	return users, nil
}

type ChannelMessagesOptions struct {
	MinMessages int       // Minimum number of messages to fetch
	MinDate     time.Time // Only fetch messages after this date
	BatchSize   int       // Number of messages per batch (max 100)
	Sleep       time.Duration
	Hook        func(msg *tg.Message) bool
}

// Default options when none are provided
var defaultChannelMessagesOptions = ChannelMessagesOptions{
	MinMessages: 99,
	BatchSize:   100,
	Sleep:       time.Millisecond * 500,
}

// GetChannelMessages fetches messages from a channel according to provided options
func (c *Client) GetChannelMessages(chatID int64, opts *ChannelMessagesOptions) ([]*tg.Message, error) {
	// Use default options if none provided
	if opts == nil {
		opts = &defaultChannelMessagesOptions
	}

	// Validate and set defaults for individual fields
	if opts.BatchSize <= 0 || opts.BatchSize > 100 {
		opts.BatchSize = 100
	}

	if opts.MinMessages <= 0 {
		opts.MinMessages = defaultChannelMessagesOptions.MinMessages
	}

	var (
		allMessages []*tg.Message
		offsetID    int
		done        bool
		lastMsgDate time.Time
	)

	for !done {
		messages, total, err := c.getChannelMessagesBatch(chatID, offsetID, opts.BatchSize)
		if err != nil {
			return nil, fmt.Errorf("get messages batch: %w", err)
		}
		var filtered []*tg.Message

		for _, msg := range messages {
			lastMsgDate = time.Unix(int64(msg.Date), 0)

			if !opts.MinDate.IsZero() && lastMsgDate.Before(opts.MinDate) {
				done = true
				break
			}

			filtered = append(filtered, msg)
		}

		if opts.Hook != nil {
			for _, msg := range filtered {
				if opts.Hook(msg) {
					done = true
					break
				}
			}
		}

		allMessages = append(allMessages, filtered...)

		// Update logging
		c.logger.Debug("Fetched message batch",
			slog.Int("batchSize", len(messages)),
			slog.Int("totalCollected", len(allMessages)),
			slog.Int("targetMin", opts.MinMessages),
			slog.Int("totalAvailable", total),
			slog.Time("minDate", opts.MinDate),
		)

		// Determine if we should continue
		if done ||
			len(messages) == 0 || // No more messages available
			len(allMessages) >= total || // Got all available messages
			(len(allMessages) >= opts.MinMessages && opts.MinDate.IsZero()) { // Got minimum required messages
			done = true
			break
		}

		// Update offset for next batch
		if len(messages) > 0 {
			offsetID = messages[len(messages)-1].ID
		}

		time.Sleep(opts.Sleep) // Respect rate limits
	}

	return allMessages, nil
}

// getChannelMessagesBatch fetches a single batch of messages from a channel
func (c *Client) getChannelMessagesBatch(chatID int64, offsetID, limit int) ([]*tg.Message, int, error) {
	inputChannel, err := c.getChannelInputByChatID(chatID)
	if err != nil {
		return nil, 0, fmt.Errorf("get channel input: %w", err)
	}

	resp, err := c.client.API().MessagesGetHistory(context.Background(), &tg.MessagesGetHistoryRequest{
		Peer: &tg.InputPeerChannel{
			ChannelID:  chatID,
			AccessHash: inputChannel.AccessHash,
		},
		OffsetID: offsetID,
		Limit:    limit,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("get channel messages: %w", err)
	}

	msgs, ok := resp.(*tg.MessagesChannelMessages)
	if !ok {
		return nil, 0, fmt.Errorf("unexpected response type: %T", resp)
	}

	var messages []*tg.Message
	for _, item := range msgs.Messages {
		if msg, ok := item.(*tg.Message); ok {
			messages = append(messages, msg)
		}
	}

	return messages, msgs.Count, nil
}

func (c *Client) resolveChannelByName(name string) (*tg.ChannelFull, error) {
	channel, err := c.getChannelInputByUsername(name)
	if err != nil {
		return nil, fmt.Errorf("resolve channel: %w", err)
	}

	res, err := c.client.API().ChannelsGetFullChannel(context.Background(), channel)
	if err != nil {
		return nil, fmt.Errorf("get full channel: %w", err)
	}

	info, ok := res.FullChat.(*tg.ChannelFull)
	if !ok {
		return nil, fmt.Errorf("unexpected channel type: %T", res.FullChat)
	}

	return info, nil
}

func (c *Client) getChannelInputByUsername(name string) (*tg.InputChannel, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()

	peer, err := c.client.API().ContactsResolveUsername(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("resolve username: %w", err)
	}

	if len(peer.Chats) == 0 {
		return nil, ErrChatNotFound
	}

	channel, ok := peer.Chats[0].(*tg.Channel)
	if !ok {
		return nil, fmt.Errorf("unexpected peer type: %T", peer.Chats[0])
	}

	return &tg.InputChannel{
		ChannelID:  channel.ID,
		AccessHash: channel.AccessHash,
	}, nil
}

func (c *Client) getChannelInputByChatID(chatID int64) (*tg.InputChannel, error) {
	ctx := context.Background()

	result, err := c.client.API().ChannelsGetChannels(ctx, []tg.InputChannelClass{
		&tg.InputChannel{
			ChannelID: chatID,
			// We use 0 as a temporary access hash - the API will still return channel info
			// if we have access to it
			AccessHash: 0,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get channel: %w", err)
	}

	// Type assert and get the channel
	chats, ok := result.(*tg.MessagesChats)
	if !ok {
		return nil, fmt.Errorf("unexpected response type")
	}

	// Find our channel
	for _, chat := range chats.Chats {
		if channel, ok := chat.(*tg.Channel); ok {
			if channel.ID == chatID {
				return &tg.InputChannel{
					ChannelID:  chatID,
					AccessHash: channel.AccessHash,
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("channel not found")
}

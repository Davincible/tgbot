package mtproto

import (
	"context"
	"fmt"
	"time"

	"github.com/gotd/td/tg"
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

	channel, err := c.getChannelInput(channelUsername)
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

func (c *Client) resolveChannelByName(name string) (*tg.ChannelFull, error) {
	channel, err := c.getChannelInput(name)
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

func (c *Client) getChannelInput(name string) (*tg.InputChannel, error) {
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

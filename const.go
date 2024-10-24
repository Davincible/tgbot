package tgbot

import "errors"

var (
	ErrNilLogger = errors.New("logger not provided")
	ErrNilConfig = errors.New("config not provided")
)

var (
	// allowedUpdates is hardcoded to make sure we receive the reaction updates,
	// which need to be explicitly requested.
	allowedUpdates = []string{
		"message",
		"edited_message",
		"channel_post",
		"edited_channel_post",
		"business_connection",
		"business_message",
		"edited_business_message",
		"deleted_business_messages",
		"message_reaction",
		"message_reaction_count",
		"inline_query",
		"chosen_inline_result",
		"callback_query",
		"shipping_query",
		"pre_checkout_query",
		"poll",
		"poll_answer",
		"my_chat_member",
		"chat_member",
		"chat_join_request",
		"chat_boost",
		"removed_chat_boost",
	}
)

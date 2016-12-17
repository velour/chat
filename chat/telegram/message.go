package telegram

// These messages match those described in the Telegram API documents.
// However, snake_case has been converted to CamlCase to match Go style.
// Optional fields are pointers.

// An Update is the core message type sent from the telegram server to notify the client of any updates.
type Update struct {
	// UpdateID is a unique identifier of the Update.
	// IDs start from a positive number and increse sequentially.
	UpdateID uint64 `json:"update_id"`

	// At most one of the following will be non-nil in a given Update.

	// It is the new incoming message of any kind.
	Message *Message `json:"message"`

	// EditedMessage is the new version of a message that is known to the bot and was edited.
	EditedMessage *Message `json:"edited_message"`
}

// A Message represents a message sent with telegram.
type Message struct {
	// MessageID is a unique identifier for the message within a chat.
	MessageID uint64 `json:"message_id"`

	// From is the sender of the message. It is nil for messages sent to channels.
	From *User `json:"from"`

	// Date is the Unix time that the message was sent.
	Date uint64 `json:"date"`

	// Chat is the chat to which the message was sent.
	Chat Chat `json:"chat"`

	// TODO: skipped some fields.

	// ReplyToMessage is the message to which this message is replying.
	// If this message is not a reply, ReplyToMessage is nil.
	ReplyToMessage *Message `json:"reply_to_message"`

	// EditDate is the Unix time that the message was last edited.
	EditDate *uint64 `json:"edit_date"`

	// Text is the text of the message, 0-4096 characters.
	Text *string `json:"text"`

	// TODO: skipped some fields.
}

// A User is a user connected to telegram.
type User struct {
	// ID is a unique identifier of this user or bot.
	ID int64 `json:"id"`

	// FirstName is the user's or bot's first name.
	FirstName string `json:"first_name"`

	// LastName is the user's or bot's last name.
	LastName string `json:"last_name"`

	// Username is the user's or bot's username.
	Username string `json:"username"`
}

type ChatType string

const (
	PrivateChatType    ChatType = "private"
	GroupChatType      ChatType = "group"
	SupergroupChatType ChatType = "supergroup"
	ChannelChatType    ChatType = "channel"
)

// A Chat is a telegram group, channel, or secret chat.
type Chat struct {
	// ID is a unique identifier for the chat.
	ID uint64 `json:"id"`

	// Type is the type of chat.
	Type ChatType `json:"type"`

	// Title is the title of the chat for supergroups, channels, and group chats.
	Title *string `json:"title"`

	// Username is the username for private chats, supergroups, and channels.
	Username *string `json:"username"`

	// FirstName is the first name of the other party in a private chat.
	FirstName *string `json:"first_name"`

	// LastName is the last name of the other party in a private chat.
	LastName *string `json:"last_name"`

	// AllMembersAreAdministrators is true if a group has 'all members are administrators' enabled.
	AllMembersAreAdministrators *bool `json:"all_members_are_administrators"`
}

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

	// ChannelPost is a new incoming post on a channel.
	ChannelPost *Message `json:"channel_post"`

	// EditedChannelPost is the new version of a channel post that is known to the bot and was edited.
	EditedChannelPost *Message `json:"edited_channel_post"`

	// InlineQuery is a new inline query.
	InlineQuery *map[string]interface{} `json:"inline_query"`

	// ChosenInlineResult is the result of an inline query that was chosen by the user and sent to their chat partner.
	ChosenInlineResult *map[string]interface{} `json:"chosen_inline_result"`

	// CallbackQuery is a new incoming callback query.
	CallbackQuery *map[string]interface{} `json:"callback_query"`
}

// A Message represents a message sent with telegram.
//
// TODO: Map all fields to a custom struct type instead of map[string]interface{}.
type Message struct {
	// MessageID is a unique identifier for the message within a chat.
	MessageID uint64 `json:"message_id"`

	// From is the sender of the message. It is nil for messages sent to channels.
	From *User `json:"from"`

	// Date is the Unix time that the message was sent.
	Date uint64 `json:"date"`

	// Chat is the chat to which the message was sent.
	Chat Chat `json:"chat"`

	// ForwardFrom is the sender of the original message, for a forwarded message.
	ForwardFrom *User `json:"forward_from"`

	// ForwardFromChat is the is the Chat of the original message, for a forwarded message.
	ForwardFromChat *Chat `json:"forward_from_chat"`

	// ForwardFromMessageID is the ID of the original message, for a forwarded message.
	ForwardFromMessageID uint64 `json:"forward_from_message_id"`

	// ForwardDate is the date of the original message, for a forwarded message.
	ForwardDate uint64 `json:"forward_date"`

	// ReplyToMessage is the message to which this message is replying.
	// If this message is not a reply, ReplyToMessage is nil.
	ReplyToMessage *Message `json:"reply_to_message"`

	// EditDate is the Unix time that the message was last edited.
	EditDate *uint64 `json:"edit_date"`

	// Text is the text of the message, 0-4096 characters.
	Text *string `json:"text"`

	Entities *[]map[string]interface{} `json:"entities"`
	Audio    *map[string]interface{}   `json:"audio"`
	Document *map[string]interface{}   `json:"document"`
	Game     *map[string]interface{}   `json:"game"`
	Photo    *[]map[string]interface{} `json:"photo"`
	Sticker  *map[string]interface{}   `json:"sticker"`
	Video    *map[string]interface{}   `json:"video"`
	Voice    *map[string]interface{}   `json:"voice"`
	Caption  *string                   `json:"caption"`
	Contact  *map[string]interface{}   `json:"contact"`
	Location *map[string]interface{}   `json:"location"`
	Venue    *map[string]interface{}   `json:"venue"`

	// NewChatMember is the User information of a new chat member, added to the group.
	NewChatMember *User `json:"new_chat_member"`

	// LeftChatMember is the User information of a chat member who just left the group.
	LeftChatMember *User `json:"left_chat_member"`

	NewChatTitle          *string                 `json:"new_chat_title"`
	NewChatPhoto          *map[string]interface{} `json:"new_chat_photo"`
	DeleteChatPhoto       *bool                   `json:"delete_chat_photo"`
	GroupChatCreated      *bool                   `json:"group_chat_created"`
	SupergroupChatCreated *bool                   `json:"supergroup_chat_created"`
	ChannelChatCreated    *bool                   `json:"channel_chat_created"`
	MigrateToChatId       *uint64                 `json:"migrate_to_chat_id"`
	MigrateFromChatId     *uint64                 `json:"migrate_from_chat_id"`
	PinnedMessage         *Message                `json:"pinned_message"`
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

// A Chat is a telegram group, channel, or secret chat.
type Chat struct {
	// ID is a unique identifier for the chat.
	ID int64 `json:"id"`

	// Type is the type of chat.
	Type string `json:"type"`

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

// PhotoSize represents a single size of a photo, file, or sticker thumbnail.
type PhotoSize struct {
	// FileID is the unique identifier for this file.
	FileID string `json:"file_id"`

	// Width is the photo width.
	Width int `json:"width"`

	// Height is the photo height.
	Height int `json:"height"`

	// FileSize is the file size.
	// TODO: what are the units, the docs don't say—bytes?
	FileSize *int `json:"file_size"`
}

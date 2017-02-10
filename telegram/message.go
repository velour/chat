package telegram

import "time"

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

	// Date is the Unix time, in seconds, that the message was sent.
	Date int64 `json:"date"`

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

	// Document indicates that the Message is a shared file.
	Document *Document `json:"document"`

	Game *map[string]interface{} `json:"game"`

	// Photo indicates that the Message is a shared photo.
	Photo *[]PhotoSize `json:"photo"`

	// Sticker indicates that the Message is a sticker.
	Sticker *Sticker `json:"sticker"`

	Video    *map[string]interface{} `json:"video"`
	Voice    *map[string]interface{} `json:"voice"`
	Caption  *string                 `json:"caption"`
	Contact  *map[string]interface{} `json:"contact"`
	Location *map[string]interface{} `json:"location"`
	Venue    *map[string]interface{} `json:"venue"`

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
	MigrateToChatID       *uint64                 `json:"migrate_to_chat_id"`
	MigrateFromChatID     *uint64                 `json:"migrate_from_chat_id"`
	PinnedMessage         *Message                `json:"pinned_message"`
}

// Time returns the time.Time represented by the Message Date field.
func (m *Message) Time() time.Time { return time.Unix(m.Date, 0) }

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

	// FileSize is the file size, if known.
	// TODO: what are the units, the docs don't say—bytes?
	FileSize *int `json:"file_size"`
}

// A File represents a file ready to be downloaded.
//
// A File differs from a Document,
// because a File represents a handle to file ready for download,
// whereas a Document describes a file uploaded to Telegram,
// whether or not it's ready for download.
type File struct {
	// FileID is the unique identifier of this file.
	FileID string `json:"file_id"`

	// FileSize is the file size, if known.
	// TODO: what are the units, the docs don't say—bytes?
	FileSize *int `json:"file_size"`

	// FilePath is the path to the file.
	// The bot can download the file at:
	// https://api.telegram.org/file/bot<token>/<file_path>
	// The link is guaranteed to be valid for 1 hour
	// from the time that the File was returned
	// by the getFile method.
	FilePath *string `json:"file_path"`
}

// A Document is a general file.
//
// A Document differs from a File,
// because a File represents a handle to file ready for download,
// whereas a Document describes a file uploaded to Telegram,
// whether or not it's ready for download.
type Document struct {
	// FileID is the unique identifier of this file.
	FileID string `json:"file_id"`
}

// A Sticker represents a sticker sent in a Message.
type Sticker struct {
	// FileID is the unique identifier of this sticker's file.
	FileID string `json:"file_id"`

	// Thumb is an optional thumbnail photo.
	Thumb *PhotoSize `json:"thumb"`

	// Emoji is the optional emoticon associated with the sticker.
	Emoji *string `json:"emoji"`
}

// A ChatMember is a User who is a member of a chat.
type ChatMember struct {
	// User is the user's information.
	User User `json:"user"`

	// Status is one of “creator”, “administrator”, “member”, “left” or “kicked”.
	Status string `json:"status"`
}

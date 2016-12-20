// Package chat provides a common API for chat service clients.
package chat

// A Client is a handle to a client connection to a chat service.
type Client interface {
	// Join joins the client to a new Channel.
	//
	// For some chat services, like Slack and Telegram,
	// bots remain in their joined Channels even after disconnect.
	// In these cases, Join may not actually change the joined-status of the bot,
	// but simply return the Channel interface.
	Join(channel string) (Channel, error)
}

// A Channel is a handle to a channel joined by the Client.
type Channel interface {
	// Receive receives the next event from the Channel.
	Receive() (interface{}, error)

	// Send sends text to the Channel and returns the sent Message.
	Send(text string) (Message, error)

	// Delete deletes the a message.
	//
	// Implementations that do not support deleting messages may treat this as a no-op.
	Delete(id MessageID) error

	// Edit edits a sent message to have the given next text,
	// and returns the unique ID representing the edited message.
	//
	// Implementations that do not support editing messages may treat this as a no-op.
	Edit(id MessageID, newText string) (MessageID, error)

	// Reply replies to a message and returns the replied Message.
	//
	// Implementations that do not support editing messages may treat this as a Send.
	// As an enhancement, such an implementation could instead
	// quote the user and text from the replyTo message,
	// and send the reply text following the quote.
	Reply(replyTo Message, text string) (Message, error)
}

// A MessageID is a unique string representing a sent message.
type MessageID string

// A Message is an event describing a message sent by a user.
type Message struct {
	// ID is a unique string identifier representing the Message.
	ID MessageID

	// From the user who sent the Message.
	From User

	// Text is the text of the Message.
	Text string
}

// A Delete is an event describing a message deleted by a user.
type Delete struct {
	// ID is the ID of the deleted message.
	ID MessageID
}

// An Edit is an event describing a message edited by a user.
type Edit struct {
	// ID is the unique identifier of the message that was edited.
	ID MessageID

	// NewID is unique identifier of the message after editing.
	NewID MessageID

	// Text is the new text of the message.
	Text string
}

// A Reply is an event describing a user replying to a message.
type Reply struct {
	// ReplyTo is the message that was replied to.
	ReplyTo Message

	// Reply is the message of the reply.
	Reply Message
}

// A Join is an event describing a user joining a channel.
type Join struct {
	// Who is the User who joined.
	Who User
}

// A Leave is an event describing a user leaving a channel.
type Leave struct {
	// Who is the User who parted.
	Who User
}

// A Rename is an event describing a user changing their Nick or Name.
type Rename struct {
	// Who is the User who renamed.
	// ID should remain the same, but Nick or Name will be the updated value.
	Who User
}

// A UserID is a unique string representing a user.
type UserID string

type User struct {
	// ID is a unique string identifying the User.
	ID UserID

	// Nick is the user's nickname.
	Nick string

	// Name is the user's full name.
	Name string
}

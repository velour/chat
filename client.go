// Package chat provides a common API for chat service clients.
package chat

import "context"

// A Client is a handle to a client connection to a chat service.
type Client interface {
	// Close closes the Client, reporting any pending errors encountered.
	Close(ctx context.Context) error

	// Join joins the client to a new Channel.
	//
	// For some chat services, like Slack and Telegram,
	// bots remain in their joined Channels even after disconnect.
	// In these cases, Join may not actually change the joined-status of the bot,
	// but simply return the Channel interface.
	Join(ctx context.Context, channel string) (Channel, error)
}

// A Channel is a handle to a channel joined by the Client.
//
// Channels are expected to be comparable with ==.
// The simplest way to achieve this is to implement Channel with a pointer.
type Channel interface {
	// Name returns the Channel's name.
	Name() string

	// ServiceName returns the name of the Channel's chat service.
	// This can be, a service name like "Telegram",
	// or a name and address like "IRC (irc.freenode.net)",
	// or anything useful to distinguish the service from others.
	ServiceName() string

	// Receive receives the next event from the Channel.
	Receive(ctx context.Context) (Event, error)

	// Who returns the Users connected to the Channel.
	Who(ctx context.Context) ([]User, error)

	// Send sends text to the Channel and returns the sent Message.
	Send(ctx context.Context, text string) (Message, error)

	// SendAs sends text to the Channel on behalf of a given user and returns the sent Message.
	// The difference between SendAs and Send is that
	// SendAs indicates a message sent on behalf of a user other that the current Client.
	// An acceptable implementation may simply prefix text with the user's name or nick.
	//
	// Note that sendAs.ID may not be from the chat service undelying this Channel.
	SendAs(ctx context.Context, sendAs User, text string) (Message, error)

	// Delete deletes the a message.
	//
	// Implementations that do not support deleting messages may treat this as a no-op.
	Delete(ctx context.Context, id MessageID) error

	// Edit changes the text of a Message previously sent on this Channel.
	// The Text field of the given Message is used as the new Text.
	//
	// Implementations that do not support editing messages
	// may treat this as a no-op.
	Edit(context.Context, Message) (Message, error)

	// Reply replies to a message and returns the replied Message.
	//
	// Implementations that do not support editing messages may treat this as a Send.
	// As an enhancement, such an implementation could instead
	// quote the user and text from the replyTo message,
	// and send the reply text following the quote.
	Reply(ctx context.Context, replyTo Message, text string) (Message, error)

	// ReplyAs replies to a message on behalf of a given user and returns the replied Message.
	// The difference between ReplyAs and Reply is that
	// ReplyAs indicates a message sent on behalf of a user other that the current Client.
	// An acceptable implementation may simply prefix text with the user's name or nick.
	//
	// Note that sendAs.ID may not be from the chat service undelying this Channel.
	ReplyAs(ctx context.Context, sendAs User, replyTo Message, text string) (Message, error)
}

// An Event signifies something happening on a Channel.
type Event interface {
	// Origin is the Channel that originated the Event.
	// Events may be forwarded, for example through a Bridge.
	// However, Origin is always the originator of the Event.
	Origin() Channel
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

func (e Message) Origin() Channel { return e.From.Channel }

// A Delete is an event describing a message deleted by a user.
type Delete struct {
	// Who is the User who deleted the message.
	Who User

	// ID is the ID of the deleted message.
	ID MessageID
}

func (e Delete) Origin() Channel { return e.Who.Channel }

// An Edit is an event describing a message edited by a user.
type Edit struct {
	// OrigID is the unique identifier of the message that was edited.
	OrigID MessageID

	// New is the new Message.
	New Message
}

func (e Edit) Origin() Channel { return e.New.From.Channel }

// A Reply is an event describing a user replying to a message.
type Reply struct {
	// ReplyTo is the message that was replied to.
	ReplyTo Message

	// Reply is the message of the reply.
	Reply Message
}

func (e Reply) Origin() Channel { return e.Reply.From.Channel }

// A Join is an event describing a user joining a channel.
type Join struct {
	// Who is the User who joined.
	Who User
}

func (e Join) Origin() Channel { return e.Who.Channel }

// A Leave is an event describing a user leaving a channel.
type Leave struct {
	// Who is the User who parted.
	Who User
}

func (e Leave) Origin() Channel { return e.Who.Channel }

// A Rename is an event describing a user info change.
type Rename struct {
	// From is the original User information.
	From User
	// To is the new User information.
	To User
}

func (e Rename) Origin() Channel { return e.To.Channel }

// A UserID is a unique string representing a user.
type UserID string

// User represents a user of a chat network.
type User struct {
	// ID is a unique string identifying the User.
	ID UserID

	// Nick is the user's nickname.
	Nick string

	// FullName is the user's full name.
	FullName string

	// DisplayName is the preferred display name for a user.
	//
	// Different chat services have different preferences
	// for whether they display the user's ID, nick, full name,
	// or something else.
	// That preference should be reflected in this field.
	DisplayName string

	// PhotoURL, if non-empty, is the URL to the User's profile photo.
	PhotoURL string

	// Channel is the relevant Channel to which the User belongs.
	// Note that the User may belong to multiple Channels.
	// This is the Channel relevant to the current context.
	// For example, in an event, it's the Channel generating the event.
	Channel Channel
}

// Name returns a name for the User that is suitable for display.
func (u User) Name() string {
	switch {
	case u.DisplayName != "":
		return u.DisplayName
	case u.Nick != "":
		return u.Nick
	case u.FullName != "":
		return u.FullName
	case u.ID != "":
		return string(u.ID)
	default:
		return "unknown"
	}
}

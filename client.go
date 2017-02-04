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

	// Send sends Message to the Channel and returns the Message with its ID set.
	//
	// The ID of the given Message is ignored.
	// The ID of the returned Message is a unique identifier for the sent Message.
	//
	// If From is nil, the Message is sent from this Channel's User.
	// If the From field is non-nil, the Message is sent on behalf of that User.
	// What is sent should clearly indicate the From user.
	// For example, an acceptable implementation
	// can prefix the Message Text with the User's name.
	//
	// If ReplyTo is non-nil, the Message is a reply to the ReplyTo Message.
	// If ReplyTo.ID is non-empty, it identifies a Message
	// previously sent on this Channel.
	// If ReplyTo.ID is empty, the exact ReplyTo Message is unknown.
	// In both cases, ReplyTo.Text is non-empty.
	// If ReplyTo.From is nil, ReplyTo was sent from this Channel's User.
	//
	// An implementation that does not support replies may ignore ReplyTo.
	// However, as an enhancement, such an implementation
	// could quote the text of the ReplyTo message
	// before sending the reply.
	Send(ctx context.Context, msg Message) (Message, error)

	// Delete deletes the a Message previously sent on this Channel.
	//
	// Implementations that do not support deleting messages
	// may treat this as a no-op.
	Delete(context.Context, Message) error

	// Edit changes the text of a Message previously sent on this Channel.
	// The Text field of the given Message is used as the new Text.
	//
	// Implementations that do not support editing messages
	// may treat this as a no-op.
	Edit(context.Context, Message) (Message, error)
}

// Say sends a Message to the Channel with the given text.
func Say(ctx context.Context, ch Channel, text string) (Message, error) {
	return ch.Send(ctx, Message{Text: text})
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

	// ReplyTo is a Message to which this Message is a reply.
	// If ReplyTo is nil, the Message is not a reply.
	ReplyTo *Message

	// From is the User who sent the Message.
	From *User

	// Text is the text of the Message.
	Text string
}

func (e Message) Origin() Channel { return e.From.Channel }

// A Delete is an event describing a message deleted by a user.
type Delete struct {
	// ID is the ID of the deleted message.
	ID MessageID

	// Channel is the origin of the delete event.
	Channel Channel
}

func (e Delete) Origin() Channel { return e.Channel }

// An Edit is an event describing a message edited by a user.
type Edit struct {
	// OrigID is the unique identifier of the message that was edited.
	OrigID MessageID

	// New is the new Message.
	New Message
}

func (e Edit) Origin() Channel { return e.New.From.Channel }

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

package slack

import "github.com/velour/chat"

// Update represents a RTS update message.
type Update struct {
	ID      uint64      `json:"id"`
	Type    string      `json:"type"`
	SubType string      `json:"subtype"`
	Channel string      `json:"channel"`
	Text    string      `json:"text"`
	User    chat.UserID `json:"user"`
	Ts      string      `json:"ts"`
	Error   struct {
		Code uint64 `json:"code"`
		Msg  string `json:"msg"`
	} `json:"error"`
	*File       `json:"file"`
	Message     *Update `json:"message"`
	DeletedTS   string  `json:"deleted_ts"`
	Attachments []struct {
		Title      string `json:"title"`
		ImageURL   string `json:"image_url"`
		Footer     string `json:"footer"`
		AuthorName string `json:"author_name"`
	} `json:"attachments"`
}

// File represents a shared file.
type File struct {
	ID                 string `json:"id"`
	URLPrivateDownload string `json:"url_private_download"`
	Mimetype           string `json:"mimetype"`
}

// ResponseHeader is a header common to all slack HTTP responses.
// Each message type that is a response should embed ResponseHeader.
type ResponseHeader struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error"`
	Warning string `json:"warning"`
}

func (rh *ResponseHeader) Header() ResponseHeader { return *rh }

// A User object describes a slack user.
type User struct {
	ID string `json:"id"`
	// Name is the username without a leading @.
	Name    string `json:"name"`
	Profile struct {
		RealName string `json:"real_name"`

		// Image is the largest profile icon available
		Image string `json:"image_192"`
	}
	// BUG(eaburns): Add remaining User object fields.
}

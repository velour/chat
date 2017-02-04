package telegram

import (
	"html"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/velour/chat"
)

// formatText returns an HTML-formatted representation of a chat.Message text,
// which is appropriate for the text of a Telegram sendMessage.
//
// It does the following formatting:
//
// First the text is HTML escaped.
//
// If the text begins with "/me" followed by non-newline-whitespace:
// • this prefix is stripped,
// • leading and trailing non-newline-whitespace is stripped,
// • non-link text is wrapped in <em> HTML tags.
// Otherwise: leading and trailing non-newline-whitespace is trimmed.
//
// If msg.From is non-nil, the text is prefixed by "<b>"+msg.From.Name()+":</b> ".
func formatText(msg chat.Message) string {
	text := html.EscapeString(msg.Text)
	if strings.HasPrefix(text, "/me") {
		rest := strings.TrimPrefix(text, "/me")
		if r, _ := utf8.DecodeRuneInString(rest); rest == "" || isNonNewlineSpace(r) {
			text = trimSpace(rest)
			text = emphasize(text)
			if msg.From != nil {
				text = "<b>" + msg.From.Name() + "</b> " + text
			}
			return text
		}
	}
	text = trimSpace(text)
	if msg.From != nil {
		text = "<b>" + msg.From.Name() + ":</b> " + text
	}
	return text
}

func isNonNewlineSpace(r rune) bool { return r != '\n' && unicode.IsSpace(r) }

func trimSpace(s string) string { return strings.TrimFunc(s, isNonNewlineSpace) }

func addName(from *chat.User, s string) string {
	if from != nil {
		return "<b>" + from.Name() + ":</b> " + s
	}
	return s
}

// emphasize wraps all non-empty, non-link text in <em></em> HTML tags.
//
// A link is a string of non-whitespace runes that
// are at the beginning of text or preceeded by whitespace,
// begin with "http://" or "https://",
// and parse successfully with url.Parse.
// Links do not overlap.
func emphasize(text string) string {
	var str string
	for {
		i, link := linkIndex(text)
		if i < 0 {
			return str + em(text)
		}
		str += em(text[:i]) + link
		text = text[i+len(link):]
	}
}

func em(s string) string {
	if s != "" {
		return "<em>" + s + "</em>"
	}
	return s
}

// linkIndex returns the index and text of the first link in the text,
// or -1 and the empty string if there are no links.
func linkIndex(text string) (int, string) {
	var offs int
	for {
		i := strings.Index(text, "http")
		if i < 0 {
			return -1, ""
		}
		// There is a 0th field, because text[i:] begins with http; it's not empty.
		link := strings.Fields(text[i:])[0]
		if !strings.HasPrefix(link, "http://") && !strings.HasPrefix(link, "https://") {
			goto next
		}
		// DecodeLastRuneInString returns RuneError,0 on empty string,
		// so i==0 case is safe.
		if r, _ := utf8.DecodeLastRuneInString(text[:i]); i > 0 && !unicode.IsSpace(r) {
			goto next
		}
		if _, err := url.Parse(link); err != nil {
			goto next
		}
		return i + offs, link

	next:
		offs = i + 1
		text = text[i+1:]
	}
}

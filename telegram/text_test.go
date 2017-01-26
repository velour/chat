package telegram

import (
	"testing"

	"github.com/velour/chat"
)

func TestFormatText(t *testing.T) {
	tests := []struct {
		name, text, want string
	}{
		{text: "", want: ""},
		{text: "  \t\t", want: ""},
		{text: "hello", want: "hello"},
		{text: "☺", want: "☺"},
		{text: "αβξ", want: "αβξ"},
		{name: "ĉapelita", text: "", want: "<b>ĉapelita:</b> "},
		{name: "ĉapelita", text: "hello", want: "<b>ĉapelita:</b> hello"},
		{name: "ĉapelita", text: "☺", want: "<b>ĉapelita:</b> ☺"},
		{name: "ĉapelita", text: "αβξ", want: "<b>ĉapelita:</b> αβξ"},

		{text: "/me", want: ""},
		{text: "/meat", want: "/meat"},
		{text: "/me\nat", want: "/me\nat"}, // don't em on newline
		{text: "/me says hi", want: "<em>says hi</em>"},
		{text: "/me αβξ", want: "<em>αβξ</em>"},
		{text: "/me αβξ    ", want: "<em>αβξ</em>"},
		{text: "/me\tsays hi", want: "<em>says hi</em>"},
		{text: "/me\tαβξ", want: "<em>αβξ</em>"},
		{text: "/me\tαβξ\t\t", want: "<em>αβξ</em>"},

		{text: "/me http://www.a.com", want: "http://www.a.com"},
		{text: "/me https://www.a.com", want: "https://www.a.com"},
		{
			text: "/me links http://www.a.com",
			want: "<em>links </em>http://www.a.com",
		},
		{
			text: "/me links https://www.a.com",
			want: "<em>links </em>https://www.a.com",
		},
		{
			text: "/me links https://www.a.com and https://www.b.com",
			want: "<em>links </em>https://www.a.com<em> and </em>https://www.b.com",
		},
		{
			text: "/me doesn't link httpnotalink",
			want: "<em>doesn't link httpnotalink</em>",
		},
		{
			text: "/me links https://www.a.com but not httpnotalink",
			want: "<em>links </em>https://www.a.com<em> but not httpnotalink</em>",
		},
		{
			name: "ĉapelita",
			text: "/me links http://www.a.com",
			want: "<b>ĉapelita:</b> <em>links </em>http://www.a.com",
		},
	}
	for _, test := range tests {
		msg := chat.Message{Text: test.text}
		if test.name != "" {
			msg.From = &chat.User{DisplayName: test.name}
		}
		got := formatText(msg)
		if got == test.want {
			continue
		}
		t.Errorf("formatText(%+v)=%q, want %q", msg, got, test.want)
	}
}

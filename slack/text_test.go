package slack

import (
	"testing"
)

func testFixTest(t *testing.T, msg, expected string) {
	actual := fixText(nil, msg)
	if len(actual) == 0 {
		t.Errorf("Did not get any results.")
	}
	if actual != expected {
		t.Errorf("Got:\t'%s'\nExpected:\t'%s'.", actual, expected)
	}
}

func TestFixTxt(t *testing.T) {
	findUser := func(id string) (string, bool) {
		if id == "Ufound" {
			return "found", true
		}
		return "", false
	}
	tests := []struct {
		name, text, want string
	}{
		{name: "Unclosed tag: space", text: "<abc >", want: "<abc >"},
		{name: "Unclosed tag: EOF", text: "<abc", want: "<abc"},
		{
			name: "User mention found",
			text: "<@Ufound>",
			want: "@found",
		},
		{
			name: "User mention unfound",
			text: "<@Unotfound>",
			want: "@Unotfound",
		},
		{
			name: "Pipe mention",
			text: "<@U0S5BGJLX|someone>",
			want: "someone",
		},
		{
			name: "Hash tag",
			text: "prefix <#C1A2B3C4D|theclub> suffix",
			want: "prefix #theclub suffix",
		},
		{
			name: "Only a link",
			text: "<https://twitter.com/rob_pike/status/816766400257658880>",
			want: "https://twitter.com/rob_pike/status/816766400257658880",
		},
		{
			name: "Link with pipe",
			text: "someone uploaded a file: <https://xyz.slack.com/files/someone/F3MRRSDM2/img_0063.jpg|Slack for Android Upload> and commented: It exists!",
			want: "someone uploaded a file: https://xyz.slack.com/files/someone/F3MRRSDM2/img_0063.jpg and commented: It exists!",
		},
		{
			name: "Link without a pipe",
			text: "someone uploaded a file: <https://xyz.slack.com/files/someone/F3MRRSDM2/img_0063.jpg> and commented: It exists!",
			want: "someone uploaded a file: https://xyz.slack.com/files/someone/F3MRRSDM2/img_0063.jpg and commented: It exists!",
		},
		{
			name: "Link without a pipe, two <",
			text: "someone uploaded a file: <https://hashvelour.slack.com/files/someone/F3MRRSDM2/img_0063.jpg> and commented: It exists but a > b!",
			want: "someone uploaded a file: https://hashvelour.slack.com/files/someone/F3MRRSDM2/img_0063.jpg and commented: It exists but a > b!",
		},
		{
			name: "Emoji",
			text: "prefix :copyright: mid:interrobang::relaxed:suffix",
			want: "prefix © mid⁉☺suffix",
		},
	}
	for _, test := range tests {
		if got := fixText(findUser, test.text); got != test.want {
			t.Errorf("%s fixText(_, %q)=%q, want %q",
				test.name, test.text, got, test.want)
		}
	}
}

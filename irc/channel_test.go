package irc

import (
	"strings"
	"testing"
)

type splitTest struct {
	channel, prefix, text, suffix string
}

func TestSplitPrivMsg(t *testing.T) {
	tests := []splitTest{
		{
			channel: "#velour",
			prefix:  "eaburns: ",
			text:    "Hello, World",
		},
		{
			channel: "#velour",
			prefix:  actionPrefix + "eaburns: ",
			suffix:  actionSuffix,
			text:    `says "Hello, World"`,
		},
	}
	for i := 0; i < MaxBytes*2; i++ {
		repeat := strings.Repeat("☺", i)
		tests = append(tests,
			splitTest{
				channel: "#velour",
				text:    repeat,
			},
			splitTest{
				channel: "#velour",
				prefix:  "eaburns: ",
				text:    repeat,
			},
			splitTest{
				channel: "#velour",
				suffix:  "!!!!",
				text:    repeat,
			},
			splitTest{
				channel: "#velour",
				prefix:  "eaburns: ",
				suffix:  "!!!!",
				text:    repeat,
			},
		)
	}
	for _, test := range tests {
		splits := splitPrivMsg("", test.channel, test.prefix, test.suffix, test.text)
		for i, s := range splits {
			msg := Message{
				Command:   PRIVMSG,
				Arguments: []string{test.channel, s},
			}
			if n := len(msg.Bytes()); n > MaxBytes {
				t.Errorf("splitPrivMsg(%q, %q, %q, %q)=%v len([%d])= is %d",
					test.channel, test.prefix, test.suffix, test.text, splits, i, n)
				break
			}
		}
	}
}

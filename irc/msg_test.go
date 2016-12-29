package irc

import (
	"reflect"
	"testing"
)

func TestReadMessageOK(t *testing.T) {
	tests := []struct {
		raw string
		msg Message
	}{
		{
			raw: ":e!foo@bar.com JOIN #test54321",
			msg: Message{
				Origin:    "e",
				User:      "foo",
				Host:      "bar.com",
				Command:   "JOIN",
				Arguments: []string{"#test54321"},
			},
		},
		{
			raw: ":e JOIN #test54321",
			msg: Message{
				Origin:    "e",
				Command:   "JOIN",
				Arguments: []string{"#test54321"},
			},
		},
		{
			raw: "JOIN #test54321",
			msg: Message{
				Command:   "JOIN",
				Arguments: []string{"#test54321"},
			},
		},
		{
			raw: "JOIN #test54321 :foo bar",
			msg: Message{
				Command:   "JOIN",
				Arguments: []string{"#test54321", "foo bar"},
			},
		},
		{
			raw: "JOIN #test54321 ::foo bar",
			msg: Message{
				Command:   "JOIN",
				Arguments: []string{"#test54321", ":foo bar"},
			},
		},
		{
			raw: "JOIN    #test54321    foo       bar   ",
			msg: Message{
				Command:   "JOIN",
				Arguments: []string{"#test54321", "foo", "bar"},
			},
		},
		{
			raw: "JOIN :",
			msg: Message{
				Command:   "JOIN",
				Arguments: []string{""},
			},
		},
	}

	for _, test := range tests {
		m, err := Parse([]byte(test.raw))
		if err != nil || !reflect.DeepEqual(m, test.msg) {
			t.Errorf("ParseMessage(%q)=%#v,%v want=%#v,nil", test.raw, m, err, test.msg)
		}
	}
}

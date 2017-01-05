package slack

import (
	"testing"
)

func testDecode(t *testing.T, msg, expected string) {
	actual := media(msg)
	if len(actual) == 0 {
		t.Errorf("Did not get any results.")
	}
	if actual != expected {
		t.Errorf("Got:\t'%s'\nExpected:\t'%s'.", actual, expected)
	}
}

func TestDecodeSlackLink(t *testing.T) {
	msg := "<@U0S5BGJLX|skiesel> uploaded a file: <https://hashvelour.slack.com/files/skiesel/F3MRRSDM2/img_0063.jpg|Slack for Android Upload> and commented: It exists!"
	expected := "<@U0S5BGJLX|skiesel> uploaded a file: https://hashvelour.slack.com/files/skiesel/F3MRRSDM2/img_0063.jpg and commented: It exists!"
	testDecode(t, msg, expected)
}

func TestDecodeSlackLinkNoPipe(t *testing.T) {
	msg := "<@U0S5BGJLX|skiesel> uploaded a file: <https://hashvelour.slack.com/files/skiesel/F3MRRSDM2/img_0063.jpg> and commented: It exists!"
	expected := "<@U0S5BGJLX|skiesel> uploaded a file: https://hashvelour.slack.com/files/skiesel/F3MRRSDM2/img_0063.jpg and commented: It exists!"
	testDecode(t, msg, expected)
}

func TestDecodeSlackLinkNoPipeTwoGt(t *testing.T) {
	msg := "<@U0S5BGJLX|skiesel> uploaded a file: <https://hashvelour.slack.com/files/skiesel/F3MRRSDM2/img_0063.jpg> and commented: It exists but a > b!"
	expected := "<@U0S5BGJLX|skiesel> uploaded a file: https://hashvelour.slack.com/files/skiesel/F3MRRSDM2/img_0063.jpg and commented: It exists but a > b!"
	testDecode(t, msg, expected)
}

func TestDecodeRegularLink(t *testing.T) {
	msg := "<https://twitter.com/rob_pike/status/816766400257658880>"
	expected := "https://twitter.com/rob_pike/status/816766400257658880"
	testDecode(t, msg, expected)
}

func TestDecodeSlackNonLink(t *testing.T) {
	msg := "<@U0S5BGJLX|skiesel>"
	expected := "<@U0S5BGJLX|skiesel>"
	testDecode(t, msg, expected)
}

func TestDecodeSlackNonLinkText(t *testing.T) {
	msg := "a < b"
	expected := "a < b"
	testDecode(t, msg, expected)
}

func TestDecodeSlackNonLinkTextWithGt(t *testing.T) {
	msg := "a < b and b > c"
	expected := "a < b and b > c"
	testDecode(t, msg, expected)
}

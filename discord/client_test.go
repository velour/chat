package discord

import (
	"net/http"
	"testing"
)

func TestLimitKey(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   string
	}{
		{"GET", "", ""},
		{"DELETE", "", ""},
		{"GET", "/", "/"},
		{"DELETE", "/", "/"},
		{"GET", "/channels", "/channels"},
		{"DELETE", "/channels", "/channels"},
		{"GET", "/channels/123", "/channels/123"},
		{"GET", "/channels/123/messages", "/channels/123/messages"},
		{"GET", "/channels/123/messages/456", "/channels/123/messages"},
		{"GET", "/channels/123/messages/789", "/channels/123/messages"},
		{"DELETE", "/channels/123/messages/789", "DELETE"},
		{"DELETE", "/channels/123/messages/123", "DELETE"},
		{"GET", "/some/123/other/456", "/some/123/other"},
		{"GET", "/some/123/other/456/path", "/some/123/other/456/path"},
		{"GET", "/some/123/other", "/some/123/other"},
		{"GET", "/some/123", "/some"},
	}
	for _, test := range tests {
		url := "http://test.com" + test.path
		req, err := http.NewRequest(test.method, url, nil)
		if err != nil {
			panic(err)
		}
		if got := limitKey(req); got != test.want {
			t.Errorf("limitKey(%s)=%s, want %s", test.path, got, test.want)
		}
	}
}

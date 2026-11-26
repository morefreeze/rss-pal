package mobilepush

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateEndpoint(t *testing.T) {
	for _, endpoint := range []string{
		"https://fcm.googleapis.com/fcm/send/token",
		"https://web.push.apple.com/Q-token",
		"https://updates.push.services.mozilla.com/wpush/v2/token",
	} {
		if err := ValidateEndpoint(endpoint); err != nil {
			t.Errorf("rejected provider endpoint %q: %v", endpoint, err)
		}
	}
	for _, endpoint := range []string{
		"http://fcm.googleapis.com/fcm/send/token", "https://127.0.0.1/private",
		"https://fcm.googleapis.com:444/path", "https://web.push.apple.com.attacker.example/path",
		"https://user@fcm.googleapis.com/path", "https://fcm.googleapis.com/path#fragment",
	} {
		if err := ValidateEndpoint(endpoint); err == nil {
			t.Errorf("accepted invalid endpoint %q", endpoint)
		}
	}
}

func TestPayloadUsesTotalUnread(t *testing.T) {
	body, err := payload(2, 20)
	if err != nil {
		t.Fatal(err)
	}
	var message struct {
		Badge        int `json:"app_badge"`
		Notification struct {
			Body     string `json:"body"`
			Navigate string `json:"navigate"`
		} `json:"notification"`
	}
	if err := json.Unmarshal(body, &message); err != nil {
		t.Fatal(err)
	}
	if message.Badge != 20 || !strings.Contains(message.Notification.Body, "新增 2 篇") ||
		!strings.Contains(message.Notification.Body, "共 20 篇未读") || message.Notification.Navigate != "/articles" {
		t.Fatalf("unexpected notification: %s", body)
	}
}

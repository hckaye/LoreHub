package notificationemail

import (
	"strings"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func TestRenderMessageUsesLocaleAndEscapesHTML(t *testing.T) {
	claim := platform.NotificationEmailClaim{
		Recipient: "alice@example.com",
		Locale:    "ja",
		Title:     `<script>alert("title")</script>`,
		Body:      `<img src=x onerror=alert("body")>`,
		Href:      "/acme/game/issues/7",
	}
	message := RenderMessage(claim, "https://lorehub.example")
	if message.Recipient != claim.Recipient || !strings.Contains(message.Text, "LoreHubで開く") ||
		!strings.Contains(message.Text, "https://lorehub.example/ja/acme/game/issues/7") {
		t.Fatalf("unexpected Japanese message: %+v", message)
	}
	if strings.Contains(message.HTML, "<script>") || strings.Contains(message.HTML, "<img") ||
		!strings.Contains(message.HTML, "&lt;script&gt;") {
		t.Fatalf("message HTML was not escaped: %s", message.HTML)
	}
}

func TestRenderMessageDefaultsUnknownLocaleToEnglish(t *testing.T) {
	message := RenderMessage(platform.NotificationEmailClaim{
		Recipient: "bob@example.com", Locale: "fr", Title: "Changed", Href: "/notifications",
	}, "https://lorehub.example/")
	if !strings.Contains(message.Text, "Open in LoreHub") ||
		!strings.Contains(message.Text, "https://lorehub.example/en/notifications") {
		t.Fatalf("unexpected English message: %+v", message)
	}
}

package notificationemail

import (
	"html"
	"net/url"
	"strings"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type Message struct {
	Recipient string
	Subject   string
	Text      string
	HTML      string
}

func RenderMessage(claim platform.NotificationEmailClaim, publicOrigin string) Message {
	locale := claim.Locale
	if locale != "ja" {
		locale = "en"
	}
	link := notificationURL(publicOrigin, locale, claim.Href)
	if locale == "ja" {
		return japaneseMessage(claim, link)
	}
	return englishMessage(claim, link)
}

func englishMessage(claim platform.NotificationEmailClaim, link string) Message {
	title := safeSubject(claim.Title)
	body := strings.TrimSpace(claim.Body)
	text := title + "\n\n"
	if body != "" {
		text += body + "\n\n"
	}
	text += "Open in LoreHub:\n" + link + "\n\n"
	text += "You received this email because email notifications are enabled for your account."
	return Message{
		Recipient: claim.Recipient,
		Subject:   "[LoreHub] " + title,
		Text:      text,
		HTML:      htmlMessage(title, body, "Open in LoreHub", link),
	}
}

func japaneseMessage(claim platform.NotificationEmailClaim, link string) Message {
	title := safeSubject(claim.Title)
	body := strings.TrimSpace(claim.Body)
	text := title + "\n\n"
	if body != "" {
		text += body + "\n\n"
	}
	text += "LoreHubで開く:\n" + link + "\n\n"
	text += "アカウント設定でメール通知が有効になっているため、このメールを送信しました。"
	return Message{
		Recipient: claim.Recipient,
		Subject:   "[LoreHub] " + title,
		Text:      text,
		HTML:      htmlMessage(title, body, "LoreHubで開く", link),
	}
}

func safeSubject(value string) string {
	value = strings.Map(func(character rune) rune {
		if character == '\r' || character == '\n' || character < ' ' {
			return ' '
		}
		return character
	}, strings.TrimSpace(value))
	value = strings.Join(strings.Fields(value), " ")
	characters := []rune(value)
	if len(characters) > 200 {
		value = string(characters[:200])
	}
	if value == "" {
		return "Notification"
	}
	return value
}

func htmlMessage(title string, body string, linkLabel string, link string) string {
	paragraph := ""
	if body != "" {
		paragraph = `<p style="white-space:pre-wrap">` + html.EscapeString(body) + `</p>`
	}
	return `<!doctype html><html><body>` +
		`<h1 style="font-size:20px">` + html.EscapeString(title) + `</h1>` + paragraph +
		`<p><a href="` + html.EscapeString(link) + `">` + html.EscapeString(linkLabel) + `</a></p>` +
		`</body></html>`
}

func notificationURL(publicOrigin string, locale string, href string) string {
	origin := strings.TrimRight(publicOrigin, "/")
	path := "/" + locale + "/" + strings.TrimLeft(href, "/")
	parsed, err := url.Parse(origin + path)
	if err != nil {
		return origin + path
	}
	return parsed.String()
}

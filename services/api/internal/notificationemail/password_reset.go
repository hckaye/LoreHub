package notificationemail

import (
	"context"
	"errors"
)

// PasswordResetMailer renders and sends the built-in password reset email.
type PasswordResetMailer struct {
	sender Sender
}

func NewPasswordResetMailer(sender Sender) (*PasswordResetMailer, error) {
	if sender == nil {
		return nil, errors.New("password reset mailer needs a sender")
	}
	return &PasswordResetMailer{sender: sender}, nil
}

func (mailer *PasswordResetMailer) SendPasswordReset(
	ctx context.Context,
	recipient string,
	locale string,
	resetURL string,
) error {
	return mailer.sender.Send(ctx, renderPasswordReset(recipient, locale, resetURL))
}

func renderPasswordReset(recipient string, locale string, resetURL string) Message {
	if locale == "ja" {
		title := "パスワードの再設定"
		body := "LoreHubアカウントのパスワード再設定がリクエストされました。心当たりがない場合は、このメールを無視してください。" +
			"リンクは60分で無効になります。"
		return Message{
			Recipient: recipient,
			Subject:   "[LoreHub] " + title,
			Text:      title + "\n\n" + body + "\n\nパスワードを再設定する:\n" + resetURL + "\n",
			HTML:      htmlMessage(title, body, "パスワードを再設定する", resetURL),
		}
	}
	title := "Reset your password"
	body := "A password reset was requested for your LoreHub account. If you did not request it, ignore this email. " +
		"The link expires in 60 minutes."
	return Message{
		Recipient: recipient,
		Subject:   "[LoreHub] " + title,
		Text:      title + "\n\n" + body + "\n\nReset your password:\n" + resetURL + "\n",
		HTML:      htmlMessage(title, body, "Reset your password", resetURL),
	}
}

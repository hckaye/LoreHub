package notificationemail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	gomail "github.com/wneessen/go-mail"
)

const (
	TLSModeStartTLS = "starttls"
	TLSModeTLS      = "tls"
	TLSModeNone     = "none"
)

type SMTPConfig struct {
	Host        string
	Port        int
	Username    string
	Password    string
	FromAddress string
	FromName    string
	TLSMode     string
	Timeout     time.Duration
}

type SMTPSender struct {
	client      *gomail.Client
	fromAddress string
	fromName    string
}

func NewSMTPSender(config SMTPConfig) (*SMTPSender, error) {
	if strings.TrimSpace(config.Host) == "" || strings.ContainsAny(config.Host, "\r\n\t ") {
		return nil, errors.New("SMTP host is required")
	}
	if config.Port < 1 || config.Port > 65535 {
		return nil, errors.New("SMTP port is invalid")
	}
	fromAddress, err := mail.ParseAddress(config.FromAddress)
	if err != nil || fromAddress.Address != config.FromAddress {
		return nil, errors.New("SMTP from address is invalid")
	}
	if strings.ContainsAny(config.FromName, "\r\n") {
		return nil, errors.New("SMTP from name is invalid")
	}
	if (config.Username == "") != (config.Password == "") {
		return nil, errors.New("SMTP username and password must be set together")
	}
	if config.Timeout <= 0 || config.Timeout > time.Minute {
		return nil, errors.New("SMTP timeout must be no longer than one minute")
	}
	options := []gomail.Option{
		gomail.WithPort(config.Port),
		gomail.WithTimeout(config.Timeout),
		gomail.WithTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12}),
	}
	switch config.TLSMode {
	case TLSModeStartTLS:
		options = append(options, gomail.WithTLSPolicy(gomail.TLSMandatory))
	case TLSModeTLS:
		options = append(options, gomail.WithSSL())
	case TLSModeNone:
		options = append(options, gomail.WithTLSPolicy(gomail.NoTLS))
	default:
		return nil, fmt.Errorf("unsupported SMTP TLS mode %q", config.TLSMode)
	}
	if config.Username != "" {
		if config.TLSMode == TLSModeNone {
			return nil, errors.New("SMTP authentication requires TLS")
		}
		options = append(
			options,
			gomail.WithSMTPAuth(gomail.SMTPAuthAutoDiscover),
			gomail.WithUsername(config.Username),
			gomail.WithPassword(config.Password),
		)
	}
	client, err := gomail.NewClient(config.Host, options...)
	if err != nil {
		return nil, fmt.Errorf("create SMTP client: %w", err)
	}
	return &SMTPSender{client: client, fromAddress: config.FromAddress, fromName: config.FromName}, nil
}

func (sender *SMTPSender) Send(ctx context.Context, message Message) error {
	mailMessage := gomail.NewMsg()
	if err := mailMessage.FromFormat(sender.fromName, sender.fromAddress); err != nil {
		return fmt.Errorf("set notification email sender: %w", err)
	}
	if err := mailMessage.To(message.Recipient); err != nil {
		return fmt.Errorf("set notification email recipient: %w", err)
	}
	mailMessage.Subject(message.Subject)
	mailMessage.SetBodyString(gomail.TypeTextPlain, message.Text)
	mailMessage.AddAlternativeString(gomail.TypeTextHTML, message.HTML)
	if err := sender.client.DialAndSendWithContext(ctx, mailMessage); err != nil {
		return errors.New("SMTP server did not accept the notification email")
	}
	return nil
}

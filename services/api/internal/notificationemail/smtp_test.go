package notificationemail

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSMTPSenderDeliversMultipartMessage(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	received := make(chan string, 1)
	serverErrors := make(chan error, 1)
	go serveTestSMTP(listener, received, serverErrors)
	port, err := strconv.Atoi(strings.Split(listener.Addr().String(), ":")[1])
	if err != nil {
		t.Fatal(err)
	}
	sender, err := NewSMTPSender(SMTPConfig{
		Host: "127.0.0.1", Port: port, FromAddress: "notifications@example.com",
		FromName: "LoreHub", TLSMode: TLSModeNone, Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sender.Send(context.Background(), Message{
		Recipient: "alice@example.com", Subject: "[LoreHub] Updated",
		Text: "Plain body", HTML: "<p>HTML body</p>",
	}); err != nil {
		select {
		case serverErr := <-serverErrors:
			t.Fatalf("send email: %v; test SMTP server: %v", err, serverErr)
		default:
			t.Fatal(err)
		}
	}
	select {
	case message := <-received:
		if !strings.Contains(message, "alice@example.com") || !strings.Contains(message, "Plain body") ||
			!strings.Contains(message, "HTML body") || !strings.Contains(message, "multipart/alternative") {
			t.Fatalf("unexpected SMTP message:\n%s", message)
		}
	case err := <-serverErrors:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("SMTP server did not receive a message")
	}
}

func serveTestSMTP(listener net.Listener, received chan<- string, serverErrors chan<- error) {
	connection, err := listener.Accept()
	if err != nil {
		serverErrors <- err
		return
	}
	defer connection.Close()
	reader := bufio.NewReader(connection)
	writer := bufio.NewWriter(connection)
	writeLine := func(value string) error {
		if _, err := writer.WriteString(value + "\r\n"); err != nil {
			return err
		}
		return writer.Flush()
	}
	if err := writeLine("220 localhost ESMTP"); err != nil {
		serverErrors <- err
		return
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			serverErrors <- err
			return
		}
		command := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(command, "EHLO"):
			if _, err := writer.WriteString("250-localhost\r\n250 8BITMIME\r\n"); err != nil {
				serverErrors <- err
				return
			}
			if err := writer.Flush(); err != nil {
				serverErrors <- err
				return
			}
		case strings.HasPrefix(command, "MAIL FROM"), strings.HasPrefix(command, "RCPT TO"):
			if err := writeLine("250 OK"); err != nil {
				serverErrors <- err
				return
			}
		case command == "NOOP", command == "RSET":
			if err := writeLine("250 OK"); err != nil {
				serverErrors <- err
				return
			}
		case command == "DATA":
			if err := writeLine("354 End data with <CR><LF>.<CR><LF>"); err != nil {
				serverErrors <- err
				return
			}
			var message strings.Builder
			for {
				line, err = reader.ReadString('\n')
				if err != nil {
					serverErrors <- err
					return
				}
				if strings.TrimSpace(line) == "." {
					break
				}
				message.WriteString(line)
			}
			received <- message.String()
			if err := writeLine("250 queued"); err != nil {
				serverErrors <- err
				return
			}
		case command == "QUIT":
			_ = writeLine("221 bye")
			return
		default:
			serverErrors <- fmt.Errorf("unexpected SMTP command %q", command)
			return
		}
	}
}

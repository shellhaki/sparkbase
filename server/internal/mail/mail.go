// Package mail sends email through SMTP, or logs it when SMTP is not configured.
package mail

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Message struct {
	To      string
	Subject string
	Text    string
	// Link is the one action link in the message. The log mailer prints it so
	// an operator without SMTP can still hand it over.
	Link string
}

type Mailer interface {
	Send(ctx context.Context, m Message) error
	// Configured reports whether email really leaves the server.
	Configured() bool
}

// New returns an SMTP mailer for smtpURL, or a log mailer when it is empty.
// smtpURL looks like smtp://user:pass@host:587 or smtps://user:pass@host:465.
func New(smtpURL, from string, log *slog.Logger) (Mailer, error) {
	if smtpURL == "" {
		return &LogMailer{Log: log}, nil
	}
	u, err := url.Parse(smtpURL)
	if err != nil || (u.Scheme != "smtp" && u.Scheme != "smtps") || u.Host == "" {
		return nil, fmt.Errorf("SMTP_URL must look like smtp://user:pass@host:587")
	}
	return &SMTPMailer{url: u, from: from}, nil
}

// LogMailer writes messages to the log. Only used without SMTP.
type LogMailer struct{ Log *slog.Logger }

func (l *LogMailer) Configured() bool { return false }

func (l *LogMailer) Send(_ context.Context, m Message) error {
	// The link is deliberately printed in the message text: without SMTP the
	// operator reads it from the logs. Recipients are masked by the logger.
	l.Log.Warn("email not sent (no SMTP_URL); link: "+m.Link, "to", m.To, "subject", m.Subject)
	return nil
}

type SMTPMailer struct {
	url  *url.URL
	from string
}

func (s *SMTPMailer) Configured() bool { return true }

func (s *SMTPMailer) Send(ctx context.Context, m Message) error {
	host := s.url.Hostname()
	addr := s.url.Host
	if s.url.Port() == "" {
		if s.url.Scheme == "smtps" {
			addr = net.JoinHostPort(host, "465")
		} else {
			addr = net.JoinHostPort(host, "587")
		}
	}
	var auth smtp.Auth
	if s.url.User != nil {
		pass, _ := s.url.User.Password()
		auth = smtp.PlainAuth("", s.url.User.Username(), pass, host)
	}
	fromAddr := s.from
	if i := strings.LastIndex(fromAddr, "<"); i >= 0 {
		fromAddr = strings.TrimSuffix(fromAddr[i+1:], ">")
	}
	body := "From: " + s.from + "\r\n" +
		"To: " + m.To + "\r\n" +
		"Subject: " + m.Subject + "\r\n" +
		"Date: " + time.Now().Format(time.RFC1123Z) + "\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" +
		strings.ReplaceAll(m.Text, "\n", "\r\n")
	done := make(chan error, 1)
	go func() { done <- smtp.SendMail(addr, auth, fromAddr, []string{m.To}, []byte(body)) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Capture records messages, for tests.
type Capture struct {
	mu       sync.Mutex
	Messages []Message
}

func (c *Capture) Configured() bool { return true }

func (c *Capture) Send(_ context.Context, m Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Messages = append(c.Messages, m)
	return nil
}

// Last returns the newest message sent to an address.
func (c *Capture) Last(to string) (Message, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.Messages) - 1; i >= 0; i-- {
		if strings.EqualFold(c.Messages[i].To, to) {
			return c.Messages[i], true
		}
	}
	return Message{}, false
}

func (c *Capture) Count(to string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, m := range c.Messages {
		if strings.EqualFold(m.To, to) {
			n++
		}
	}
	return n
}

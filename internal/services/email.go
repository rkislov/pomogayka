package services

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"strings"

	"github.com/rkislov/pomogayka/internal/db"
	"github.com/rkislov/pomogayka/internal/models"
)

type EmailNotifier struct {
	Store *db.Store
}

func (e *EmailNotifier) NotifyTicketAuthor(ticket *models.Ticket, text string) {
	if e == nil || e.Store == nil || ticket == nil || ticket.Author == nil {
		return
	}
	if ticket.Author.Email == "" {
		return
	}
	subject := "Помогайка: заявка #" + ticket.Number
	body := text
	if tmpl, err := e.Store.GetTemplateByEvent(ticket.TenantID, "comment_public"); err == nil {
		subject, body = RenderTemplate(tmpl.Subject, tmpl.Body, *ticket, ticket.Author, text)
	}
	if err := e.SendTicketMail(ticket, []string{ticket.Author.Email}, subject, body); err != nil {
		log.Printf("email notify: %v", err)
	}
}

func (e *EmailNotifier) SendTicketMail(ticket *models.Ticket, recipients []string, subject, body string) error {
	if ticket == nil {
		return fmt.Errorf("nil ticket")
	}
	mb, err := e.Store.GetMailboxForTicket(ticket.TenantID, ticket.QueueID)
	if err != nil || !mb.IsReady() {
		return fmt.Errorf("no active mailbox for tenant")
	}
	return SendSMTP(mb, recipients, subject, body)
}

func SendSMTP(mb *models.EmailMailbox, recipients []string, subject, body string) error {
	if mb == nil || !mb.IsReady() {
		return fmt.Errorf("mailbox not ready")
	}
	var to []string
	for _, r := range recipients {
		r = strings.TrimSpace(r)
		if r != "" {
			to = append(to, r)
		}
	}
	if len(to) == 0 {
		return fmt.Errorf("no recipients")
	}

	from := mb.FromEmail
	msg := strings.Join([]string{
		"From: " + from,
		"To: " + strings.Join(to, ", "),
		"Subject: " + sanitizeHeader(subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		body,
	}, "\r\n")

	addr := net.JoinHostPort(mb.SMTPHost, fmt.Sprintf("%d", mb.SMTPPort))
	auth := smtp.PlainAuth("", mb.SMTPUsername, mb.SMTPPassword, mb.SMTPHost)

	if mb.SMTPUseTLS {
		tlsConfig := &tls.Config{ServerName: mb.SMTPHost} //nolint:gosec
		conn, err := tls.Dial("tcp", addr, tlsConfig)
		if err != nil {
			// STARTTLS fallback
			return sendStartTLS(addr, mb, auth, from, to, []byte(msg))
		}
		c, err := smtp.NewClient(conn, mb.SMTPHost)
		if err != nil {
			return err
		}
		defer c.Close()
		if mb.SMTPUsername != "" {
			if err := c.Auth(auth); err != nil {
				return err
			}
		}
		if err := c.Mail(from); err != nil {
			return err
		}
		for _, rcpt := range to {
			if err := c.Rcpt(rcpt); err != nil {
				return err
			}
		}
		w, err := c.Data()
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte(msg)); err != nil {
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}
		return c.Quit()
	}
	return smtp.SendMail(addr, auth, from, to, []byte(msg))
}

func sendStartTLS(addr string, mb *models.EmailMailbox, auth smtp.Auth, from string, to []string, msg []byte) error {
	c, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: mb.SMTPHost}); err != nil { //nolint:gosec
			return err
		}
	}
	if mb.SMTPUsername != "" {
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func sanitizeHeader(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

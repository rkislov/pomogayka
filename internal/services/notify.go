package services

import (
	"html"
	"regexp"
	"strings"

	"github.com/rkislov/pomogayka/internal/models"
)

var tagRe = regexp.MustCompile(`(?s)<[^>]*>`)
var spaceRe = regexp.MustCompile(`[ \t]+\n`)
var nlRe = regexp.MustCompile(`\n{3,}`)
var placeholderRe = regexp.MustCompile(`\{\{\s*([^}]+)\s*\}\}`)

func PlainDescription(raw string) string {
	text := tagRe.ReplaceAllString(raw, "")
	text = html.UnescapeString(text)
	text = spaceRe.ReplaceAllString(text, "\n")
	return strings.TrimSpace(nlRe.ReplaceAllString(text, "\n\n"))
}

func RenderTemplate(subject, body string, ticket models.Ticket, recipient *models.User, commentText string) (string, string) {
	values := map[string]string{
		"ticket.id":            ticket.Number,
		"ticket.number":        ticket.Number,
		"ticket.title":         ticket.Title,
		"ticket.description":   PlainDescription(ticket.Description),
		"ticket.appeal_text":   PlainDescription(ticket.Description),
		"ticket.status":        ticket.Status.Label(),
		"ticket.priority":      ticket.Priority.Label(),
		"ticket.queue":         "",
		"author.full_name":     "",
		"assignee.full_name":   "",
		"recipient.full_name":  "",
		"comment.text":         commentText,
	}
	if ticket.Queue != nil {
		values["ticket.queue"] = ticket.Queue.Name
	}
	if ticket.Author != nil {
		values["author.full_name"] = ticket.Author.FullName
	}
	if ticket.Assignee != nil {
		values["assignee.full_name"] = ticket.Assignee.FullName
	}
	if recipient != nil {
		values["recipient.full_name"] = recipient.FullName
	}
	replace := func(s string) string {
		return placeholderRe.ReplaceAllStringFunc(s, func(m string) string {
			key := strings.TrimSpace(placeholderRe.FindStringSubmatch(m)[1])
			if v, ok := values[key]; ok {
				return v
			}
			return ""
		})
	}
	return replace(subject), replace(body)
}

package bot

import "github.com/rkislov/pomogayka/internal/models"

type TicketNotifier interface {
	NotifyTicketAuthor(ticket *models.Ticket, text string)
}

type MultiNotifier struct {
	Notifiers []TicketNotifier
}

func (m *MultiNotifier) NotifyTicketAuthor(ticket *models.Ticket, text string) {
	for _, n := range m.Notifiers {
		if n != nil {
			n.NotifyTicketAuthor(ticket, text)
		}
	}
}

func (m *MultiNotifier) Add(n TicketNotifier) {
	if n != nil {
		m.Notifiers = append(m.Notifiers, n)
	}
}

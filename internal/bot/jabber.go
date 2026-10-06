package bot

import (
	"crypto/tls"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/xmppo/go-xmpp"

	"github.com/rkislov/pomogayka/internal/auth"
	"github.com/rkislov/pomogayka/internal/config"
	"github.com/rkislov/pomogayka/internal/db"
	"github.com/rkislov/pomogayka/internal/models"
)

type JabberBot struct {
	cfg    config.Config
	store  *db.Store
	name   string
	client *xmpp.Client

	mu    sync.Mutex
	state map[string]string // bare JID -> pending action
}

func NewJabber(cfg config.Config, store *db.Store, appName string) (*JabberBot, error) {
	if cfg.JabberJID == "" || cfg.JabberPassword == "" {
		return nil, fmt.Errorf("JABBER_JID and JABBER_PASSWORD are required")
	}
	b := &JabberBot{
		cfg:   cfg,
		store: store,
		name:  appName,
		state: map[string]string{},
	}
	if err := b.connect(); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *JabberBot) connect() error {
	opts := xmpp.Options{
		Host:                         b.cfg.JabberHost,
		User:                         b.cfg.JabberJID,
		Password:                     b.cfg.JabberPassword,
		Resource:                     "pomogayka-bot",
		NoTLS:                        b.cfg.JabberNoTLS,
		StartTLS:                     b.cfg.JabberStartTLS,
		InsecureAllowUnencryptedAuth: b.cfg.JabberNoTLS,
		Session:                      true,
		Status:                       "chat",
		StatusMessage:                b.name + " bot",
	}
	if b.cfg.JabberInsecureTLS {
		serverName := ""
		if at := strings.Index(b.cfg.JabberJID, "@"); at >= 0 {
			serverName = b.cfg.JabberJID[at+1:]
		}
		opts.TLSConfig = &tls.Config{InsecureSkipVerify: true, ServerName: serverName} //nolint:gosec
	}
	client, err := opts.NewClient()
	if err != nil {
		return err
	}
	b.mu.Lock()
	if b.client != nil {
		_ = b.client.Close()
	}
	b.client = client
	b.mu.Unlock()
	log.Printf("Jabber bot connected as %s", b.cfg.JabberJID)
	return nil
}

func (b *JabberBot) Start() {
	for {
		b.mu.Lock()
		client := b.client
		b.mu.Unlock()
		if client == nil {
			if err := b.connect(); err != nil {
				log.Printf("jabber reconnect failed: %v", err)
				time.Sleep(5 * time.Second)
				continue
			}
			continue
		}
		stanza, err := client.Recv()
		if err != nil {
			log.Printf("jabber recv error: %v", err)
			_ = client.Close()
			b.mu.Lock()
			b.client = nil
			b.mu.Unlock()
			time.Sleep(3 * time.Second)
			continue
		}
		switch v := stanza.(type) {
		case xmpp.Chat:
			if v.Text == "" || v.Remote == "" {
				continue
			}
			// Ignore groupchat noise / empty types that aren't chat
			if v.Type != "" && v.Type != "chat" && v.Type != "normal" {
				continue
			}
			go b.handleMessage(bareJID(v.Remote), strings.TrimSpace(v.Text))
		case xmpp.Presence:
			// auto-approve subscriptions so users can message the bot
			if v.Type == "subscribe" && v.From != "" {
				client.ApproveSubscription(bareJID(v.From))
				client.RequestSubscription(bareJID(v.From))
			}
		}
	}
}

func (b *JabberBot) NotifyUser(jid, text string) {
	jid = bareJID(jid)
	if jid == "" {
		return
	}
	b.mu.Lock()
	client := b.client
	b.mu.Unlock()
	if client == nil {
		return
	}
	if _, err := client.Send(xmpp.Chat{Remote: jid, Type: "chat", Text: stripHTML(text)}); err != nil {
		log.Printf("jabber notify error: %v", err)
	}
}

func (b *JabberBot) NotifyTicketAuthor(ticket *models.Ticket, text string) {
	if ticket == nil || ticket.AuthorID == "" {
		return
	}
	user, err := b.store.GetUserByID(ticket.AuthorID)
	if err != nil || user.JabberJID == "" {
		return
	}
	b.NotifyUser(user.JabberJID, text)
}

func (b *JabberBot) handleMessage(fromJID, text string) {
	if text == "" {
		return
	}
	if strings.HasPrefix(text, "/") {
		b.clearState(fromJID)
		parts := strings.Fields(text)
		cmd := strings.ToLower(parts[0])
		args := strings.TrimSpace(strings.TrimPrefix(text, parts[0]))
		switch cmd {
		case "/start", "/help":
			b.reply(fromJID, helpText(b.name, "Jabber"))
		case "/link":
			b.cmdLink(fromJID, args)
		case "/unlink":
			b.cmdUnlink(fromJID)
		case "/new":
			b.cmdNew(fromJID, args)
		case "/list":
			b.cmdList(fromJID)
		case "/ticket":
			b.cmdTicket(fromJID, args)
		case "/comment":
			b.cmdComment(fromJID, args)
		case "/cancel":
			b.reply(fromJID, "Ок, отменено.")
		default:
			b.reply(fromJID, "Неизвестная команда. /help — список команд.")
		}
		return
	}

	state := b.getState(fromJID)
	switch {
	case state == "new":
		b.createFromText(fromJID, text)
	case strings.HasPrefix(state, "comment:"):
		b.addComment(fromJID, strings.TrimPrefix(state, "comment:"), text)
	default:
		b.reply(fromJID, "Напишите /help, чтобы увидеть доступные команды.")
	}
}

func (b *JabberBot) cmdLink(fromJID, args string) {
	parts := strings.Fields(args)
	if len(parts) < 2 {
		b.reply(fromJID, "Формат: /link email@company.ru ваш_пароль")
		return
	}
	email := parts[0]
	password := strings.Join(parts[1:], " ")
	user, err := b.store.GetUserByEmail(email)
	if err != nil || !user.IsActive || !auth.CheckPassword(user.PasswordHash, password) {
		b.reply(fromJID, "Не удалось привязать: проверьте email и пароль.")
		return
	}
	if err := b.store.LinkJabber(user.ID, fromJID); err != nil {
		b.reply(fromJID, "Ошибка привязки: "+err.Error())
		return
	}
	b.reply(fromJID, fmt.Sprintf("Готово, %s. Jabber привязан к %s.\nТеперь можно создавать заявки: /new", user.FullName, user.Email))
}

func (b *JabberBot) cmdUnlink(fromJID string) {
	user, err := b.store.GetUserByJabberJID(fromJID)
	if err != nil {
		b.reply(fromJID, "Аккаунт не привязан.")
		return
	}
	_ = b.store.UnlinkJabber(user.ID)
	b.reply(fromJID, "Jabber отвязан от аккаунта.")
}

func (b *JabberBot) requireUser(fromJID string) *models.User {
	user, err := b.store.GetUserByJabberJID(fromJID)
	if err != nil || !user.IsActive {
		b.reply(fromJID, "Сначала привяжите аккаунт: /link email пароль")
		return nil
	}
	return user
}

func (b *JabberBot) cmdNew(fromJID, args string) {
	if b.requireUser(fromJID) == nil {
		return
	}
	if args == "" {
		b.setState(fromJID, "new")
		b.reply(fromJID, "Опишите заявку одним сообщением.\nФормат: Тема | Описание\nИли просто текст — тема возьмётся из первой строки.")
		return
	}
	b.createFromText(fromJID, args)
}

func (b *JabberBot) createFromText(fromJID, text string) {
	user := b.requireUser(fromJID)
	if user == nil {
		return
	}
	title, description := parseTicketText(text)
	if len(title) < 3 || len(description) < 3 {
		b.reply(fromJID, "Нужны тема и описание (минимум 3 символа).\nПример: Не работает VPN | Не подключается с утра")
		return
	}
	ticket, err := b.store.CreateTicket(user.TenantID, title, description, models.PriorityMedium, user.ID, nil)
	b.clearState(fromJID)
	if err != nil {
		b.reply(fromJID, "Не удалось создать заявку: "+err.Error())
		return
	}
	b.reply(fromJID, fmt.Sprintf("Заявка #%s создана.\n%s\n\n/ticket %s — подробности", ticket.Number, title, ticket.Number))
}

func (b *JabberBot) cmdList(fromJID string) {
	user := b.requireUser(fromJID)
	if user == nil {
		return
	}
	tickets, err := b.store.ListTickets(db.TicketFilter{User: user})
	if err != nil {
		b.reply(fromJID, "Ошибка загрузки заявок.")
		return
	}
	if len(tickets) == 0 {
		b.reply(fromJID, "Заявок пока нет. Создайте: /new")
		return
	}
	if len(tickets) > 15 {
		tickets = tickets[:15]
	}
	var sb strings.Builder
	sb.WriteString("Ваши заявки\n")
	for _, t := range tickets {
		sb.WriteString(fmt.Sprintf("• #%s %s — %s\n", t.Number, t.Title, t.Status.Label()))
	}
	sb.WriteString("\nОткрыть: /ticket P-00001")
	b.reply(fromJID, sb.String())
}

func (b *JabberBot) cmdTicket(fromJID, args string) {
	user := b.requireUser(fromJID)
	if user == nil {
		return
	}
	number := strings.TrimSpace(args)
	if number == "" {
		b.reply(fromJID, "Формат: /ticket P-00001")
		return
	}
	ticket, err := b.store.GetTicketByNumber(strings.TrimPrefix(number, "#"))
	if err != nil {
		b.reply(fromJID, "Заявка не найдена.")
		return
	}
	if !models.CanAccessTicket(user, ticket) {
		b.reply(fromJID, "Нет доступа к этой заявке.")
		return
	}
	assignee := "не назначен"
	if ticket.Assignee != nil {
		assignee = ticket.Assignee.FullName
	}
	queue := "—"
	if ticket.Queue != nil {
		queue = ticket.Queue.Name
	}
	desc := ticket.Description
	if len(desc) > 800 {
		desc = desc[:800] + "…"
	}
	b.reply(fromJID, fmt.Sprintf(
		"#%s %s\nСтатус: %s\nПриоритет: %s\nОчередь: %s\nИсполнитель: %s\n\n%s\n\nОтветить: /comment %s",
		ticket.Number, ticket.Title, ticket.Status.Label(), ticket.Priority.Label(), queue, assignee, desc, ticket.Number,
	))
}

func (b *JabberBot) cmdComment(fromJID, args string) {
	user := b.requireUser(fromJID)
	if user == nil {
		return
	}
	parts := strings.SplitN(strings.TrimSpace(args), " ", 2)
	if parts[0] == "" {
		b.reply(fromJID, "Формат:\n/comment P-00001 текст ответа\nили /comment P-00001 и следующим сообщением текст")
		return
	}
	ticket, err := b.store.GetTicketByNumber(strings.TrimPrefix(parts[0], "#"))
	if err != nil {
		b.reply(fromJID, "Заявка не найдена.")
		return
	}
	if !models.CanAccessTicket(user, ticket) {
		b.reply(fromJID, "Нет доступа к этой заявке.")
		return
	}
	if len(parts) == 1 {
		b.setState(fromJID, "comment:"+ticket.ID)
		b.reply(fromJID, fmt.Sprintf("Напишите текст ответа для #%s", ticket.Number))
		return
	}
	b.addComment(fromJID, ticket.ID, parts[1])
}

func (b *JabberBot) addComment(fromJID, ticketID, body string) {
	user := b.requireUser(fromJID)
	if user == nil {
		return
	}
	body = strings.TrimSpace(body)
	if body == "" {
		b.reply(fromJID, "Пустой комментарий.")
		return
	}
	ticket, err := b.store.GetTicket(ticketID)
	if err != nil {
		b.reply(fromJID, "Заявка не найдена.")
		return
	}
	if !models.CanAccessTicket(user, ticket) {
		b.reply(fromJID, "Нет доступа.")
		return
	}
	if _, err := b.store.AddComment(ticketID, user.ID, body, false); err != nil {
		b.reply(fromJID, "Не удалось добавить комментарий.")
		return
	}
	b.clearState(fromJID)
	b.reply(fromJID, fmt.Sprintf("Ответ добавлен в #%s", ticket.Number))
}

func (b *JabberBot) reply(toJID, text string) {
	b.NotifyUser(toJID, text)
}

func (b *JabberBot) setState(jid, state string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state[jid] = state
}

func (b *JabberBot) getState(jid string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state[jid]
}

func (b *JabberBot) clearState(jid string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.state, jid)
}

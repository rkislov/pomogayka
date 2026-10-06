package bot

import (
	"fmt"
	"log"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/rkislov/pomogayka/internal/auth"
	"github.com/rkislov/pomogayka/internal/db"
	"github.com/rkislov/pomogayka/internal/models"
)

type TelegramBot struct {
	api   *tgbotapi.BotAPI
	store *db.Store
	name  string

	mu    sync.Mutex
	state map[int64]string // chatID -> pending action: "new" or "comment:<ticketID>"
}

func NewTelegram(token string, store *db.Store, appName string) (*TelegramBot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}
	api.Debug = false
	return &TelegramBot{
		api:   api,
		store: store,
		name:  appName,
		state: map[int64]string{},
	}, nil
}

func (b *TelegramBot) Start() {
	log.Printf("Telegram bot @%s started", b.api.Self.UserName)
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30
	updates := b.api.GetUpdatesChan(u)
	for update := range updates {
		if update.Message == nil {
			continue
		}
		go b.handleMessage(update.Message)
	}
}

func (b *TelegramBot) NotifyUser(telegramID int64, text string) {
	msg := tgbotapi.NewMessage(telegramID, text)
	msg.ParseMode = "HTML"
	if _, err := b.api.Send(msg); err != nil {
		log.Printf("telegram notify error: %v", err)
	}
}

func (b *TelegramBot) NotifyTicketAuthor(ticket *models.Ticket, text string) {
	if ticket == nil || ticket.AuthorID == "" {
		return
	}
	user, err := b.store.GetUserByID(ticket.AuthorID)
	if err != nil || user.TelegramID == nil {
		return
	}
	b.NotifyUser(*user.TelegramID, text)
}

func (b *TelegramBot) handleMessage(m *tgbotapi.Message) {
	chatID := m.Chat.ID
	text := strings.TrimSpace(m.Text)
	if text == "" {
		b.reply(chatID, "Отправьте текстовое сообщение.")
		return
	}

	if strings.HasPrefix(text, "/") {
		b.clearState(chatID)
		parts := strings.Fields(text)
		cmd := strings.ToLower(strings.Split(parts[0], "@")[0])
		args := strings.TrimSpace(strings.TrimPrefix(text, parts[0]))
		switch cmd {
		case "/start", "/help":
			b.reply(chatID, b.helpText())
		case "/link":
			b.cmdLink(chatID, args)
		case "/unlink":
			b.cmdUnlink(chatID)
		case "/new":
			b.cmdNew(chatID, args)
		case "/list":
			b.cmdList(chatID)
		case "/ticket":
			b.cmdTicket(chatID, args)
		case "/comment":
			b.cmdComment(chatID, args)
		case "/cancel":
			b.reply(chatID, "Ок, отменено.")
		default:
			b.reply(chatID, "Неизвестная команда. /help — список команд.")
		}
		return
	}

	state := b.getState(chatID)
	switch {
	case state == "new":
		b.createFromText(chatID, text)
	case strings.HasPrefix(state, "comment:"):
		ticketID := strings.TrimPrefix(state, "comment:")
		b.addComment(chatID, ticketID, text)
	default:
		b.reply(chatID, "Напишите /help, чтобы увидеть доступные команды.")
	}
}

func (b *TelegramBot) helpText() string {
	return fmt.Sprintf(`<b>%s</b> — Telegram-бот поддержки

Команды:
/link email пароль — привязать аккаунт
/unlink — отвязать Telegram
/new — создать заявку
/new Тема | Описание — создать сразу
/list — мои заявки
/ticket P-00001 — карточка заявки
/comment P-00001 — ответить в заявку
/cancel — отменить текущий шаг

Сначала выполните /link, затем создавайте заявки.`, b.name)
}

func (b *TelegramBot) cmdLink(chatID int64, args string) {
	parts := strings.Fields(args)
	if len(parts) < 2 {
		b.reply(chatID, "Формат: /link email@company.ru ваш_пароль")
		return
	}
	email := parts[0]
	password := strings.Join(parts[1:], " ")
	user, err := b.store.GetUserByEmail(email)
	if err != nil || !user.IsActive || !auth.CheckPassword(user.PasswordHash, password) {
		b.reply(chatID, "Не удалось привязать: проверьте email и пароль.")
		return
	}
	if err := b.store.LinkTelegram(user.ID, chatID); err != nil {
		b.reply(chatID, "Ошибка привязки: "+err.Error())
		return
	}
	b.reply(chatID, fmt.Sprintf("Готово, %s. Telegram привязан к %s.\nТеперь можно создавать заявки: /new", user.FullName, user.Email))
}

func (b *TelegramBot) cmdUnlink(chatID int64) {
	user, err := b.store.GetUserByTelegramID(chatID)
	if err != nil {
		b.reply(chatID, "Аккаунт не привязан.")
		return
	}
	_ = b.store.UnlinkTelegram(user.ID)
	b.reply(chatID, "Telegram отвязан от аккаунта.")
}

func (b *TelegramBot) requireUser(chatID int64) *models.User {
	user, err := b.store.GetUserByTelegramID(chatID)
	if err != nil || !user.IsActive {
		b.reply(chatID, "Сначала привяжите аккаунт: /link email пароль")
		return nil
	}
	return user
}

func (b *TelegramBot) cmdNew(chatID int64, args string) {
	if b.requireUser(chatID) == nil {
		return
	}
	if args == "" {
		b.setState(chatID, "new")
		b.reply(chatID, "Опишите заявку одним сообщением.\nФормат: <b>Тема | Описание</b>\nИли просто текст — тема возьмётся из первой строки.")
		return
	}
	b.createFromText(chatID, args)
}

func (b *TelegramBot) createFromText(chatID int64, text string) {
	user := b.requireUser(chatID)
	if user == nil {
		return
	}
	title, description := parseTicketText(text)
	if len(title) < 3 || len(description) < 3 {
		b.reply(chatID, "Нужны тема и описание (минимум 3 символа).\nПример: Не работает VPN | Не подключается с утра")
		return
	}
	ticket, err := b.store.CreateTicket(title, description, models.PriorityMedium, user.ID, nil)
	b.clearState(chatID)
	if err != nil {
		b.reply(chatID, "Не удалось создать заявку: "+err.Error())
		return
	}
	b.reply(chatID, fmt.Sprintf("Заявка <b>#%s</b> создана.\n%s\n\n/ticket %s — подробности", ticket.Number, title, ticket.Number))
}

func parseTicketText(text string) (string, string) {
	text = strings.TrimSpace(text)
	if strings.Contains(text, "|") {
		parts := strings.SplitN(text, "|", 2)
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	lines := strings.SplitN(text, "\n", 2)
	title := strings.TrimSpace(lines[0])
	if len(lines) == 1 {
		return title, title
	}
	desc := strings.TrimSpace(lines[1])
	if desc == "" {
		desc = title
	}
	return title, desc
}

func (b *TelegramBot) cmdList(chatID int64) {
	user := b.requireUser(chatID)
	if user == nil {
		return
	}
	tickets, err := b.store.ListTickets(db.TicketFilter{User: user})
	if err != nil {
		b.reply(chatID, "Ошибка загрузки заявок.")
		return
	}
	if len(tickets) == 0 {
		b.reply(chatID, "Заявок пока нет. Создайте: /new")
		return
	}
	if len(tickets) > 15 {
		tickets = tickets[:15]
	}
	var sb strings.Builder
	sb.WriteString("<b>Ваши заявки</b>\n")
	for _, t := range tickets {
		sb.WriteString(fmt.Sprintf("• <b>#%s</b> %s — %s\n", t.Number, t.Title, t.Status.Label()))
	}
	sb.WriteString("\nОткрыть: /ticket P-00001")
	b.reply(chatID, sb.String())
}

func (b *TelegramBot) cmdTicket(chatID int64, args string) {
	user := b.requireUser(chatID)
	if user == nil {
		return
	}
	number := strings.TrimSpace(args)
	if number == "" {
		b.reply(chatID, "Формат: /ticket P-00001")
		return
	}
	ticket, err := b.store.GetTicketByNumber(strings.TrimPrefix(number, "#"))
	if err != nil {
		b.reply(chatID, "Заявка не найдена.")
		return
	}
	if !user.Role.IsStaff() && ticket.AuthorID != user.ID {
		b.reply(chatID, "Нет доступа к этой заявке.")
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
	b.reply(chatID, fmt.Sprintf(
		"<b>#%s</b> %s\nСтатус: %s\nПриоритет: %s\nОчередь: %s\nИсполнитель: %s\n\n%s\n\nОтветить: /comment %s",
		ticket.Number, ticket.Title, ticket.Status.Label(), ticket.Priority.Label(), queue, assignee, escapeHTML(desc), ticket.Number,
	))
}

func (b *TelegramBot) cmdComment(chatID int64, args string) {
	user := b.requireUser(chatID)
	if user == nil {
		return
	}
	parts := strings.SplitN(strings.TrimSpace(args), " ", 2)
	if parts[0] == "" {
		b.reply(chatID, "Формат:\n/comment P-00001 текст ответа\nили /comment P-00001 и следующим сообщением текст")
		return
	}
	ticket, err := b.store.GetTicketByNumber(strings.TrimPrefix(parts[0], "#"))
	if err != nil {
		b.reply(chatID, "Заявка не найдена.")
		return
	}
	if !user.Role.IsStaff() && ticket.AuthorID != user.ID {
		b.reply(chatID, "Нет доступа к этой заявке.")
		return
	}
	if len(parts) == 1 {
		b.setState(chatID, "comment:"+ticket.ID)
		b.reply(chatID, fmt.Sprintf("Напишите текст ответа для #%s", ticket.Number))
		return
	}
	b.addComment(chatID, ticket.ID, parts[1])
}

func (b *TelegramBot) addComment(chatID int64, ticketID, body string) {
	user := b.requireUser(chatID)
	if user == nil {
		return
	}
	body = strings.TrimSpace(body)
	if body == "" {
		b.reply(chatID, "Пустой комментарий.")
		return
	}
	ticket, err := b.store.GetTicket(ticketID)
	if err != nil {
		b.reply(chatID, "Заявка не найдена.")
		return
	}
	if !user.Role.IsStaff() && ticket.AuthorID != user.ID {
		b.reply(chatID, "Нет доступа.")
		return
	}
	if _, err := b.store.AddComment(ticketID, user.ID, body, false); err != nil {
		b.reply(chatID, "Не удалось добавить комментарий.")
		return
	}
	b.clearState(chatID)
	b.reply(chatID, fmt.Sprintf("Ответ добавлен в #%s", ticket.Number))
}

func (b *TelegramBot) reply(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"
	if _, err := b.api.Send(msg); err != nil {
		log.Printf("telegram send error: %v", err)
	}
}

func (b *TelegramBot) setState(chatID int64, state string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state[chatID] = state
}

func (b *TelegramBot) getState(chatID int64) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state[chatID]
}

func (b *TelegramBot) clearState(chatID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.state, chatID)
}

func escapeHTML(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
	)
	return replacer.Replace(s)
}

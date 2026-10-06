package bot

import (
	"regexp"
	"strings"
)

var htmlTagRe = regexp.MustCompile(`(?s)<[^>]*>`)

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

func stripHTML(s string) string {
	s = htmlTagRe.ReplaceAllString(s, "")
	replacer := strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'")
	return strings.TrimSpace(replacer.Replace(s))
}

func bareJID(jid string) string {
	jid = strings.TrimSpace(jid)
	if i := strings.Index(jid, "/"); i >= 0 {
		jid = jid[:i]
	}
	return strings.ToLower(jid)
}

func helpText(appName, channel string) string {
	return appName + " — " + channel + "-бот поддержки\n\n" +
		"Команды:\n" +
		"/link email пароль — привязать аккаунт\n" +
		"/unlink — отвязать " + channel + "\n" +
		"/new — создать заявку\n" +
		"/new Тема | Описание — создать сразу\n" +
		"/list — мои заявки\n" +
		"/ticket P-00001 — карточка заявки\n" +
		"/comment P-00001 — ответить в заявку\n" +
		"/cancel — отменить текущий шаг\n\n" +
		"Сначала выполните /link, затем создавайте заявки."
}

# Помогайка

Тикет-система поддержки на Go: серверные HTML-шаблоны, HTMX и небольшой слой JavaScript.

**Автор:** Роман Сергеевич Кислов  
**Лицензия:** [Apache License 2.0](LICENSE)

## Возможности

- Роли: заявитель (`client`), специалист (`agent`), менеджер (`manager`), администратор (`admin`)
- Мультитенантность: организации (slug), изоляция заявок/очередей/шаблонов
- Менеджер контролирует нескольких специалистов, назначает им заявки
- Заявки: создание, список, фильтры, статусы, приоритеты, очереди
- Переписка с HTMX (без перезагрузки страницы)
- Действия специалиста: взять в работу, ожидание ответа, закрытие
- Админка: пользователи/роли, очереди, шаблоны уведомлений
- Переменные шаблонов: `ticket.number`, `ticket.title`, `ticket.description` / `ticket.appeal_text`, `ticket.status`, `ticket.queue`, `author.full_name`, `assignee.full_name`, `comment.text`
- SQLite из коробки, сессии в cookie
- Telegram-бот: привязка аккаунта, создание заявок, список, ответы и уведомления
- Jabber/XMPP-бот: те же команды и уведомления заявителю

## Быстрый старт

```bash
cp .env.example .env
go run ./cmd/server
```

Откройте http://localhost:8080

Первый администратор:

- Email: `admin@example.com`
- Password: `admin12345`

### Docker

```bash
docker compose up --build
```



## Мультитенантность и менеджеры

- Каждая организация — тенант со своим `slug` (например `default`, `romashka`).
- Пользователи, очереди, заявки и шаблоны уведомлений изолированы по тенанту.
- При регистрации указывается slug организации.
- Роль `manager` видит раздел «Команда», фильтр заявок команды и может назначать заявки своим специалистам.
- В админке администратор назначает специалистам менеджера.

## Telegram-бот

1. Создайте бота через [@BotFather](https://t.me/BotFather) и получите токен.
2. Добавьте в `.env`:

```bash
TELEGRAM_BOT_TOKEN=123456:ABC...
```

3. Перезапустите сервер. Бот работает long polling в том же процессе.

Команды бота:

- `/link email пароль` — привязать аккаунт Помогайки
- `/new Тема | Описание` — создать заявку
- `/list` — мои заявки
- `/ticket P-00001` — карточка заявки
- `/comment P-00001 текст` — публичный ответ
- `/unlink` — отвязать Telegram

Когда специалист отвечает в веб-интерфейсе, заявитель с привязанным Telegram получает уведомление.


## Jabber / XMPP-бот

1. Создайте отдельный XMPP-аккаунт для бота (например `pomogayka-bot@xmpp.example.com`).
2. Добавьте в `.env`:

```bash
JABBER_JID=pomogayka-bot@xmpp.example.com
JABBER_PASSWORD=secret
# при необходимости:
# JABBER_HOST=xmpp.example.com:5222
# JABBER_START_TLS=true
# JABBER_INSECURE_TLS=false
```

3. Перезапустите сервер. Бот подключается как XMPP-клиент в том же процессе.

Команды те же, что у Telegram: `/link`, `/new`, `/list`, `/ticket`, `/comment`, `/unlink`.

Пользователь пишет боту в любом Jabber-клиенте (Pidgin, Gajim, Conversations и т.п.), привязывает аккаунт через `/link email пароль` и работает с заявками. Публичные ответы из веба уходят и в Telegram, и в Jabber (если привязаны).

## Стек

- Go + chi
- `html/template` + embed
- HTMX
- SQLite (`modernc.org/sqlite`)
- scs (сессии)

## Структура

```
cmd/server          — точка входа
internal/           — config, db, auth, handlers, middleware, services, models
web/templates       — HTML-шаблоны
web/static          — CSS и JS
migrations          — SQL-схема
```

## Лицензия

Copyright 2026 Роман Сергеевич Кислов

Licensed under the Apache License, Version 2.0. См. файлы `LICENSE` и `NOTICE`.

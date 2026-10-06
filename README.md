# Помогайка

Тикет-система поддержки на Go: серверные HTML-шаблоны, HTMX и небольшой слой JavaScript.

**Автор:** Роман Сергеевич Кислов  
**Лицензия:** [Apache License 2.0](LICENSE)

## Возможности

- Роли: заявитель (`client`), специалист (`agent`), администратор (`admin`)
- Заявки: создание, список, фильтры, статусы, приоритеты, очереди
- Переписка с HTMX (без перезагрузки страницы)
- Действия специалиста: взять в работу, ожидание ответа, закрытие
- Админка: пользователи/роли, очереди, шаблоны уведомлений
- Переменные шаблонов: `ticket.number`, `ticket.title`, `ticket.description` / `ticket.appeal_text`, `ticket.status`, `ticket.queue`, `author.full_name`, `assignee.full_name`, `comment.text`
- SQLite из коробки, сессии в cookie
- Telegram-бот: привязка аккаунта, создание заявок, список, ответы и уведомления

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

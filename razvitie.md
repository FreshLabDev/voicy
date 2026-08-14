# Voicy — развитие и текущий контракт

Публичное имя: **Voicy** (репозиторий [FreshLabDev/voicy](https://github.com/FreshLabDev/voicy)).
Внутренний ключ экосистемы остаётся `voicetotext` (схема, роль `voicetotext_core`). Стек — Go, своя HTTP-обёртка Telegram, STT — только Deepgram.

Этот файл — живой план слайсов **и** зафиксированные продуктовые решения. Реализация идёт по ним, не по старой версии документа.

---

## 1. Продукт

**DM.** Пользователь кидает войс (`voice`) или кружочек (`video_note`). Бот стримит аудио в Deepgram Live, параллельно рисует черновик в Telegram (`sendMessageDraft`), затем присылает финальный текст.

**Группа.** Голый войс игнорируется (тишина, privacy mode ON). Явный запрос:

| Команда | Кому виден текст |
|---|---|
| `/v` реплаем на войс/кружок | всем в чате / топике |
| `/vp` реплаем на войс/кружок | только запросившему: команда **эфемерная**, сразу placeholder в 15 с, потом `editEphemeralMessageText` |

`/start` в группе — эфемерный, как у Branchy. Настройки и статистика живут в DM.

**Кэш.** Ключ — Telegram `file_id`. Храним `file_id` + текст + метаданные. Байты аудио не храним: файл остаётся у Telegram, повтор того же `file_id` не ходит в Deepgram.

**Статистика.** В пользовательские агрегаты попадают только успешные непустые транскрипты. Пустые и ошибки — только в логи. Серий (streak) нет.

---

## 2. Экосистема core

Identity — только `core.touch` / `core.set_language` / `core.effective_language`. Своих `users`/`chats` нет.

Локальный compose поднимает урезанный `deploy/core-init.sql`. Живая миграция `009` в соседнем `core` — отдельный релиз, не блокер этого репо.

Один пул, `search_path=voicetotext`, вызовы `core.*` schema-qualified. Перед INSERT джобы с FK на `core.person` — синхронный `touch`.

---

## 3. Стиль и поведение

- Тихо в группах без `/v` или `/vp`.
- Команда только голая или `@этотбот`. `@другойбот` игнор.
- Unknown command — подсказка только в DM.
- UI: HTML, `html.EscapeString`, без preview ссылок.
- Короткий sentence case. Панель `/start` правится на месте (Language / Stats / Help / About / Close).
- Ошибка пользователю без сырых Telegram/Deepgram строк и без токенов.
- Не логируем токен, Deepgram key, полный Bot API URL, сырые байты. Пустой/упавший прогон логируем с `file_id`, `update_id`, кодом ошибки — без полного аудио.

---

## 4. Своя Telegram-обёртка

Не копируем `branchy/internal/telegram` as-is и не берём `go-telegram`. Пишем свой пакет в этом репо:

- long poll `getUpdates` (`message`, `callback_query`, `my_chat_member`)
- `sendMessage` / `editMessageText` HTML
- `sendMessageDraft` (`draft_id` + растущий текст) — best-effort; в группах часто игнорируется, финал всё равно уходит
- `sendMessage` с `receiver_user_id` для `/vp`
- ephemeral `/start` (`is_ephemeral`, `ephemeral_message_id`)
- `getFile` + скачивание `https://api.telegram.org/file/bot<token>/<file_path>` (не отдаём этот URL в Deepgram)
- типы `Voice`, `VideoNote`, `ReplyToMessage`, `MessageThreadID`, `LanguageCode`
- `sendChatAction(typing)`
- GET-ретраи 429/5xx; POST — повтор только на `retry_after`
- токен в ошибках затирается

---

## 5. Deepgram

Основной путь — **streaming**:

```
wss://api.deepgram.com/v1/listen?model=nova-3&language=multi&smart_format=true&interim_results=true&mip_opt_out=true
Authorization: Token <key>
```

Байты скачанного файла уходят чанками, затем `{type:Finalize}` и `{type:CloseStream}`. Сообщения `type=Results`: `channel.alternatives[0].transcript`, `is_final`. Interim склеиваются для draft; финал — склейка `is_final`.

Запас — prererecorded `POST https://api.deepgram.com/v1/listen` бинарным телом (`Authorization: Token`, не URL Telegram-файла), если WS не поднялся или отвалился.

`mip_opt_out=true` всегда. Транскрипт Deepgram не хранит — храним сами.

---

## 6. Кэш и БД (`voicetotext.*`)

| Таблица | Назначение |
|---|---|
| `transcripts` | PK `file_id`, текст, kind, language, confidence, duration, request_id |
| `jobs` | идемпотентность `telegram_update_id`, статус `received/sent/empty/failed`, `cache_hit` |
| `user_stats` | lifetime только успешных непустых |
| `runtime_state` | offset long poll |
| `schema_migrations` | ledger |

Пустой/failed **не** пишем в `transcripts` (чтобы можно было повторить) и **не** трогаем `user_stats`.

Retention сырых job/текст — позже (`TRANSCRIPT_RETENTION`); кэш по `file_id` живёт, пока ряд не истёк.

---

## 7. Слайсы реализации

| Слайс | Что | Статус |
|---|---|---|
| 0 | Контракт (этот файл) | сейчас |
| 1 | Скелет Go, Docker, git, `.env` вне git | сейчас |
| 2 | Обёртка Telegram + Deepgram stream/listen | сейчас |
| 3 | Схема, кэш `file_id`, stats без empty/fail | сейчас |
| 4 | Бот: DM implicit, группа `/v` `/vp`, draft + финал | сейчас |
| 5 | Меню `/start`, язык, help, stats UI | сейчас (минимум) |
| 6 | Retention, delete, админский auto, диаризация | позже |

---

## 8. Как тестируем

Чистые `decide` / extract / `ShouldCount` / format — таблицами. HTTP-клиенты — `httptest`. Стриминг — реальный `Stream` с подставным dial: URL `wss://api.deepgram.com/v1/listen` и заголовок `Authorization: Token`. Кэш-hit — поставленный `transcribe` не вызывает Listen. Живой Telegram/Deepgram в CI не обязателен.

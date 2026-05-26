# Control API (multi-user)

HTTP API для управления пользователями и генерации `tg://proxy` ссылок. Формат ответов и маршрутов ориентирован на [telemt Control API](https://github.com/telemt/telemt/blob/main/docs/API.md).

Control-plane только: трафик MTProto через API не идёт.

## Секреты пользователей

**Да — у каждого пользователя свой уникальный secret.**

| Ситуация | Поведение |
| --- | --- |
| `POST /v1/users` **без** поля `secret` | Генерируется новый **ee-secret** (случайные 16 байт ключа + hostname из `users.toml` → `[general] default-host`). У разных пользователей ключи всегда разные. |
| `POST /v1/users` **с** полем `secret` | Используется переданный ee-secret (base64 или hex). Должен быть валидным для mtg (префикс `ee`, FakeTLS). |
| `secret` в основном `config.toml` | Шаблон для domain fronting / bootstrap. В multi-user режиме **не** используется как общий пароль для клиентов — только секреты из `users.toml`. |
| Истёкший пользователь | `expiration_rfc3339` / `expires_at` в прошлом → secret перестаёт приниматься на handshake, в API `expired: true`. |
| `DELETE /v1/users/{username}` | Пользователь и secret удаляются из `users.toml` безвозвратно. |

Секрет в ответе `POST` — поле `data.secret` (дублирует secret внутри `tg://` ссылки в `data.user.links.tls`).

Формат секрета mtg (не 32 hex как в classic telemt):

```text
ee + 16 байт ключа + hostname (FakeTLS / domain fronting)
```

Пример генерации вручную:

```bash
mtg generate-secret your-domain.example
```

---

## Включение в config.toml

```toml
users-file = "/opt/mtg/users.toml"

[api]
enabled = true
listen = "127.0.0.1:9092"          # только loopback; снаружи — через nginx
whitelist = ["127.0.0.1/32", "::1/128"]
auth-header = "Bearer YOUR_TOKEN"   # точное значение заголовка Authorization
read-only = false
```

Публичный HTTPS (nginx + Let's Encrypt) описан в [deploy/README.md](../deploy/README.md).

---

## Быстрые примеры curl

Переменные (из [deploy/.env](../deploy/.env) после деплоя):

```bash
source deploy/.env

export API_URL="https://${MTG_DOMAIN}:${MTG_API_PUBLIC_PORT}"
# Authorization — точная строка из MTG_API_AUTH_HEADER (уже с "Bearer ...")
export AUTH="$MTG_API_AUTH_HEADER"
```

### 1. Создать пользователя и получить ссылку

```bash
curl -sk -X POST "$API_URL/v1/users" \
  -H "Authorization: $AUTH" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "alice",
    "expires_at": "2026-12-31T23:59:59Z"
  }'
```

Только ссылка `tg://proxy` (нужен [jq](https://jqlang.org/)):

```bash
curl -sk -X POST "$API_URL/v1/users" \
  -H "Authorization: $AUTH" \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","expires_at":"2026-12-31T23:59:59Z"}' \
  | jq -r '.data.user.links.tls[0]'
```

Только ee-secret:

```bash
curl -sk -X POST "$API_URL/v1/users" \
  -H "Authorization: $AUTH" \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","expires_at":"2026-12-31T23:59:59Z"}' \
  | jq -r '.data.secret'
```

Пример ответа:

```json
{
  "ok": true,
  "data": {
    "user": {
      "username": "alice",
      "expiration_rfc3339": "2026-12-31T23:59:59Z",
      "links": {
        "tls": ["tg://proxy?server=stun06-ge.zxcv.best&port=3129&secret=..."]
      },
      "expired": false
    },
    "secret": "..."
  },
  "revision": "..."
}
```

### 2. Получить ссылку существующего пользователя

```bash
curl -sk "$API_URL/v1/users/alice" \
  -H "Authorization: $AUTH"
```

Только ссылка:

```bash
curl -sk "$API_URL/v1/users/alice" \
  -H "Authorization: $AUTH" \
  | jq -r '.data.links.tls[0]'
```

Проверить, не истёк ли срок:

```bash
curl -sk "$API_URL/v1/users/alice" \
  -H "Authorization: $AUTH" \
  | jq '{username: .data.username, expired: .data.expired, expires: .data.expiration_rfc3339, link: .data.links.tls[0]}'
```

### 3. Удалить пользователя (revoke)

Ссылка и secret перестают работать сразу после успешного ответа.

```bash
curl -sk -X DELETE "$API_URL/v1/users/alice" \
  -H "Authorization: $AUTH"
```

Пример ответа:

```json
{
  "ok": true,
  "data": "alice",
  "revision": "..."
}
```

### Полный цикл (создать → прочитать → удалить)

```bash
source deploy/.env
API_URL="https://${MTG_DOMAIN}:${MTG_API_PUBLIC_PORT}"
AUTH="$MTG_API_AUTH_HEADER"

# создать
curl -sk -X POST "$API_URL/v1/users" \
  -H "Authorization: $AUTH" \
  -H "Content-Type: application/json" \
  -d '{"username":"demo","expires_at":"2026-12-31T23:59:59Z"}' \
  | jq -r '.data.user.links.tls[0]'

# получить
curl -sk "$API_URL/v1/users/demo" -H "Authorization: $AUTH" | jq .

# удалить
curl -sk -X DELETE "$API_URL/v1/users/demo" -H "Authorization: $AUTH" | jq .

# после удаления — 404
curl -sk "$API_URL/v1/users/demo" -H "Authorization: $AUTH" | jq .
```

### https://t.me/proxy

Из `tg://` ссылки:

```bash
LINK=$(curl -sk "$API_URL/v1/users/alice" -H "Authorization: $AUTH" | jq -r '.data.links.tls[0]')
QUERY="${LINK#*?}"
echo "https://t.me/proxy?${QUERY}"
```

---

## Протокол

| Параметр | Значение |
| --- | --- |
| Transport | HTTP/1.1 |
| Content-Type | `application/json; charset=utf-8` |
| Prefix | `/v1` |
| Authorization | Заголовок `Authorization` = значение `auth-header` из конфига (целиком, например `Bearer …`) |
| Optimistic locking | Опционально `If-Match: <revision>` на мутирующих запросах (`revision` из успешного ответа) |

### Успешный ответ

```json
{
  "ok": true,
  "data": { },
  "revision": "sha256-hex-of-users.toml"
}
```

### Ошибка

```json
{
  "ok": false,
  "error": {
    "code": "machine_code",
    "message": "human-readable"
  },
  "request_id": 1,
  "revision": "sha256-hex"
}
```

### Коды ошибок

| HTTP | `error.code` | Когда |
| --- | --- | --- |
| `400` | `bad_request` | Невалидный JSON, username, secret, дата |
| `401` | `unauthorized` | Нет или неверный `Authorization` |
| `403` | `forbidden` | IP не в `whitelist` (прямой доступ к loopback API) |
| `403` | `read_only` | `read-only = true`, мутация запрещена |
| `404` | `not_found` | Нет маршрута или пользователя |
| `409` | `user_exists` | Повторное создание username |
| `409` | (в теле) revision conflict | Неверный `If-Match` |
| `503` | `api_disabled` | Нет `users-file` / store |

---

## Эндпоинты

| Method | Path | Body | Success | `data` |
| --- | --- | --- | --- | --- |
| `GET` | `/v1/health` | — | `200` | `{ "status": "ok", "read_only": bool }` |
| `POST` | `/v1/users` | `CreateUserRequest` | `201` | `CreateUserResponse` |
| `GET` | `/v1/users/{username}` | — | `200` | `UserInfo` |
| `DELETE` | `/v1/users/{username}` | — | `200` | `string` (username) |

> Список всех пользователей (`GET /v1/users`) пока не реализован — только по одному username.

---

## `CreateUserRequest` — `POST /v1/users`

| Поле | Тип | Обязательно | Описание |
| --- | --- | --- | --- |
| `username` | string | да | `[A-Za-z0-9_.-]`, длина 1–64 |
| `secret` | string | нет | ee-secret (base64/hex). Если пусто — автогенерация |
| `expiration_rfc3339` | string | нет | Срок действия, RFC3339 (например `2026-12-31T23:59:59Z`) |
| `expires_at` | string | нет | Алиас для `expiration_rfc3339` |

### Пример: создать пользователя с автогенерацией secret

```bash
export API_URL="https://proxy.example.com:8445"
export AUTH='Bearer YOUR_TOKEN'

curl -sk -X POST "$API_URL/v1/users" \
  -H "Authorization: $AUTH" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "alice",
    "expires_at": "2026-12-31T23:59:59Z"
  }'
```

**Ответ `201`:**

```json
{
  "ok": true,
  "data": {
    "user": {
      "username": "alice",
      "expiration_rfc3339": "2026-12-31T23:59:59Z",
      "current_connections": 0,
      "active_unique_ips": 0,
      "total_octets": 0,
      "links": {
        "tls": [
          "tg://proxy?port=3129&secret=7qgQLS0-...&server=proxy.example.com"
        ]
      },
      "expired": false
    },
    "secret": "7qgQLS0-..."
  },
  "revision": "abc123..."
}
```

Ссылку для Telegram: `data.user.links.tls[0]` или собрать из `server`/`port`/`secret`.

### Пример: свой secret

```bash
SECRET=$(mtg generate-secret proxy.example.com)

curl -sk -X POST "$API_URL/v1/users" \
  -H "Authorization: $AUTH" \
  -H "Content-Type: application/json" \
  -d "{
    \"username\": \"bob\",
    \"secret\": \"$SECRET\",
    \"expiration_rfc3339\": \"2027-06-01T00:00:00Z\"
  }"
```

### Пример: конфликт username

```bash
# второй раз тот же username → 409
curl -sk -X POST "$API_URL/v1/users" \
  -H "Authorization: $AUTH" \
  -H "Content-Type: application/json" \
  -d '{"username":"alice"}'
```

```json
{
  "ok": false,
  "error": {
    "code": "user_exists",
    "message": "user already exists"
  },
  "request_id": 2
}
```

### Optimistic locking при создании

```bash
REV=$(curl -sk "$API_URL/v1/health" -H "Authorization: $AUTH" | jq -r .revision)

curl -sk -X POST "$API_URL/v1/users" \
  -H "Authorization: $AUTH" \
  -H "If-Match: $REV" \
  -H "Content-Type: application/json" \
  -d '{"username":"carol","expires_at":"2026-01-01T00:00:00Z"}'
```

---

## `UserInfo` — `GET /v1/users/{username}`

| Поле | Тип | Описание |
| --- | --- | --- |
| `username` | string | Имя пользователя |
| `expiration_rfc3339` | string? | Срок действия |
| `current_connections` | number | Зарезервировано (сейчас всегда `0`) |
| `active_unique_ips` | number | Зарезервировано (сейчас всегда `0`) |
| `total_octets` | number | Зарезервировано (сейчас всегда `0`) |
| `links.tls` | string[] | Активные `tg://proxy` (ee / FakeTLS) |
| `expired` | bool | `true`, если срок истёк |

### Пример

```bash
curl -sk "$API_URL/v1/users/alice" \
  -H "Authorization: $AUTH"
```

**Ответ `200`:** объект `UserInfo` в `data` (без отдельного поля `secret` — только внутри `links.tls`).

### Пример: пользователь не найден

```bash
curl -sk "$API_URL/v1/users/nobody" -H "Authorization: $AUTH"
# → 404, error.code = "not_found"
```

---

## Revoke — `DELETE /v1/users/{username}`

Удаляет запись из `users.toml`. Прокси перестаёт принимать этот secret сразу после записи файла.

### Пример

```bash
curl -sk -X DELETE "$API_URL/v1/users/alice" \
  -H "Authorization: $AUTH"
```

**Ответ `200`:**

```json
{
  "ok": true,
  "data": "alice",
  "revision": "new-sha256..."
}
```

### С If-Match

```bash
REV="..."  # revision из GET или предыдущего POST

curl -sk -X DELETE "$API_URL/v1/users/alice" \
  -H "Authorization: $AUTH" \
  -H "If-Match: $REV"
```

---

## `GET /v1/health`

Проверка API и режима read-only.

```bash
curl -sk "$API_URL/v1/health" -H "Authorization: $AUTH"
```

```json
{
  "ok": true,
  "data": {
    "status": "ok",
    "read_only": false
  },
  "revision": "..."
}
```

Без заголовка `Authorization` (если `auth-header` задан): `401 unauthorized`.

---

## users.toml (файл на диске)

Синхронизируется с API. Пример:

```toml
[general]
default-host = "proxy.example.com"

[general.links]
public-host = "proxy.example.com"
public-port = 3129

[[users]]
username = "alice"
secret = "ee..."
expiration-rfc3339 = "2026-12-31T23:59:59Z"
```

Поля `links` задают `server` и `port` в `tg://proxy`. Hostname внутри secret (`default-host`) — для FakeTLS/SNI.

---

## Безопасность

- API на `127.0.0.1` + nginx TLS снаружи — типичная схема.
- `auth-header` обязателен в проде (длинный случайный токен).
- Не публикуй `deploy/.env` и `/opt/mtg/deploy.env`.
- `whitelist` на loopback API: с интернета до процесса mtg достучится только nginx на `127.0.0.1`.

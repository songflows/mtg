# Деплой mtg + Users API на VPS

Пошаговая инструкция: бинарник mtg с multi-user API, nginx + Let's Encrypt, firewall.

См. также [docs/API.md](../docs/API.md) — описание всех методов API.

---

## Что получится

| Компонент | Пример |
| --- | --- |
| MTProto proxy | `your-domain.example:3129` |
| Control API (HTTPS) | `https://your-domain.example:8445/v1/...` |
| Пользователи | У каждого свой ee-secret, срок via API |
| Файлы на сервере | `/opt/mtg/config.toml`, `users.toml`, `mtg` |

---

## Требования

**Локально (с машины, с которой деплоите):**

- Go 1.26+
- `ansible-playbook`
- SSH-доступ на VPS (`root` или пользователь с sudo)
- DNS: A-запись домена → IP VPS

**На VPS (Ubuntu/Debian):**

- nginx
- ufw (или другой firewall — откройте порты вручную)
- Свободные порты: **MTProto** (например `3129`), **HTTPS API** (например `8445`)
- Порт `443` может быть занят (vpn/другой прокси) — это нормально

---

## Шаг 1. Клонировать и собрать бинарник

```bash
git clone <your-fork-or-repo> mtg
cd mtg

GOOS=linux GOARCH=amd64 go build -trimpath -o deploy/mtg-linux-amd64 .
```

Проверка:

```bash
file deploy/mtg-linux-amd64
# ELF 64-bit LSB executable, x86-64
```

---

## Шаг 2. Подготовить `deploy/.env`

```bash
cp deploy/.env.example deploy/.env
```

Заполните переменные:

| Переменная | Описание |
| --- | --- |
| `MTG_SSH_HOST` | IP VPS |
| `MTG_SSH_USER` | SSH user (`root`) |
| `MTG_DOMAIN` | Домен для API и ссылок (`proxy.example.com`) |
| `MTG_PUBLIC_IP` | Публичный IP VPS (если отличается от DNS) |
| `MTG_PROXY_PORT` | Порт MTProto (например `3129`) |
| `MTG_API_BIND` | Loopback API (`127.0.0.1:9092`) |
| `MTG_API_PUBLIC_PORT` | Внешний HTTPS порт nginx (`8445`) |
| `MTG_API_AUTH_HEADER` | **В кавычках:** `"Bearer $(openssl rand -hex 32)"` |
| `MTG_SECRET` | Шаблон ee-secret: `$(go run . generate-secret $MTG_DOMAIN)` |
| `MTG_DEFAULT_HOST` | Hostname в автогенерируемых user-secret (обычно = домен) |

Пример генерации токена и secret:

```bash
echo "MTG_API_AUTH_HEADER=\"Bearer $(openssl rand -hex 32)\"" >> deploy/.env

DOMAIN=proxy.example.com
echo "MTG_SECRET=$(go run . generate-secret "$DOMAIN")" >> deploy/.env
echo "MTG_DEFAULT_HOST=$DOMAIN" >> deploy/.env
```

> **Важно:** значение `MTG_API_AUTH_HEADER` с пробелом (`Bearer …`) держите в **двойных кавычках** в `.env`.

---

## Шаг 3. Inventory Ansible

Отредактируйте `deploy/ansible/inventory.ini`:

```ini
[mtg]
mtg-prod ansible_host=YOUR_VPS_IP ansible_user=root
```

---

## Шаг 4. Запуск playbook

```bash
set -a && source deploy/.env && set +a

ansible-playbook -i deploy/ansible/inventory.ini deploy/ansible/playbook.yml
```

Playbook:

1. Открывает порты в UFW (`MTG_PROXY_PORT`, `MTG_API_PUBLIC_PORT`)
2. Кладёт бинарник в `/opt/mtg/mtg`
3. Рендерит `config.toml`, `users.toml`
4. Ставит `systemd` unit `mtg.service`
5. Настраивает nginx + certbot для `MTG_DOMAIN`
6. Проксирует `https://domain:8445` → `127.0.0.1:9092`

Копия секретов на сервер: `deploy/.env` → `/opt/mtg/deploy.env` (chmod 600) — при необходимости добавьте task в playbook.

---

## Шаг 5. Проверка на VPS

```bash
ssh root@YOUR_VPS_IP

systemctl status mtg
ss -tlnp | grep -E '3129|9092'
curl -s http://127.0.0.1:9092/v1/health -H "Authorization: Bearer ..."   # токен из deploy.env
```

С локальной машины:

```bash
source deploy/.env
curl -sk "https://$MTG_DOMAIN:$MTG_API_PUBLIC_PORT/v1/health" \
  -H "Authorization: $MTG_API_AUTH_HEADER"
```

Ожидается: `"ok": true`, `"status": "ok"`.

---

## Шаг 6. Пользователи и ссылки через API

Пока в `users.toml` нет пользователей, MTProto **не пускает** клиентов.

```bash
source deploy/.env
API_URL="https://${MTG_DOMAIN}:${MTG_API_PUBLIC_PORT}"
AUTH="$MTG_API_AUTH_HEADER"
```

**Создать** пользователя с датой истечения и вывести `tg://` ссылку:

```bash
curl -sk -X POST "$API_URL/v1/users" \
  -H "Authorization: $AUTH" \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","expires_at":"2026-12-31T23:59:59Z"}' \
  | jq -r '.data.user.links.tls[0]'
```

**Получить** ссылку снова:

```bash
curl -sk "$API_URL/v1/users/alice" \
  -H "Authorization: $AUTH" \
  | jq -r '.data.links.tls[0]'
```

**Удалить** (revoke — безвозвратно):

```bash
curl -sk -X DELETE "$API_URL/v1/users/alice" \
  -H "Authorization: $AUTH" \
  | jq .
```

Полный справочник curl: [docs/API.md](../docs/API.md#быстрые-примеры-curl).

---

## Обновление версии mtg

```bash
git pull
GOOS=linux GOARCH=amd64 go build -o deploy/mtg-linux-amd64 .
set -a && source deploy/.env && set +a
ansible-playbook -i deploy/ansible/inventory.ini deploy/ansible/playbook.yml
```

---

## Структура deploy/

```text
deploy/
  .env.example          # шаблон переменных
  .env                  # ваши секреты (в .gitignore)
  mtg-linux-amd64       # собранный бинарник (в .gitignore)
  ansible/
    inventory.ini
    playbook.yml
    templates/
      config.toml.j2
      users.toml.j2
      mtg.service.j2
      nginx-mtg-api*.j2
```

---

## Типичные проблемы

| Симптом | Причина | Решение |
| --- | --- | --- |
| `mtg` в restart loop, `value out of range` для `65536` | `request-body-limit-bytes` > 65535 в конфиге | Уберите строку из шаблона (лимит по умолчанию в коде) |
| curl снаружи timeout | UFW закрывает порт | `ufw allow 8445/tcp`, `ufw allow 3129/tcp` |
| `401 unauthorized` | Неверный `Authorization` | Заголовок = **точно** `MTG_API_AUTH_HEADER` из `.env` |
| `api_disabled` | Нет `users-file` в config | Проверьте `/opt/mtg/config.toml` |
| MTProto не подключается | Нет пользователей в `users.toml` | `POST /v1/users` |
| certbot failed | DNS не на VPS / порт 80 закрыт | Проверьте A-запись и `ufw allow 80/tcp` |

---

## Без nginx (только для отладки)

Не рекомендуется в проде. API слушает `127.0.0.1:9092`, с интернета недоступен без reverse proxy.

Для теста на VPS:

```bash
curl -s http://127.0.0.1:9092/v1/health -H "Authorization: ..."
```

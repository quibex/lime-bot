# Lime-VPN • Полное ТЗ (v0.6)

*14 июня 2025*

Финальная версия спецификации lime-bot в связке с уже реализованным и задеплоенным wg-agent.

---

## 1 Архитектура

| Компонент    | Описание                                                          | Стек                                          |
| ------------ | ----------------------------------------------------------------- | --------------------------------------------- |
| **lime-bot** | Telegram-бот, бизнес-логика, хранение в SQLite (позже PostgreSQL) | Go 1.24, `telegram-bot-api/v6`, GORM (sqlite) |
| **wg-agent** | Управление WireGuard через gRPC (готово и задеплоено)             | Go 1.24, `wgctrl`, `grpc-go` (TLS)            |

> **Без Redis**: всё состояние — в БД и в памяти бота.

---

## 2 gRPC-контракт wg-agent

```protobuf
syntax = "proto3";
package wgagent;

import "google/protobuf/empty.proto";

option go_package = "github.com/our-org/wg-project/api/proto";

service WireGuardAgent {
  // Основные операции с пирами
  rpc AddPeer(AddPeerRequest) returns (AddPeerResponse);
  rpc RemovePeer(RemovePeerRequest) returns (google.protobuf.Empty);
  rpc DisablePeer(DisablePeerRequest) returns (google.protobuf.Empty);
  rpc EnablePeer(EnablePeerRequest) returns (google.protobuf.Empty);
  
  // Информация и статистика
  rpc GetPeerInfo(GetPeerInfoRequest) returns (GetPeerInfoResponse);
  rpc ListPeers(ListPeersRequest) returns (ListPeersResponse);
  
  // Генерация конфигураций
  rpc GeneratePeerConfig(GeneratePeerConfigRequest) returns (GeneratePeerConfigResponse);
}

message AddPeerRequest {
  string interface   = 1;  // "wg0"
  string public_key  = 2;
  string allowed_ip  = 3;  // "10.8.0.10/32"
  int32  keepalive_s = 4;  // 25
  string peer_id     = 5;  // уникальный идентификатор пира для lime-bot
}

message AddPeerResponse { 
  int32 listen_port = 1;
  string config     = 2;  // полная конфигурация клиента
  string qr_code    = 3;  // QR код в base64
}

message RemovePeerRequest { 
  string interface = 1; 
  string public_key = 2; 
}

message DisablePeerRequest {
  string interface = 1;
  string public_key = 2;
}

message EnablePeerRequest {
  string interface = 1;
  string public_key = 2;
}

message GetPeerInfoRequest {
  string interface = 1;
  string public_key = 2;
}

message GetPeerInfoResponse {
  string public_key = 1;
  string allowed_ip = 2;
  int64 last_handshake_unix = 3;
  int64 rx_bytes = 4;
  int64 tx_bytes = 5;
  bool enabled = 6;
  string peer_id = 7;
}

message ListPeersRequest { 
  string interface = 1; 
}

message ListPeersResponse { 
  repeated PeerInfo peers = 1; 
}

message PeerInfo {
  string public_key = 1;
  string allowed_ip = 2;
  bool enabled = 3;
  string peer_id = 4;
}

message GeneratePeerConfigRequest {
  string interface = 1;
  string server_endpoint = 2;  // "vpn.example.com:51820"
  string dns_servers = 3;      // "1.1.1.1, 1.0.0.1"
  string allowed_ips = 4;      // "0.0.0.0/0" для полного туннеля
}

message GeneratePeerConfigResponse {
  string private_key = 1;
  string public_key = 2;
  string config = 3;      // конфигурация для клиента
  string qr_code = 4;     // QR код в base64
  string allowed_ip = 5;  // выделенный IP адрес
}
```

## 3 Схема базы данных Схема базы данных

```sql
-- планы
CREATE TABLE plans (
  id            SERIAL PRIMARY KEY,
  name          TEXT NOT NULL,
  price_int     INT NOT NULL,
  duration_days INT NOT NULL,
  archived      BOOL DEFAULT FALSE,
  created_at    TIMESTAMPTZ DEFAULT NOW()
);

-- пользователи
CREATE TABLE users (
  tg_id      BIGINT PRIMARY KEY,
  username   TEXT,
  phone      TEXT,
  ref_code   TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- администраторы
CREATE TABLE admins (
  tg_id    BIGINT PRIMARY KEY,
  role     TEXT CHECK(role IN('super','cashier','support')),
  disabled BOOL DEFAULT FALSE
);

-- способы оплаты (реквизиты)
CREATE TABLE payment_methods (
  id            SERIAL PRIMARY KEY,
  phone_number  TEXT NOT NULL,
  bank          TEXT NOT NULL,
  owner_name    TEXT NOT NULL,
  archived      BOOL DEFAULT FALSE,
  created_at    TIMESTAMPTZ DEFAULT NOW()
);

-- платежи
CREATE TABLE payments (
  id              SERIAL PRIMARY KEY,
  user_id         BIGINT REFERENCES users(tg_id),
  method_id       INT REFERENCES payment_methods(id),
  amount          INT NOT NULL,
  plan_id         INT REFERENCES plans(id),
  qty             INT NOT NULL,
  receipt_file_id TEXT,
  status          TEXT CHECK(status IN('pending','approved','rejected')),
  approved_by     BIGINT REFERENCES admins(tg_id),
  created_at      TIMESTAMPTZ DEFAULT NOW()
);

-- подписки (ключи)
CREATE TABLE subscriptions (
  id          SERIAL PRIMARY KEY,
  user_id     BIGINT REFERENCES users(tg_id),
  plan_id     INT REFERENCES plans(id),
  peer_id     TEXT UNIQUE NOT NULL,
  priv_key_enc TEXT NOT NULL,
  public_key  TEXT NOT NULL,
  interface   TEXT NOT NULL,
  allowed_ip  INET NOT NULL,
  platform    TEXT NOT NULL,
  start_date  DATE NOT NULL,
  end_date    DATE NOT NULL,
  active      BOOL DEFAULT TRUE,
  payment_id  INT REFERENCES payments(id)
);

-- рефералы
CREATE TABLE referrals (
  id          SERIAL PRIMARY KEY,
  inviter_id  BIGINT REFERENCES users(tg_id),
  invitee_id  BIGINT REFERENCES users(tg_id),
  created_at  TIMESTAMPTZ DEFAULT NOW()
);
```

*Поле `priv_key_enc` хранит AES-GCM-зашифрованный приватный ключ (master-key из ENV).*

---

## 3.1 Миграции базы данных

через гусь

---

## 3.2  Команды ↔ SQL‑операции (подробный разбор)

> Ниже — по‑шаговый «рецепт» для каждого маршрута бота: какие запросы выполняем, нужны ли транзакции, индексы и блокировки. SQLite описан как СУБД по умолчанию; в скобках указаны нюансы PostgreSQL (на будущее).

### Легенда

* **Tx** — оборачиваем в транзакцию (`BEGIN DEFERRED` … `COMMIT`).
* **IMMEDIATE lock** — для SQLite аналог `SELECT … FOR UPDATE` (блокирует страницу файла).
* **PK‑индекс** — первичный ключ уже индексирован, дополнительные перечислены вручную.

### 1. `/addplan`  — создать тариф

| Шаг     | SQL                                                             | Комментарий                                      |
| ------- | --------------------------------------------------------------- | ------------------------------------------------ |
| 1       | `INSERT INTO plans(name,price_int,duration_days) VALUES(?,?,?)` | Однострочная вставка, транзакция не обязательна. |
| Индексы | `CREATE UNIQUE INDEX idx_plans_name ON plans(name);`            | быстро искать по названию.                       |

### 2. `/archiveplan`  — архивировать тариф

| Шаг | SQL                                      | Комментарий         |
| --- | ---------------------------------------- | ------------------- |
| 1   | `UPDATE plans SET archived=1 WHERE id=?` | Простое обновление. |

### 3. `/addpmethod`  — новый способ оплаты

| Шаг    | SQL                                                                       | Комментарий                      |
| ------ | ------------------------------------------------------------------------- | -------------------------------- |
| 1      | `INSERT INTO payment_methods(phone_number,bank,owner_name) VALUES(?,?,?)` | Телефон лучше хранить как TEXT.  |
| Индекс | `CREATE INDEX idx_pm_active ON payment_methods(archived, id);`            | Быстро получать активные методы. |

### 4. `/buy`  — пользователь оформляет заказ

| Шаг     | SQL                                                                                             | Комментарий                                            |
| ------- | ----------------------------------------------------------------------------------------------- | ------------------------------------------------------ |
| 1       | **Tx Begin** (`BEGIN IMMEDIATE`)                                                                | блокируем БД на запись, чтобы сериализовать IP‑выдачу. |
| 2       | `INSERT INTO payments(user_id,method_id,amount,plan_id,qty,status) VALUES(?,?,?,?,?,'pending')` | сохраняем чек.                                         |
| 3       | — генерируем peer‑config через gRPC, получаем `peer_id`, `allowed_ip`, `public_key`, `priv_key` |                                                        |
| 4       | `INSERT INTO subscriptions(...) VALUES(...)`                                                    | создаём подписку, `active=1`.                          |
| 5       | **Tx Commit**                                                                                   |                                                        |
| Индексы | `CREATE INDEX idx_sub_user_active ON subscriptions(user_id,active);`                            | ускоряет `/mykeys`.                                    |

> **Почему `BEGIN IMMEDIATE`?** В SQLite нет row‑level Lock, поэтому при параллельной покупке двух ключей одним юзером мы сознательно сереализуем транзакцию; конфликтов мало — приемлемо.

### 5. `/payqueue` — кассир нажимает ✅

| Шаг | SQL                                                                                                           | Комментарий                           |
| --- | ------------------------------------------------------------------------------------------------------------- | ------------------------------------- |
| 1   | **Tx Begin**                                                                                                  |                                       |
| 2   | `UPDATE payments SET status='approved', approved_by=? WHERE id=? AND status='pending'`                        | проверяем, что ещё pend.              |
| 3   | **IMMEDIATE lock on subscription rows** (SQLite: `UPDATE subscriptions SET active=active WHERE payment_id=?`) | страхуемся от гонки двойного approve. |
| 4   | оставляем подписку как есть (`active` уже TRUE).                                                              |                                       |
| 5   | **Commit**                                                                                                    |                                       |

### 6. `/disable` / `/enable`

| Шаг | SQL                                                 | Комментарий |
| --- | --------------------------------------------------- | ----------- |
| 1   | `UPDATE subscriptions SET active=? WHERE peer_id=?` | active=0/1  |
| 2   | gRPC `DisablePeer` / `EnablePeer`                   |             |

### 7. Крон «истёк срок»

| Шаг | SQL                                                                               | Комментарий         |
| --- | --------------------------------------------------------------------------------- | ------------------- |
| 1   | `SELECT peer_id FROM subscriptions WHERE active=1 AND end_date<DATE('now')`       | список просроченных |
| 2   | для каждого → `DisablePeer`; затем `UPDATE subscriptions SET active=0 WHERE id=?` |                     |

### 8. `/info <nick>`

| Шаг | SQL                                                                  | Комментарий |   |   |               |             |
| --- | -------------------------------------------------------------------- | ----------- | - | - | ------------- | ----------- |
| 1   | \`SELECT \* FROM users WHERE username LIKE '%'                       |             | ? |   | '%' LIMIT 5\` | fuzzy‑поиск |
| 2   | `SELECT * FROM subscriptions WHERE user_id=? ORDER BY end_date DESC` |             |   |   |               |             |
| 3   | `SELECT * FROM payments WHERE user_id=? ORDER BY created_at DESC`    |             |   |   |               |             |

### 9. `/admins` операции

* **Добавить**: `INSERT INTO admins(tg_id,role) VALUES(?,?)`
* **Отключить**: `UPDATE admins SET disabled=1 WHERE tg_id=?`
* **Назначить кассира**: `UPDATE admins SET role='cashier' WHERE tg_id=?`

### 10. Индекс‑сводка

```sql
-- быстрый поиск активных подписок юзера
CREATE INDEX idx_sub_user_active ON subscriptions(user_id,active);
-- активные тарифы
CREATE INDEX idx_plans_archived ON plans(archived);
-- активные payment_methods
CREATE INDEX idx_pm_archived ON payment_methods(archived);
-- очередь платежей
CREATE INDEX idx_pay_status ON payments(status, created_at);
```

> **PostgreSQL в будущем**: все запросы совместимы, но там вместо `BEGIN IMMEDIATE` можно использовать `BEGIN; ... FOR UPDATE`.

---

## 4 lime-bot: команды и сценарии: команды и сценарии

### 4.1 Админские команды

| Команда            | UI / Параметры                         | Действие                                                                         |
| ------------------ | -------------------------------------- | -------------------------------------------------------------------------------- |
| `/newkey`          | ник → тариф → платформа → дата старта  | `GeneratePeerConfig` → `AddPeer` → insert в `subscriptions` → отправка QR+config |
| `/disable <nick>`  | —                                      | `DisablePeer` + `active=false`                                                   |
| `/enable <nick>`   | —                                      | `EnablePeer` + `active=true`                                                     |
| `/addplan`         | name, duration\_days, price\_int       | insert в `plans`                                                                 |
| `/archiveplan`     | inline-список `plans`                  | `archived=true`                                                                  |
| `/info <nick>`     | fuzzy-поиск                            | вывод user + подписки + платежи                                                  |
| `/admins`          | ➕ Add、🗑 Remove、⭐ Set cashier (inline) | insert/update `admins`                                                           |
| `/payqueue`        | список `payments.status='pending'`     | inline ✅ → approve (create subscriptions) / ❌ → reject + `DisablePeer`           |
| `/addpmethod`      | телефон, банк, имя владельца           | insert в `payment_methods`                                                       |
| `/archivepmethod`  | inline-список `payment_methods`        | `archived=true`                                                                  |
| `/listpmethods`    | —                                      | показать все НЕ архивированные способы оплаты                                    |
| `/delpmethod <id>` | —                                      | `archived=true` для method\_id                                                   |

### 4.2 Пользовательские команды

| Команда     | Логика                                                                                                 |
| ----------- | ------------------------------------------------------------------------------------------------------ |
| `/buy`      | выбор тарифа → платформа → qty → выбор payment\_method (inline) → реквизиты → чек → key выдаётся сразу |
| `/mykeys`   | список активных подписок + кнопки «🔗 Link / 📄 Conf / 📷 QR»                                          |
| `/ref`      | генерируем `t.me/limevpn_bot?start=ref_<code>`                                                         |
| `/feedback` | текст/фото → форвард в канал отзывов                                                                   |
| `/help`     | статический FAQ                                                                                        |

### 4.3 Cron-задачи

| Частота           | Задача                                                               |
| ----------------- | -------------------------------------------------------------------- |
| ⏱ каждые 1 мин    | обработка inline CallbackQuery (approve/reject)                      |
| ⏱ каждые 30 мин   | напоминание «подписка кончится через 3 дня»                          |
| ⏱ ежедневно 00:10 | `DisablePeer`/`RemovePeer` для всех `subscriptions.end_date < today` |
| ⏱ каждые 5 мин    | запуск `/usr/local/bin/wg-agent-health.sh`                           |

---

## 5 ENV и Dockerfile

```env
# Telegram
BOT_TOKEN=...
SUPER_ADMIN_ID=123456789
REVIEWS_CHANNEL_ID=-1001900

# БД (SQLite)
DB_DSN=file://data/limevpn.db

# wg-agent gRPC
WG_AGENT_ADDR=wg-agent:7443
WG_CLIENT_CERT=/run/secrets/client.crt
WG_CLIENT_KEY=/run/secrets/client.key
WG_CA_CERT=/run/secrets/ca.crt

# Health-check для скрипта
TG_TOKEN=...
TG_CHAT_ID=123456789
```

```dockerfile
# Dockerfile для lime-bot
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o lime-bot ./cmd/lime-bot

FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/lime-bot /usr/local/bin/lime-bot
WORKDIR /data
VOLUME ["/data"]
ENV DB_DSN=file://data/limevpn.db
CMD ["/usr/local/bin/lime-bot", "serve"]
```

> **Для локальной разработки** можно добавить `docker-compose.dev.yml`, но в проде используется только Dockerfile + Swarm/Portainer или прямой `docker run`.

---

## 6 Roadmap & Пошаговый план разработки lime-bot

### ✅ Выполнено

1. **Инициализация проекта**

   * ✅ Обновлен Go-модуль с правильными зависимостями (telegram-bot-api v5, GORM SQLite, gRPC)
   * ✅ Обновлена конфигурация согласно ТЗ (ENV переменные)
   * ✅ Модели БД приведены в соответствие с ТЗ
   * ✅ Dockerfile обновлен согласно спецификации
   * ✅ Создан protobuf контракт для wg-agent

2. **Базовые команды**

   * ✅ `/start`, `/help`, `/plans` (SELECT plans WHERE archived=false)
   * ✅ CRUD на plans: `/addplan`, `/archiveplan` → GORM-модели + handlers  
   * ✅ Inline-UI для архивирования тарифов

3. **Управление реквизитами**

   * ✅ Модель `payment_methods`
   * ✅ Команды `/addpmethod`, `/listpmethods`, `/archivepmethod`
   * ✅ Inline-UI для архивирования способов оплаты

4. **Покупка / платежи**

   * ✅ `/buy` flow: выбор плана → платформа → qty → метод → создание `payments(pending)`
   * ✅ Транзакционное создание подписок с интеграцией wg-agent
   * ✅ Inline-UI для всего процесса покупки
   * 🔄 Inline-кнопки в `/payqueue`: approve → создание `subscriptions`, reject → `DisablePeer`

5. **Интеграция wg-agent**

   * ✅ RPC `GeneratePeerConfig` → сохранить priv/pub, allowed\_ip
   * ✅ RPC `AddPeer(peer_id)` → получить `listen_port`, `config`, `qr_code`
   * ✅ Сохранить в `subscriptions` + отправить пользователю
   * ✅ Mock клиент для разработки (готов к замене на реальный API)

6. **Управление подписками**

   * ✅ Команды `/disable`, `/enable` для администраторов
   * ✅ Пользовательская `/mykeys` с inline-кнопками
   * ✅ Отправка конфигураций и QR-кодов
   * 🔄 Авто-Disable в cron

### ✅ Реализовано недавно

7. **Дополнительные фичи**

   * ✅ `/admins` - управление администраторами с inline-кнопками
   * ✅ `/payqueue` - очередь платежей на проверку с approve/reject
   * ✅ `/info <username>` - информация о пользователе (fuzzy поиск)
   * ✅ `/ref` - реферальная система с генерацией ссылок
   * ✅ `/feedback` - система отзывов с пересылкой в канал

8. **Планировщик и мониторинг**

   * ✅ Cron-задачи для автоматического отключения истекших подписок
   * ✅ Напоминания о скором истечении подписок
   * ✅ Health-check wg-agent сервиса
   * ✅ Система логирования и обработки ошибок

9. **Тестирование**

   * ✅ Unit-тесты для критических функций
   * ✅ Система обработки ошибок с отчетами админу
   * ✅ Полная проверка сборки

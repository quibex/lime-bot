# Анализ багов и исправления

## 1. Баг обработки чеков (buy.go:552-614)

### Текущая проблема

В `handleReceiptMessage()` когда пользователь отправляет чек (фото или документ), бот сразу создавал подписки и выдавал ключи через WG Agent, но при этом не уведомлял кассиров для проверки чека.

### Причина бага

- Отсутствовала уведомление кассиров о получении чека
- Не было логики отключения подписок при отклонении кассиром

### Правильный процесс

1. Пользователь отправляет чек → СРАЗУ создается подписка и выдаются ключи
2. Чек сохраняется в БД с `ReceiptFileID`
3. Кассиры получают уведомление о необходимости проверки чека
4. Если кассир отклоняет → подписки ОТКЛЮЧАЮТСЯ через WG Agent
5. Пользователь получает уведомление об отклонении

### Исправление

**1. В `handleReceiptMessage()`:**

- Сохраняем чек в БД
- СРАЗУ создаем подписки и выдаем ключи пользователю
- Уведомляем кассиров о новом чеке для проверки

**2. В `rejectPayment()`:**

- При отклонении платежа отключаем все связанные подписки
- Уведомляем пользователя об отклонении и отключении подписок

**3. Добавлена функция `notifyCashiersAboutReceipt()`:**

- Находит всех кассиров (или админов если кассиров нет)
- Отправляет уведомление с кнопкой для перехода в /payqueue

## 2. Баг множественных кассиров (admin.go:709-720)

### Текущая проблема

В `setCashierRole()` роль кассира назначалась новому пользователю без удаления роли у предыдущих кассиров, что приводило к множественным кассирам.

### Причина бага

- Отсутствовала транзакция БД для атомарности операции
- Не снималась роль кассира у предыдущих пользователей

### Исправление

**1. Добавлена транзакция:**

```go
tx := s.repo.DB().Begin()
```

**2. Сначала снимаем роль у всех:**

```go
tx.Model(&db.Admin{}).Where("role = ?", RoleCashier.String()).Update("role", RoleAdmin.String())
```

**3. Проверяем что целевой пользователь активный админ:**

```go
if result.RowsAffected == 0 {
    return fmt.Errorf("пользователь не является активным администратором")
}
```

**4. Назначаем нового кассира:**

```go
tx.Model(&targetAdmin).Update("role", RoleCashier.String())
```

## 3. WG Agent интеграционный тест

### Текущая проблема

Тестовый код был помещен прямо в main.go, что нарушает принципы организации кода.

### Исправление

**1. Создан отдельный пакет `internal/wgtest/`**

- `IntegrationTest` структура для тестов
- `RunStartupTest()` - тест при запуске
- `RunPeriodicHealthCheck()` - периодические проверки

**2. В main.go используется правильный пакет:**

```go
wgIntegrationTest := wgtest.NewIntegrationTest(wgConfig, notifyFn)
go wgIntegrationTest.RunStartupTest(testCtx)
go wgIntegrationTest.RunPeriodicHealthCheck(ctx, 5*time.Minute)
```

## Все исправления протестированы командой `make all` ✅

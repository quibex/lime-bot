# Детальное описание кейсов обработки в lime-bot

*Документ описывает как именно обрабатывается каждый кейс в коде на момент анализа*

## Основная диспетчеризация

### Точка входа - handleUpdate()

**Файл:** `internal/telegram/bot.go`, строки 78-133

1. **Строка 87-97:** Обработка сообщения
   - Создание пользователя `db.User` с TgID и Username
   - Запрос `s.repo.DB().FirstOrCreate(user, "tg_id = ?", upd.Message.From.ID)`
   - Обновление username если изменился

2. **Строка 109-119:** Проверка типа сообщения
   - Если команда: `s.handleCommand(upd.Message)`
   - Если текст: `s.handleReceiptMessage(upd.Message)` и `s.handleFeedbackMessage(upd.Message)`

3. **Строка 121-133:** Обработка callback-запросов
   - Все callback проходят через `s.handleCallbackQuery(upd.CallbackQuery)`

### Диспетчер команд - handleCommand()

**Файл:** `internal/telegram/bot.go`, строки 240-300

1. **Строка 245-249:** Валидация команды
   - Проверка `cmd.IsValid()` на основе списка в `types.go:30-37`

2. **Строка 252-259:** Проверка прав
   - Для админских команд проверка `s.isAdmin(msg.From.ID)`
   - Список админских команд в `types.go:41-46`

3. **Строка 261-289:** Роутинг команд
   - Switch по типу команды с вызовом соответствующего обработчика

## Пользовательские кейсы

### Кейс 1: Старт бота (/start)

**Обработчик:** `handleStartWithRef()` в `internal/telegram/user.go:124-204`

**Поток выполнения:**

1. **Строка 126-134:** Создание пользователя
   - Создание/обновление `db.User` с TgID и Username
   - `s.repo.DB().FirstOrCreate(user, "tg_id = ?", msg.From.ID)`

2. **Строка 137-142:** Проверка реферальной ссылки
   - Если args не начинается с "ref_", вызов обычного `s.handleStart(msg)`

3. **Строка 144-204:** Обработка реферального кода
   - Извлечение refCode из аргументов
   - Поиск приглашающего: `s.repo.DB().Where("ref_code = ?", refCode).First(&inviter)`
   - Проверка на самоприглашение
   - Создание записи `db.Referral`
   - Уведомление приглашающего

### Кейс 2: Покупка VPN ключа (/buy)

**Файл:** `internal/telegram/buy.go`

#### Этап 1: Начало покупки - handleBuy()

**Строки 32-63:**

1. **Строка 35-41:** Получение тарифов
   - Запрос `s.repo.DB().Where("archived = false").Find(&plans)`

2. **Строка 47-56:** Создание клавиатуры
   - Формирование `tgbotapi.InlineKeyboardButton` для каждого тарифа
   - Callback data: `CallbackBuyPlan.WithID(plan.ID)`

3. **Строка 58-61:** Создание состояния покупки
   - Сохранение в глобальный `buyStates[msg.From.ID]`
   - Установка `Step: BuyStepPlan`

#### Этап 2: Выбор тарифа - handlePlanSelection()

**Строки 86-124:**

1. **Строка 87-94:** Парсинг ID тарифа
   - Извлечение из callback data: `strings.TrimPrefix(callback.Data, CallbackBuyPlan.String())`

2. **Строка 96-98:** Обновление состояния
   - `state.PlanID = uint(planID)`
   - `state.Step = BuyStepPlatform`

3. **Строка 101-115:** Создание меню платформ
   - 5 кнопок для платформ (Android, iOS, Windows, Linux, macOS)
   - Callback data: `CallbackBuyPlatform.WithID(PlatformAndroid.String())`

#### Этап 3: Выбор платформы - handlePlatformSelection()

**Строки 126-154:**

1. **Строка 127-134:** Валидация платформы
   - Парсинг из callback: `strings.TrimPrefix(callback.Data, CallbackBuyPlatform.String())`
   - Проверка `platform.IsValid()`

2. **Строка 136-138:** Обновление состояния
   - `state.Platform = platform`
   - `state.Step = BuyStepQty`

3. **Строка 141-147:** Создание меню количества
   - 4 варианта: 1, 2, 3, 5 ключей

#### Этап 4: Выбор количества - handleQtySelection()

**Строки 156-198:**

1. **Строка 157-164:** Парсинг количества
   - Конвертация в int: `strconv.Atoi(qtyStr)`

2. **Строка 166-168:** Обновление состояния
   - `state.Qty = qty`
   - `state.Step = BuyStepMethod`

3. **Строка 171-179:** Получение способов оплаты
   - Запрос `s.repo.DB().Where("archived = false").Find(&methods)`

4. **Строка 185-195:** Создание меню способов оплаты
   - Кнопка для каждого метода с названием банка и номером

#### Этап 5: Выбор способа оплаты - handleMethodSelection()

**Строки 200-221:**

1. **Строка 201-209:** Парсинг ID метода
   - Извлечение и конвертация: `strconv.ParseUint(methodIDStr, 10, 32)`

2. **Строка 211-212:** Обновление состояния
   - `state.MethodID = uint(methodID)`

3. **Строка 215-221:** Обработка покупки
   - Вызов `s.processPurchase(callback, state)`
   - Очистка состояния: `delete(buyStates, callback.From.ID)`

#### Этап 6: Создание платежа - processPurchase()

**Строки 222-329:**

1. **Строка 232-238:** Получение данных тарифа и метода
   - Загрузка `db.Plan` и `db.PaymentMethod` из БД

2. **Строка 240-248:** Расчет суммы
   - `totalAmount := plan.PriceInt * state.Qty`

3. **Строка 250-261:** Создание записи платежа
   - Создание `db.Payment` со статусом `PaymentStatusPending`
   - Сохранение в БД: `s.repo.DB().Create(payment)`

4. **Строка 264-268:** Уведомления
   - Пользователю: `s.sendPaymentInstructions()`
   - В админ-чат: `s.sendPaymentInfo()`

### Кейс 3: Просмотр моих ключей (/mykeys)

**Обработчик:** `handleMyKeys()` в `internal/telegram/subscriptions.go:17-65`

**Поток выполнения:**

1. **Строка 19-21:** Получение подписок
   - Запрос `s.repo.DB().Where("user_id = ? AND active = true", msg.From.ID).Preload("Plan").Find(&subscriptions)`

2. **Строка 29-42:** Формирование текста
   - Цикл по подпискам с выводом информации
   - Статус, ID, дата окончания

3. **Строка 44-58:** Создание кнопок
   - Для каждой подписки кнопки "Config" и "QR"
   - Callback data: `sub_config_{peer_id}` и `sub_qr_{peer_id}`

### Кейс 4: Обработка чеков (/receipt)

**Обработчик:** `handleReceiptMessage()` в `internal/telegram/buy.go:553-614`

**Поток выполнения:**

1. **Строка 555-561:** Проверка наличия фото
   - Возврат, если `msg.Photo == nil`

2. **Строка 563-569:** Поиск pending платежа
   - Запрос `s.repo.DB().Where("user_id = ? AND status = ?", msg.From.ID, PaymentStatusPending).Preload("Plan").First(&payment)`

3. **Строка 575-590:** Сохранение фото чека
   - Получение `FileID` самого большого фото
   - Обновление поля `ReceiptPhoto`: `s.repo.DB().Model(&payment).Update("receipt_photo", photo.FileID)`

4. **Строка 592-614:** Уведомления
   - Пользователю: подтверждение загрузки
   - Админам: информация о новом чеке

## Административные кейсы

### Кейс 5: Управление админами (/admins)

**Обработчик:** `handleAdmins()` в `internal/telegram/admin.go:18-47`

**Поток выполнения:**

1. **Строка 21-28:** Проверка прав суперадмина
   - Вызов `s.isSuperAdmin(msg.From.ID)`
   - Отказ в доступе обычным админам

2. **Строка 31-41:** Создание меню управления
   - 4 кнопки: "Добавить", "Список", "Отключить", "Назначить кассира"
   - Callback data: `CallbackAdminAdd.String()` и т.д.

### Кейс 6: Добавление админа (/add_admin)

**Обработчик:** `handleAddAdmin()` в `internal/telegram/admin.go:724-784`

**Поток выполнения:**

1. **Строка 725-729:** Проверка прав суперадмина
   - Только суперадмин может добавлять админов

2. **Строка 731-744:** Парсинг аргументов
   - Извлечение username и роли
   - Валидация роли: `role.IsValid()`

3. **Строка 747-753:** Поиск пользователя
   - Запрос `s.repo.DB().Where("username = ?", username).First(&user)`

4. **Строка 756-762:** Проверка существующего админа
   - Проверка `s.repo.DB().Where("tg_id = ?", user.TgID).First(&existingAdmin)`

5. **Строка 765-776:** Создание нового админа
   - Создание `db.Admin` с ролью
   - Сохранение: `s.repo.DB().Create(admin)`

### Кейс 7: Очередь платежей (/payqueue)

**Обработчик:** `handlePayQueue()` в `internal/telegram/admin.go:48-127`

**Поток выполнения:**

1. **Строка 49-53:** Проверка админских прав
   - Вызов `s.isAdmin(msg.From.ID)`

2. **Строка 55-61:** Получение pending платежей
   - Запрос `s.repo.DB().Where("status = ?", PaymentStatusPending).Preload("Plan").Preload("User").Order("created_at ASC").Find(&payments)`

3. **Строка 68-90:** Формирование списка
   - Для каждого платежа: пользователь, тариф, сумма, дата
   - Кнопки "Одобрить" и "Отклонить"

4. **Строка 92-127:** Обработка callback
   - Одобрение: `CallbackPaymentApprove.WithID(payment.ID)`
   - Отклонение: `CallbackPaymentReject.WithID(payment.ID)`

### Кейс 8: Одобрение платежа

**Обработчик:** `approvePayment()` в `internal/telegram/admin.go:448-525`

**Поток выполнения:**

1. **Строка 452-465:** Получение данных платежа
   - Загрузка `db.Payment` с Preload("Plan", "User")
   - Проверка что статус "pending"

2. **Строка 467-474:** Транзакция БД
   - Начало транзакции: `s.repo.DB().Begin()`
   - Обновление статуса: `tx.Model(&payment).Update("status", PaymentStatusApproved.String())`

3. **Строка 477-505:** Создание подписки
   - Вызов `s.createSubscriptionForPayment(tx, &payment)`
   - Интеграция с WG Agent для создания peer

4. **Строка 507-525:** Уведомления
   - Пользователю: конфиг и QR код
   - Админу: подтверждение одобрения

### Кейс 9: Отключение пользователя (/disable)

**Обработчик:** `handleDisable()` в `internal/telegram/subscriptions.go:66-152`

**Поток выполнения:**

1. **Строка 69-77:** Проверка админских прав
   - Логирование попытки доступа

2. **Строка 79-85:** Парсинг username
   - Извлечение из аргументов команды

3. **Строка 88-107:** Поиск пользователя
   - Запрос `s.repo.DB().Where("username = ?", username).First(&user)`
   - Обработка ошибок с детальным логированием

4. **Строка 109-121:** Получение активных подписок
   - Запрос `s.repo.DB().Where("user_id = ? AND active = true", user.TgID).Find(&subscriptions)`

5. **Строка 129-149:** Отключение подписок
   - Цикл по подпискам
   - Вызов `s.disablePeer(sub.Interface, sub.PublicKey)` для WG Agent
   - Обновление БД: `s.repo.DB().Model(&sub).Update("active", false)`

### Кейс 10: Информация о пользователе (/info)

**Обработчик:** `handleInfo()` в `internal/telegram/admin.go:128-175`

**Поток выполнения:**

1. **Строка 129-133:** Проверка админских прав

2. **Строка 135-141:** Парсинг username

3. **Строка 143-156:** Поиск пользователей
   - Поиск по LIKE: `s.repo.DB().Where("username LIKE ?", "%"+username+"%").Limit(5).Find(&users)`

4. **Строка 158-172:** Обработка результатов
   - Если один пользователь: вызов `s.sendUserInfo()`
   - Если несколько: создание кнопок для выбора

5. **Строка 177-245:** Детальная информация - sendUserInfo()
   - Загрузка всех подписок пользователя
   - Информация о платежах, рефералах
   - Статистика использования

## Callback обработчики

### Обработка покупок - handleBuyCallback()

**Файл:** `internal/telegram/buy.go:65-84`

Диспетчер для всех callback-ов процесса покупки:

- `CallbackBuyPlan` → `handlePlanSelection()`
- `CallbackBuyPlatform` → `handlePlatformSelection()`
- `CallbackBuyQty` → `handleQtySelection()`
- `CallbackBuyMethod` → `handleMethodSelection()`

### Обработка админских действий - handleAdminCallback()

**Файл:** `internal/telegram/admin.go:247-322`

Диспетчер для всех админских callback-ов:

- `CallbackAdminList` → показ списка админов
- `CallbackPaymentApprove` → одобрение платежа
- `CallbackPaymentReject` → отклонение платежа
- `CallbackInfoUser` → показ информации о пользователе
- `CallbackDisableAdmin` → отключение админа
- `CallbackSetCashier` → назначение кассира

### Обработка подписок - handleSubscriptionCallback()

**Файл:** `internal/telegram/subscriptions.go:200-215`

- `sub_config_{peer_id}` → отправка конфигурации WireGuard
- `sub_qr_{peer_id}` → отправка QR кода

## Система ошибок

### Централизованная обработка - handleError()

**Файл:** `internal/telegram/errors.go:86-107`

1. **Строка 90-97:** Определение типа ошибки
   - Проверка на `*BotError`
   - Создание BotError из обычной ошибки

2. **Строка 99-107:** Отправка сообщений
   - Пользователю: понятное сообщение
   - Суперадмину: техническая информация с контекстом

## Интеграция с WG Agent

### Создание peer - в createSubscriptionForPayment()

**Файл:** `internal/telegram/admin.go:786-923`

1. **Строка 792-808:** Подключение к WG Agent
   - Настройка TLS сертификатов
   - Fallback на insecure соединение

2. **Строка 815-846:** Генерация конфигурации
   - Запрос `GeneratePeerConfig`
   - Fallback на placeholder при недоступности

3. **Строка 848-875:** Добавление peer
   - Запрос `AddPeer` с PeerID
   - Установка Keepalive = 25 секунд

4. **Строка 878-900:** Создание подписки
   - Расчет дат начала и окончания
   - Сохранение всех данных peer в `db.Subscription`

## Состояние и данные

### Глобальные переменные состояния

1. **buyStates** (`buy.go:28`) - состояния процесса покупки
2. **feedbackStates** (`user.go:69`) - состояния отправки отзывов

### Используемые сущности БД

1. **db.User** - пользователи (TgID, Username, RefCode)
2. **db.Plan** - тарифы (Name, PriceInt, DurationDays)
3. **db.Payment** - платежи (UserID, PlanID, Status, Amount)
4. **db.Subscription** - подписки (UserID, PeerID, PrivKeyEnc, PublicKey)
5. **db.Admin** - администраторы (TgID, Role, Disabled)
6. **db.PaymentMethod** - способы оплаты (PhoneNumber, Bank, OwnerName)
7. **db.Referral** - рефералы (InviterID, InviteeID)

Каждый кейс использует прямые GORM запросы к этим сущностям без дополнительных слоев абстракции.

## Дополнительные пользовательские кейсы

### Кейс 11: Реферальная система (/ref)

**Обработчик:** `handleRef()` в `internal/telegram/user.go:13-45`

**Поток выполнения:**

1. **Строка 15-20:** Получение пользователя
   - Запрос `s.repo.DB().First(&user, "tg_id = ?", msg.From.ID)`

2. **Строка 22-27:** Генерация реферального кода
   - Если `user.RefCode` пустой, генерация через `generateRefCode(user.TgID)`
   - Сохранение: `s.repo.DB().Save(&user)`

3. **Строка 29-31:** Подсчет рефералов
   - Запрос `s.repo.DB().Model(&db.Referral{}).Where("inviter_id = ?", user.TgID).Count(&referralCount)`

4. **Строка 33-45:** Формирование ответа
   - Создание ссылки `https://t.me/{bot}?start=ref_{code}`
   - Статистика приглашенных пользователей

### Кейс 12: Отзывы и обратная связь (/feedback)

**Обработчик:** `handleFeedback()` в `internal/telegram/user.go:47-68`

**Поток выполнения:**

1. **Строка 49-53:** Проверка настройки канала
   - Если `s.cfg.ReviewsChannelID` пустой, ошибка

2. **Строка 55-67:** Активация режима отзыва
   - Установка флага `feedbackStates[msg.From.ID] = true`
   - Инструкции пользователю

### Кейс 13: Обработка сообщений с отзывами

**Обработчик:** `handleFeedbackMessage()` в `internal/telegram/user.go:72-122`

**Поток выполнения:**

1. **Строка 74-78:** Проверка состояния
   - Если пользователь не в режиме отзыва, возврат
   - Очистка состояния: `delete(feedbackStates, msg.From.ID)`

2. **Строка 80-86:** Парсинг ID канала
   - Конвертация `s.cfg.ReviewsChannelID` в int64

3. **Строка 88-93:** Формирование заголовка
   - Создание header с username пользователя

4. **Строка 95-122:** Пересылка в канал
   - **Текст:** прямая пересылка с заголовком
   - **Фото:** пересылка с caption
   - **Документ:** пересылка с caption

### Кейс 14: Поддержка (/support)

**Обработчик:** `handleSupport()` в `internal/telegram/bot.go:447-465`

**Поток выполнения:**

1. **Строка 449-453:** Поиск админов поддержки
   - Запрос `s.repo.DB().Where("role = ? AND disabled = false", RoleSupport.String()).Find(&admins)`

2. **Строка 455-465:** Формирование списка
   - Для каждого админа получение username из `db.User`
   - Создание списка контактов поддержки

### Кейс 15: Просмотр тарифов (/plans)

**Обработчик:** `handlePlans()` в `internal/telegram/bot.go:363-383`

**Поток выполнения:**

1. **Строка 365-371:** Получение активных тарифов
   - Запрос `s.repo.DB().Where("archived = false").Find(&plans)`

2. **Строка 378-383:** Формирование списка
   - Цикл по тарифам с выводом Name, PriceInt, DurationDays

### Кейс 16: Включение пользователя (/enable)

**Обработчик:** `handleEnable()` в `internal/telegram/subscriptions.go:153-199`

**Поток выполнения:**

1. **Строка 154-158:** Проверка админских прав

2. **Строка 160-168:** Парсинг username

3. **Строка 170-176:** Поиск пользователя
   - Запрос с LIKE: `s.repo.DB().Where("username LIKE ?", "%"+username+"%").First(&user)`

4. **Строка 178-186:** Поиск отключенных подписок
   - Запрос `s.repo.DB().Where("user_id = ? AND active = false AND end_date > NOW()", user.TgID).Find(&subscriptions)`

5. **Строка 192-199:** Включение подписок
   - Цикл с вызовом `s.enablePeer(sub.Interface, sub.PublicKey)`
   - Обновление `active = true` в БД

## Управление способами оплаты

### Кейс 17: Добавление способа оплаты (/addpmethod)

**Обработчик:** `handleAddPaymentMethod()` в `internal/telegram/payment_methods.go:11-39`

**Поток выполнения:**

1. **Строка 12-18:** Парсинг аргументов
   - Извлечение телефона, банка, имени владельца
   - Очистка кавычек у имени: `strings.Trim(ownerName, "\"")`

2. **Строка 20-26:** Создание метода
   - Создание `db.PaymentMethod`
   - Сохранение: `s.repo.DB().Create(method)`

### Кейс 18: Список способов оплаты (/listpmethods)

**Обработчик:** `handleListPaymentMethods()` в `internal/telegram/payment_methods.go:40-61`

**Поток выполнения:**

1. **Строка 42-48:** Получение активных методов
   - Запрос `s.repo.DB().Where("archived = false").Find(&methods)`

2. **Строка 55-61:** Формирование списка
   - Цикл с выводом PhoneNumber, Bank, OwnerName

### Кейс 19: Архивирование способа оплаты (/archivepmethod)

**Обработчик:** `handleArchivePaymentMethod()` в `internal/telegram/payment_methods.go:62-90`

**Поток выполнения:**

1. **Строка 64-70:** Получение активных методов

2. **Строка 76-86:** Создание клавиатуры
   - Кнопка для каждого метода с callback `archive_pmethod_{id}`

## Callback обработчики по конфигурациям

### Получение конфигурации WireGuard

**Обработчик:** `sendConfigForPeer()` и `sendQRForPeer()` в `internal/telegram/subscriptions.go`

**Поток выполнения для конфигурации:**

1. Поиск подписки по PeerID
2. Генерация конфигурационного файла через `generateWireguardConfig()`
3. Отправка как документ с именем `{peer_id}.conf`

**Поток выполнения для QR:**

1. Поиск подписки по PeerID
2. Генерация конфигурации
3. Создание QR кода через библиотеку
4. Отправка как изображение

## Система логирования ошибок

### Детальная система ошибок

**Файл:** `internal/telegram/errors.go`

**Основные компоненты:**

1. **Строка 25-37:** Типы ошибок
   - Константы для всех типов: `ErrInvalidInput`, `ErrDatabaseError`, `ErrWGAgentError` и т.д.

2. **Строка 39-50:** Структура BotError
   - Поля: Code, Message, UserMessage, Details, Context, Timestamp, StackTrace

3. **Строка 86-107:** Основной обработчик `handleError()`
   - Определение типа ошибки
   - Отправка пользователю и суперадмину

4. **Строка 109-160:** Логирование и отчеты `logAndReportError()`
   - Структурированное логирование через slog
   - Автоматическая отправка отчетов суперадмину
   - Добавление контекста и stack trace

5. **Строка 162-210:** Фабричные методы
   - `ErrDatabasef()`, `ErrWGAgentf()`, `ErrValidationf()` и т.д.
   - Автоматическое добавление stack trace

Каждый кейс использует прямые GORM запросы к этим сущностям без дополнительных слоев абстракции.

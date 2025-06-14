#!/bin/bash

# Скрипт для удаленной отладки lime-bot
# Отправляет логи и статус в Telegram

CONTAINER_NAME="lime-bot"
LOG_FILE="/tmp/lime-bot-debug.log"
MAX_LOG_LINES=50

# Функция отправки сообщения в Telegram
send_debug_message() {
    local message="$1"
    if [ -n "$ALERT_BOT_TOKEN" ] && [ -n "$ALERT_CHAT_ID" ]; then
        curl -s -X POST "https://api.telegram.org/bot$ALERT_BOT_TOKEN/sendMessage" \
             -d "chat_id=$ALERT_CHAT_ID" \
             -d "text=$message" \
             -d "parse_mode=HTML" > /dev/null 2>&1
    fi
}

# Функция получения статуса контейнера
get_container_status() {
    if docker ps --format "table {{.Names}}\t{{.Status}}" | grep -q "$CONTAINER_NAME"; then
        echo "🟢 RUNNING"
    elif docker ps -a --format "table {{.Names}}\t{{.Status}}" | grep -q "$CONTAINER_NAME"; then
        echo "🔴 STOPPED"
    else
        echo "❌ NOT_FOUND"
    fi
}

# Функция получения логов контейнера
get_container_logs() {
    if docker ps -q -f name="$CONTAINER_NAME" > /dev/null 2>&1; then
        docker logs --tail $MAX_LOG_LINES "$CONTAINER_NAME" 2>&1
    else
        echo "Контейнер не запущен"
    fi
}

# Функция получения статуса health check
get_health_status() {
    local health_url="http://localhost:8080/health"
    if curl -s -f "$health_url" > /dev/null 2>&1; then
        echo "🟢 HEALTHY"
    else
        echo "🔴 UNHEALTHY"
    fi
}

# Основная функция отладки
debug_bot() {
    local hostname=$(hostname)
    local timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    local container_status=$(get_container_status)
    local health_status=$(get_health_status)
    
    # Получаем последние логи
    local logs=$(get_container_logs)
    
    # Формируем сообщение
    local debug_message="🔧 <b>LIME-BOT DEBUG</b>

🖥 <b>Сервер:</b> <code>$hostname</code>
🕒 <b>Время:</b> <code>$timestamp</code>
📦 <b>Контейнер:</b> $container_status
🏥 <b>Health:</b> $health_status

📋 <b>Последние логи:</b>
<pre>$(echo "$logs" | tail -20)</pre>"

    # Отправляем в Telegram
    send_debug_message "$debug_message"
    
    # Также сохраняем в файл
    echo "=== DEBUG $timestamp ===" >> "$LOG_FILE"
    echo "Container Status: $container_status" >> "$LOG_FILE"
    echo "Health Status: $health_status" >> "$LOG_FILE"
    echo "Logs:" >> "$LOG_FILE"
    echo "$logs" >> "$LOG_FILE"
    echo "" >> "$LOG_FILE"
}

# Функция для отправки переменных окружения (без токенов)
debug_env() {
    local env_info="🔧 <b>ENVIRONMENT DEBUG</b>

📋 <b>Переменные окружения:</b>
<pre>ALERT_BOT_TOKEN: ${ALERT_BOT_TOKEN:+УСТАНОВЛЕН}${ALERT_BOT_TOKEN:-НЕ УСТАНОВЛЕН}
ALERT_CHAT_ID: ${ALERT_CHAT_ID:-НЕ УСТАНОВЛЕН}
BOT_TOKEN: ${BOT_TOKEN:+УСТАНОВЛЕН}${BOT_TOKEN:-НЕ УСТАНОВЛЕН}
DB_HOST: ${DB_HOST:-НЕ УСТАНОВЛЕН}
DB_NAME: ${DB_NAME:-НЕ УСТАНОВЛЕН}</pre>"

    send_debug_message "$env_info"
}

# Функция для тестирования health_checker
test_health_checker() {
    local test_message="🧪 <b>ТЕСТ HEALTH_CHECKER</b>

Запускаем health_checker с отладкой..."
    
    send_debug_message "$test_message"
    
    # Запускаем health_checker и захватываем вывод
    local output=$(bash "$(dirname "$0")/health_checker.sh" 2>&1)
    
    local result_message="📊 <b>РЕЗУЛЬТАТ ТЕСТА:</b>
<pre>$output</pre>"
    
    send_debug_message "$result_message"
}

# Обработка аргументов
case "${1:-status}" in
    "status"|"")
        debug_bot
        ;;
    "env")
        debug_env
        ;;
    "test")
        test_health_checker
        ;;
    "logs")
        get_container_logs
        ;;
    *)
        echo "Использование: $0 [status|env|test|logs]"
        echo "  status - отправить статус и логи в Telegram (по умолчанию)"
        echo "  env    - отправить информацию о переменных окружения"
        echo "  test   - протестировать health_checker"
        echo "  logs   - показать логи в консоли"
        exit 1
        ;;
esac 
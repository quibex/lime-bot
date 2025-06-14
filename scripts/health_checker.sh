#!/bin/bash

# Health checker for lime-bot service
# Sends/updates status messages to your personal Telegram account
# Required environment variables: ALERT_BOT_TOKEN, ALERT_CHAT_ID

SERVICE_NAME="lime-bot"
HEALTH_URL="http://localhost:8080/health"
OK_MESSAGE_ID_FILE="/tmp/lime-bot-ok-message-id"
FAIL_MESSAGE_ID_FILE="/tmp/lime-bot-fail-message-id"
FAIL_START_TIME_FILE="/tmp/lime-bot-fail-start-time"
LOCK_FILE="/tmp/lime-bot-health-check.lock"
HOSTNAME=$(hostname)

# Cleanup function
cleanup() {
    rm -f "$LOCK_FILE"
    exit $1
}

# Set trap to cleanup on exit
trap 'cleanup $?' EXIT INT TERM

# Check if another instance is running
if [ -f "$LOCK_FILE" ]; then
    # Check if the process is actually running
    if kill -0 "$(cat "$LOCK_FILE")" 2>/dev/null; then
        # Another instance is running, exit silently
        exit 0
    else
        # Stale lock file, remove it
        rm -f "$LOCK_FILE"
    fi
fi

# Create lock file with current PID
echo $$ > "$LOCK_FILE"

send_message() {
    local message="$1"
    local save_to_file="$2"
    if [ -n "$ALERT_BOT_TOKEN" ] && [ -n "$ALERT_CHAT_ID" ]; then
        echo "DEBUG: Отправляем сообщение в Telegram..." >&2
        echo "DEBUG: ALERT_BOT_TOKEN установлен: ${ALERT_BOT_TOKEN:0:10}..." >&2
        echo "DEBUG: ALERT_CHAT_ID: $ALERT_CHAT_ID" >&2
        
        response=$(curl -s -X POST "https://api.telegram.org/bot$ALERT_BOT_TOKEN/sendMessage" \
                        -d "chat_id=$ALERT_CHAT_ID" \
                        -d "text=$message" \
                        -d "parse_mode=HTML")
        
        echo "DEBUG: Ответ от Telegram API: $response" >&2
        
        # Extract message_id from response and save it
        message_id=$(echo "$response" | grep -o '"message_id":[0-9]*' | cut -d':' -f2)
        if [ -n "$message_id" ] && [ -n "$save_to_file" ]; then
            echo "$message_id" > "$save_to_file"
            echo "DEBUG: Сохранен message_id: $message_id в файл: $save_to_file" >&2
        fi
    else
        echo "DEBUG: Переменные окружения не установлены!" >&2
        echo "DEBUG: ALERT_BOT_TOKEN: ${ALERT_BOT_TOKEN:-НЕ УСТАНОВЛЕН}" >&2
        echo "DEBUG: ALERT_CHAT_ID: ${ALERT_CHAT_ID:-НЕ УСТАНОВЛЕН}" >&2
    fi
}

edit_message() {
    local message="$1"
    local message_id="$2"
    if [ -n "$ALERT_BOT_TOKEN" ] && [ -n "$ALERT_CHAT_ID" ] && [ -n "$message_id" ]; then
        echo "DEBUG: Редактируем сообщение ID: $message_id" >&2
        curl -s -X POST "https://api.telegram.org/bot$ALERT_BOT_TOKEN/editMessageText" \
             -d "chat_id=$ALERT_CHAT_ID" \
             -d "message_id=$message_id" \
             -d "text=$message" \
             -d "parse_mode=HTML" > /dev/null 2>&1
        return $?
    else
        echo "DEBUG: Не могу редактировать сообщение - отсутствуют параметры" >&2
        echo "DEBUG: message_id: ${message_id:-НЕ УСТАНОВЛЕН}" >&2
    fi
    return 1
}

# Calculate downtime duration
calculate_duration() {
    local start_time="$1"
    local current_time="$2"
    local duration=$((current_time - start_time))
    
    if [ $duration -lt 60 ]; then
        echo "${duration} сек"
    elif [ $duration -lt 3600 ]; then
        echo "$((duration / 60)) мин $((duration % 60)) сек"
    else
        echo "$((duration / 3600)) ч $((duration % 3600 / 60)) мин"
    fi
}

# Main health check function
check_health() {
    curl -s -f "$HEALTH_URL" > /dev/null
    return $?
}

# Handle OK status
handle_ok_status() {
    local current_time=$(date '+%Y-%m-%d %H:%M:%S')
    local was_failing=false
    
    # Check if we're recovering from failure
    if [ -f "$FAIL_START_TIME_FILE" ]; then
        was_failing=true
        local fail_start=$(cat "$FAIL_START_TIME_FILE")
        local current_timestamp=$(date +%s)
        local downtime=$(calculate_duration "$fail_start" "$current_timestamp")
        
        # Send recovery message
        local recovery_message="🎉 <b>$SERVICE_NAME ВОССТАНОВЛЕН!</b>

🖥 Сервер: <code>$HOSTNAME</code>
🕒 Восстановлен: <code>$current_time</code>
⏱ Время простоя: <code>$downtime</code>
📊 Статус: <b>OK</b>"
        
        send_message "$recovery_message" "$OK_MESSAGE_ID_FILE"
        
        # Clean up failure files
        rm -f "$FAIL_START_TIME_FILE" "$FAIL_MESSAGE_ID_FILE"
        
        exit 0
    fi
    
    # Normal OK status - update existing message
    local ok_message="✅ <b>$SERVICE_NAME</b> работает нормально

🖥 Сервер: <code>$HOSTNAME</code>
🕒 Обновлено: <code>$current_time</code>
📊 Статус: <b>OK</b>"

    if [ -f "$OK_MESSAGE_ID_FILE" ]; then
        local message_id=$(cat "$OK_MESSAGE_ID_FILE")
        if ! edit_message "$ok_message" "$message_id"; then
            # Edit failed, send new message
            send_message "$ok_message" "$OK_MESSAGE_ID_FILE"
        fi
    else
        # No previous OK message, send new one
        send_message "$ok_message" "$OK_MESSAGE_ID_FILE"
    fi
    
    exit 0
}

# Handle FAIL status with monitoring loop
handle_fail_status() {
    local current_timestamp=$(date +%s)
    local current_time=$(date '+%Y-%m-%d %H:%M:%S')
    
    # If this is the first failure, record start time and send initial message
    if [ ! -f "$FAIL_START_TIME_FILE" ]; then
        echo "$current_timestamp" > "$FAIL_START_TIME_FILE"
        
        local initial_fail_message="🚨 <b>$SERVICE_NAME НЕДОСТУПЕН!</b>

🖥 Сервер: <code>$HOSTNAME</code>
🕒 Время ошибки: <code>$current_time</code>
❌ Статус: <b>FAILED</b>
🔗 URL: <code>$HEALTH_URL</code>"

        send_message "$initial_fail_message" "$FAIL_MESSAGE_ID_FILE"
        
        # Remove OK message ID since service is down
        rm -f "$OK_MESSAGE_ID_FILE"
    fi
    
    # Enter monitoring loop - check every 10 seconds until recovery
    local fail_start=$(cat "$FAIL_START_TIME_FILE")
    
    while true; do
        sleep 10
        
        # Check if service recovered
        if check_health; then
            handle_ok_status
            exit 0
        fi
        
        # Update failure message with current duration
        current_timestamp=$(date +%s)
        current_time=$(date '+%Y-%m-%d %H:%M:%S')
        local downtime=$(calculate_duration "$fail_start" "$current_timestamp")
        
        local updated_fail_message="🚨 <b>$SERVICE_NAME НЕДОСТУПЕН!</b>

🖥 Сервер: <code>$HOSTNAME</code>
🕒 Время ошибки: <code>$(date -d @$fail_start '+%Y-%m-%d %H:%M:%S')</code>
⏱ Простой: <code>$downtime</code>
🔄 Последняя проверка: <code>$current_time</code>
❌ Статус: <b>FAILED</b>"

        if [ -f "$FAIL_MESSAGE_ID_FILE" ]; then
            local message_id=$(cat "$FAIL_MESSAGE_ID_FILE")
            edit_message "$updated_fail_message" "$message_id"
        fi
    done
}

# Main logic
if check_health; then
    handle_ok_status
else
    handle_fail_status
fi

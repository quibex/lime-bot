#!/bin/bash

set -e

CERTS_DIR="/tmp/lime-certs"
CA_KEY="$CERTS_DIR/ca-key.pem"
CA_CERT="$CERTS_DIR/ca.pem"
CLIENT_KEY="$CERTS_DIR/lime-bot-key.pem"
CLIENT_CERT="$CERTS_DIR/lime-bot.pem"

echo "Создание временной директории..."
mkdir -p "$CERTS_DIR"

echo "Получение CA из переменных окружения..."
if [ -z "$CA_CERT_PEM" ] || [ -z "$CA_KEY_PEM" ]; then
    echo "❌ Переменные CA_CERT_PEM и CA_KEY_PEM не установлены"
    exit 1
fi

echo "$CA_CERT_PEM" | base64 -d > "$CA_CERT"
echo "$CA_KEY_PEM" | base64 -d > "$CA_KEY"
chmod 600 "$CA_KEY"
chmod 644 "$CA_CERT"

echo "Генерация клиентского сертификата для lime-bot..."
openssl genrsa -out "$CLIENT_KEY" 4096
openssl req -new -key "$CLIENT_KEY" -out "$CERTS_DIR/client.csr" \
    -subj "/C=RU/ST=Moscow/L=Moscow/O=WG-Agent/OU=Client/CN=lime-bot"

openssl x509 -req -days 365 -in "$CERTS_DIR/client.csr" -CA "$CA_CERT" -CAkey "$CA_KEY" \
    -CAcreateserial -out "$CLIENT_CERT"

echo "Очистка временных файлов..."
rm -f "$CERTS_DIR"/*.csr "$CERTS_DIR"/*.srl

chmod 600 "$CLIENT_KEY"
chmod 644 "$CLIENT_CERT" "$CA_CERT"

echo "Установка сертификатов..."
sudo mkdir -p /etc/lime-bot
sudo cp "$CLIENT_CERT" /etc/lime-bot/client.pem
sudo cp "$CLIENT_KEY" /etc/lime-bot/client-key.pem
sudo cp "$CA_CERT" /etc/lime-bot/ca.pem
sudo chmod 600 /etc/lime-bot/client-key.pem
sudo chmod 644 /etc/lime-bot/client.pem /etc/lime-bot/ca.pem

echo "Очистка временной директории..."
rm -rf "$CERTS_DIR"

echo "✅ Клиентский сертификат установлен в /etc/lime-bot/" 
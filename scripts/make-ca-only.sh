#!/bin/bash

set -e

CERTS_DIR="certs"
CA_KEY="$CERTS_DIR/ca-key.pem"
CA_CERT="$CERTS_DIR/ca.pem"

echo "Создание директории для сертификатов..."
mkdir -p "$CERTS_DIR"

if [ -f "$CA_CERT" ] && [ -f "$CA_KEY" ]; then
    echo "⚠️  CA уже существует. Удалите certs/ если хотите пересоздать."
    exit 1
fi

echo "Генерация CA (Certificate Authority)..."
openssl genrsa -out "$CA_KEY" 4096
openssl req -new -x509 -days 3650 -key "$CA_KEY" -out "$CA_CERT" \
    -subj "/C=RU/ST=Moscow/L=Moscow/O=WG-Agent/OU=CA/CN=wg-agent-ca"

chmod 600 "$CA_KEY"
chmod 644 "$CA_CERT"

echo ""
echo "✅ CA создан:"
echo "  📁 CA Cert: $CA_CERT"
echo "  🔑 CA Key:  $CA_KEY"
echo ""
echo "🚨 ВАЖНО: Добавьте эти файлы в GitHub Secrets:"
echo "  CA_CERT_PEM: содержимое файла $CA_CERT"
echo "  CA_KEY_PEM:  содержимое файла $CA_KEY"
echo ""
echo "Команды для копирования в GitHub Secrets:"
echo "  cat $CA_CERT | base64"
echo "  cat $CA_KEY | base64" 
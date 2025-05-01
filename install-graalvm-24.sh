#!/bin/bash

# Скрипт для скачивания и установки GraalVM 24.0.1 для Java 24

echo "Установка GraalVM 24.0.1 для Java 24..."

# Создаем директорию для установки
INSTALL_DIR="$HOME/.jdks/graalvm-jdk-24.0.1"
mkdir -p "$INSTALL_DIR"

# Скачиваем GraalVM
# Используем скрипт-френдли URL из документации GraalVM
echo "Скачивание GraalVM 24.0.1..."
curl -L https://download.oracle.com/graalvm/24/latest/graalvm-jdk-24_linux-x64_bin.tar.gz -o /tmp/graalvm-24.tar.gz

# Распаковываем архив
echo "Распаковка архива..."
tar -xzf /tmp/graalvm-24.tar.gz -C /tmp

# Перемещаем файлы в директорию установки
echo "Установка в $INSTALL_DIR..."
mv /tmp/graalvm-jdk-24* $INSTALL_DIR/

# Удаляем временные файлы
rm /tmp/graalvm-24.tar.gz

echo "Установка завершена!"
echo "Чтобы использовать GraalVM в текущей сессии, выполните:"
echo 'export JAVA_HOME="'$INSTALL_DIR'"'
echo 'export PATH="$JAVA_HOME/bin:$PATH"'
echo ""
echo "Чтобы использовать его постоянно, добавьте эти строки в ваш ~/.bashrc или ~/.zshrc файл."
echo ""
echo "Для настройки Cursor/VSCode, укажите путь к GraalVM в .vscode/settings.json:"
echo '    "java.configuration.runtimes": ['
echo '        {'
echo '            "name": "JavaSE-24",'
echo '            "path": "'$INSTALL_DIR'",'
echo '            "default": true'
echo '        }'
echo '    ],'
echo '    "java.jdt.ls.java.home": "'$INSTALL_DIR'",'
echo '    "spring-boot.ls.java.home": "'$INSTALL_DIR'"'

#!/bin/bash

# Скрипт для установки SDKMAN и GraalVM для Java 24

echo "Установка SDKMAN..."

# Установка SDKMAN
curl -s "https://get.sdkman.io" | bash
source "$HOME/.sdkman/bin/sdkman-init.sh"

# Проверка установки
sdk version

# Вывод доступных версий Java
echo "Доступные версии Java:"
sdk list java | grep -E "graal|24"

# Установка GraalVM для Java 24
echo "Установка GraalVM для Java 24..."
sdk install java 24.0.1-graal

# Проверка
java -version

echo "Установка завершена!"
echo "Чтобы использовать SDKMAN, перезапустите терминал или выполните:"
echo "source \"$HOME/.sdkman/bin/sdkman-init.sh\""
echo ""
echo "Для автоматического переключения в этом проекте выполните:"
echo "sdk env"

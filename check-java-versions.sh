#!/bin/bash

echo "Проверка версий Java..."
echo "----------------------"

echo "1. Версия Java по умолчанию:"
java -version
echo ""

echo "2. Установленные версии Java в /usr/lib/jvm:"
find /usr/lib/jvm -name "release" | while read file; do
    JDK_PATH=$(dirname "$file")
    if [ -x "$JDK_PATH/bin/java" ]; then
        echo "  - $JDK_PATH:"
        "$JDK_PATH/bin/java" -version 2>&1 | sed 's/^/    /'
        echo ""
    fi
done

echo "3. Установленные версии GraalVM:"
find $HOME/.jdks -name "release" | grep -i graal | while read file; do
    JDK_PATH=$(dirname "$file")
    if [ -x "$JDK_PATH/bin/java" ]; then
        echo "  - $JDK_PATH:"
        "$JDK_PATH/bin/java" -version 2>&1 | sed 's/^/    /'
        echo ""
    fi
done

echo "4. Рекомендации:"
echo "   Для использования Java 24 с виртуальными потоками,"
echo "   рекомендуется установить GraalVM 24.0.1 для Java 24"
echo "   с помощью скрипта install-graalvm-24.sh"
echo ""
echo "   После установки настройте Cursor/VSCode следуя инструкциям"
echo "   в README.md файле."

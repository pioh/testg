#!/bin/bash

# Конфигурация приложений для тестирования
# Формат: [имя_приложения]="команда_запуска"
declare -A APPS

# Java с виртуальными потоками (Virtual Threads)
APPS["java-virtualThreads"]="./gradlew build -x test && java -jar build/libs/*.jar --thread-model=virtual"

# Java с фиксированным пулом потоков (24 потока)
APPS["java-fixedThreads24"]="./gradlew build -x test && java -jar build/libs/*.jar --thread-model=fixed --thread-count=24"

# Java с фиксированным пулом потоков (1024 потока)
APPS["java-fixedThreads1024"]="./gradlew build -x test && java -jar build/libs/*.jar --thread-model=fixed --thread-count=1024"

# Java с фиксированным пулом потоков (24) и асинхронным NIO
APPS["java-fixedThreads24AsyncNIO"]="./gradlew build -x test && java -jar build/libs/*.jar --thread-model=asyncNio --thread-count=24 --app.async-nio=true"

# GraalVM Native Image с виртуальными потоками
APPS["graalvm-native-virtualThreads"]="./gradlew nativeCompile -x test --parallel && ./build/native/nativeCompile/test-app -Xmx8g --thread-model=virtual"

# GraalVM Native Image с фиксированным пулом потоков (24 потока)
APPS["graalvm-native-fixedThreads24"]="./gradlew nativeCompile -x test --parallel && ./build/native/nativeCompile/test-app -Xmx8g --thread-model=fixed --thread-count=24"

# GraalVM Native Image с фиксированным пулом потоков (1024 потока)
APPS["graalvm-native-fixedThreads1024"]="./gradlew nativeCompile -x test --parallel && ./build/native/nativeCompile/test-app -Xmx8g --thread-model=fixed --thread-count=1024"

# GraalVM Native Image с фиксированным пулом потоков (24) и асинхронным NIO
APPS["graalvm-native-fixedThreads24AsyncNIO"]="./gradlew nativeCompile -x test --parallel && ./build/native/nativeCompile/test-app -Xmx8g --thread-model=asyncNio --thread-count=24 --app.async-nio=true"

# Go FastHTTP приложение
APPS["go-fasthttp"]="mkdir -p build && CGO_ENABLED=0 go build -ldflags=\"-s -w\" -trimpath -tags=netgo,osusergo -o build/app *.go && ./build/app --server-type=fasthttp"

# Go стандартный HTTP сервер
APPS["go-stdlib"]="mkdir -p build && CGO_ENABLED=0 go build -ldflags=\"-s -w\" -trimpath -tags=netgo,osusergo -o build/app *.go && ./build/app --server-type=stdlib"

# Go FastHTTP с GOMAXPROCS=1
APPS["go-fasthttp-maxprocs1"]="mkdir -p build && CGO_ENABLED=0 go build -ldflags=\"-s -w\" -trimpath -tags=netgo,osusergo -o build/app *.go && GOMAXPROCS=1 ./build/app --server-type=fasthttp"

# Go стандартный HTTP с GOMAXPROCS=1
APPS["go-stdlib-maxprocs1"]="mkdir -p build && CGO_ENABLED=0 go build -ldflags=\"-s -w\" -trimpath -tags=netgo,osusergo -o build/app *.go && GOMAXPROCS=1 ./build/app --server-type=stdlib"

# Экспортируем массив для использования в app-manager.sh
export APPS

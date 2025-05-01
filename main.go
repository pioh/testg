package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"go.uber.org/zap"
)

// Доступные типы серверов
const (
	ServerTypeFastHTTP = "fasthttp"
	ServerTypeStdlib   = "stdlib"
)

func main() {
	// Парсинг аргументов командной строки
	serverType := flag.String("server-type", ServerTypeFastHTTP, "Тип сервера (fasthttp или stdlib)")
	flag.Parse()

	// Настройка логгера
	cfg := zap.NewProductionConfig()
	cfg.Encoding = "console"
	cfg.Level = zap.NewAtomicLevelAt(zap.ErrorLevel) // Только ошибки для максимальной производительности
	logger, _ := cfg.Build()
	defer logger.Sync()

	// Информация о запуске
	logger.Info("Starting server",
		zap.String("server_type", *serverType),
		zap.Int("gomaxprocs", runtime.GOMAXPROCS(0)),
		zap.Int("cpu_cores", runtime.NumCPU()))

	// Запуск соответствующего типа сервера
	switch *serverType {
	case ServerTypeFastHTTP:
		if err := StartFastHTTPServer(logger); err != nil {
			logger.Error("Failed to start FastHTTP server", zap.Error(err))
			os.Exit(1)
		}
	case ServerTypeStdlib:
		if err := StartStdlibServer(logger); err != nil {
			logger.Error("Failed to start standard HTTP server", zap.Error(err))
			os.Exit(1)
		}
	default:
		logger.Error("Unknown server type", zap.String("server_type", *serverType))
		fmt.Printf("Неизвестный тип сервера: %s. Доступные типы: fasthttp, stdlib\n", *serverType)
		os.Exit(1)
	}
}

package main

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
)

// Глобальный клиент для всех запросов fasthttp
var fasthttpClient = &fasthttp.Client{
	MaxConnsPerHost:               1000000,
	MaxIdleConnDuration:           3600 * time.Second, // Увеличиваем до 1 часа
	MaxConnWaitTimeout:            60 * time.Second,   // Увеличиваем время ожидания
	ReadTimeout:                   120 * time.Second,  // Увеличиваем таймаут чтения
	WriteTimeout:                  120 * time.Second,  // Увеличиваем таймаут записи
	MaxResponseBodySize:           100 * 1024 * 1024,  // 100 МБ
	MaxIdemponentCallAttempts:     8,
	DisableHeaderNamesNormalizing: true,
	NoDefaultUserAgentHeader:      true,
	DisablePathNormalizing:        true,
	DialDualStack:                 true,
	ReadBufferSize:                64 * 1024, // Увеличиваем размер буфера чтения
	WriteBufferSize:               64 * 1024, // Увеличиваем размер буфера записи
}

// Пул объектов для повторного использования в fasthttp
var (
	fasthttpRequestPool  sync.Pool
	fasthttpResponsePool sync.Pool
)

func init() {
	// Инициализация пулов объектов для fasthttp
	fasthttpRequestPool = sync.Pool{
		New: func() interface{} {
			return fasthttp.AcquireRequest()
		},
	}

	fasthttpResponsePool = sync.Pool{
		New: func() interface{} {
			return fasthttp.AcquireResponse()
		},
	}
}

// Функция isConnectionClosed проверяет, является ли ошибка закрытием соединения
func isConnectionClosed(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "connection closed") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "reset by peer") ||
		strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "server closed connection")
}

// StartFastHTTPServer запускает сервер на базе fasthttp
func StartFastHTTPServer(logger *zap.Logger) error {
	counter := int64(0)
	errCounter := int64(0)
	logger.Info("Starting FastHTTP server on port 8080")

	// Оптимизированный обработчик запросов
	requestHandler := func(ctx *fasthttp.RequestCtx) {
		// Получаем путь запроса
		path := string(ctx.Path())

		// Обработка корневого пути
		if path == "/" || path == "" {
			ctx.WriteString("API is running. Use /check?left=N to start a test.")
			return
		}

		// Обработка пути /check
		if path == "/check" {
			// Увеличиваем счетчик атомарно
			atomic.AddInt64(&counter, 1)

			// Получаем параметры left и rnd
			leftStr := string(ctx.QueryArgs().Peek("left"))
			rndStr := string(ctx.QueryArgs().Peek("rnd"))

			// Если left не указан, используем 0
			left := 0
			if leftStr != "" {
				var err error
				left, err = strconv.Atoi(leftStr)
				if err != nil {
					logger.Error("Invalid left parameter", zap.String("left", leftStr), zap.Error(err))
					ctx.Error("Invalid left parameter", fasthttp.StatusBadRequest)
					return
				}
			}

			// Если rnd не указан, генерируем случайное число
			if rndStr == "" {
				rndStr = strconv.Itoa(rand.Intn(100000))
			}

			// Если left > 0, делаем рекурсивный вызов
			if left > 0 {
				// Защита от слишком глубокой рекурсии
				if left > 1000 {
					logger.Warn("Excessive recursion depth limited", zap.Int("left", left))
					left = 1000 // Ограничиваем глубину рекурсии
				}

				// Используем пул объектов
				req := fasthttpRequestPool.Get().(*fasthttp.Request)
				resp := fasthttpResponsePool.Get().(*fasthttp.Response)

				defer func() {
					// Обработка паники в рекурсивном вызове
					if r := recover(); r != nil {
						atomic.AddInt64(&errCounter, 1)
						logger.Error("Recovered from panic in recursive call", zap.Any("panic", r))
						ctx.Error("Internal Server Error from panic", fasthttp.StatusInternalServerError)
					}
					// Возвращаем объекты в пул даже при панике
					fasthttpRequestPool.Put(req)
					fasthttpResponsePool.Put(resp)
				}()

				// Формируем URL для рекурсивного вызова
				nextURL := fmt.Sprintf("http://127.0.0.1:8080/check?left=%d&rnd=%s", left-1, rndStr)
				req.SetRequestURI(nextURL)

				// Устанавливаем keep-alive
				req.Header.Set("Connection", "keep-alive")

				// Выполняем запрос с повторными попытками для ошибок соединения
				var err error
				maxRetries := 3

				for retry := 0; retry <= maxRetries; retry++ {
					err = fasthttpClient.Do(req, resp)
					if err == nil {
						break
					}

					// Если ошибка не связана с соединением, прекращаем попытки
					if !isConnectionClosed(err) {
						break
					}

					// Логируем попытку, но только если это не последняя попытка
					if retry < maxRetries {
						logger.Warn("Retry recursive call after connection error",
							zap.Int("retry", retry+1),
							zap.String("url", nextURL),
							zap.String("error", err.Error()))
					}
				}

				if err != nil {
					atomic.AddInt64(&errCounter, 1)
					if errMsg := err.Error(); len(errMsg) > 100 {
						errMsg = errMsg[:100] + "..." // Ограничиваем размер сообщения для логов
					}
					logger.Error("Error making recursive call",
						zap.String("url", nextURL),
						zap.String("error", err.Error()))

					// Возвращаем ошибку клиенту, но создаем ответ JSON с ошибкой
					ctx.Response.Reset()
					ctx.Response.SetStatusCode(fasthttp.StatusOK) // Возвращаем 200 OK
					ctx.Response.Header.Set("Content-Type", "application/json")
					ctx.Response.Header.Set("Connection", "keep-alive")
					ctx.WriteString(fmt.Sprintf(`{"rnd": "%s", "error": "Internal recursion error"}`, rndStr))
					return
				}

				// Проверяем статус ответа
				if resp.StatusCode() != fasthttp.StatusOK {
					atomic.AddInt64(&errCounter, 1)
					logger.Error("Non-OK status from recursive call",
						zap.Int("status", resp.StatusCode()),
						zap.String("url", nextURL),
						zap.String("body", string(resp.Body())))

					// Возвращаем ошибку клиенту, но создаем ответ JSON с ошибкой
					ctx.Response.Reset()
					ctx.Response.SetStatusCode(fasthttp.StatusOK) // Возвращаем 200 OK
					ctx.Response.Header.Set("Content-Type", "application/json")
					ctx.Response.Header.Set("Connection", "keep-alive")
					ctx.Response.Header.Set("Keep-Alive", "timeout=3600, max=10000")
					ctx.Response.Header.Set("X-Content-Type-Options", "nosniff")
					ctx.WriteString(fmt.Sprintf(`{"rnd": "%s", "error": "Recursive call returned non-OK status"}`, rndStr))
					return
				}

				// Устанавливаем все заголовки ответа до копирования тела
				ctx.Response.Header.VisitAll(func(k, v []byte) {
					ctx.Response.Header.Set(string(k), string(v))
				})

				// Явно устанавливаем критически важные заголовки в любом случае
				ctx.Response.Header.Set("Connection", "keep-alive")
				ctx.Response.Header.Set("Keep-Alive", "timeout=3600, max=10000")
				ctx.Response.Header.Set("Content-Type", string(ctx.Response.Header.ContentType()))

				// Возвращаем ответ
				ctx.Response.SetStatusCode(resp.StatusCode())
				ctx.Response.SetBody(resp.Body())
			} else {
				// Если left = 0, возвращаем rnd
				ctx.Response.Header.Set("Content-Type", "application/json")
				ctx.Response.Header.Set("Connection", "keep-alive")
				ctx.Response.Header.Set("Keep-Alive", "timeout=3600, max=10000")
				ctx.Response.Header.Set("X-Content-Type-Options", "nosniff")
				ctx.WriteString(fmt.Sprintf(`{"rnd": "%s"}`, rndStr))
			}

			return
		}

		// Обработка остальных путей
		ctx.Error("Not Found", fasthttp.StatusNotFound)
	}

	// Создаем промежуточное ПО с восстановлением от паники
	panicRecoveryHandler := func(ctx *fasthttp.RequestCtx) {
		defer func() {
			if r := recover(); r != nil {
				atomic.AddInt64(&errCounter, 1)
				logger.Error("Recovered from panic in request handler", zap.Any("panic", r))
				ctx.Error("Internal Server Error from panic", fasthttp.StatusInternalServerError)
			}
		}()

		// Вызываем основной обработчик
		requestHandler(ctx)
	}

	// Оптимизированные настройки сервера
	server := &fasthttp.Server{
		Handler:                       panicRecoveryHandler,
		Name:                          "OptimizedServer",
		Concurrency:                   1000000,
		ReadTimeout:                   120 * time.Second, // Увеличиваем время чтения
		WriteTimeout:                  120 * time.Second, // Увеличиваем время записи
		MaxRequestBodySize:            10 * 1024 * 1024,  // 10 МБ
		NoDefaultServerHeader:         true,
		ReduceMemoryUsage:             true,
		TCPKeepalive:                  true,
		DisableKeepalive:              false,              // Явно включаем keepalive
		MaxKeepaliveDuration:          3600 * time.Second, // Увеличиваем до 1 часа
		LogAllErrors:                  false,
		DisableHeaderNamesNormalizing: true,
		GetOnly:                       false,
		KeepHijackedConns:             false,
		Logger:                        nil,
		ReadBufferSize:                64 * 1024, // Увеличиваем размер буфера чтения
		WriteBufferSize:               64 * 1024, // Увеличиваем размер буфера записи
	}

	// Запускаем сервер
	return server.ListenAndServe("127.0.0.1:8080")
}

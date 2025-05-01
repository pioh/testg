package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// Глобальный HTTP клиент для стандартной библиотеки
var stdlibHTTPClient = &http.Client{
	Timeout: 120 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:          100000,
		MaxIdleConnsPerHost:   100000,
		MaxConnsPerHost:       100000,
		IdleConnTimeout:       3600 * time.Second, // Увеличиваем до 1 часа
		DisableCompression:    true,
		DisableKeepAlives:     false,
		ResponseHeaderTimeout: 120 * time.Second,
		ExpectContinueTimeout: 30 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   60 * time.Second,
			KeepAlive: 3600 * time.Second, // Увеличиваем до 1 часа
			DualStack: true,
		}).DialContext,
		ForceAttemptHTTP2:   false,
		TLSHandshakeTimeout: 20 * time.Second,
		ReadBufferSize:      64 * 1024,
		WriteBufferSize:     64 * 1024,
	},
}

// isStdlibConnectionClosed проверяет, связана ли ошибка с закрытием соединения
func isStdlibConnectionClosed(err error) bool {
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

// SafeJSONResponse отправляет безопасный JSON-ответ, даже в случае ошибок
func SafeJSONResponse(w http.ResponseWriter, rndStr string, err string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Keep-Alive", "timeout=3600, max=10000")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK) // Всегда возвращаем 200 OK

	response := map[string]string{"rnd": rndStr}
	if err != "" {
		response["error"] = err
	}

	// Игнорируем ошибку кодирования, в крайнем случае просто напишем строку
	if encodeErr := json.NewEncoder(w).Encode(response); encodeErr != nil {
		w.Write([]byte(fmt.Sprintf(`{"rnd":"%s","error":"%s"}`, rndStr, "Error encoding response")))
	}
}

// StartStdlibServer запускает сервер с использованием стандартной библиотеки net/http
func StartStdlibServer(logger *zap.Logger) error {
	counter := int64(0)
	errCounter := int64(0)
	logger.Info("Starting standard HTTP server on port 8080")

	// Сбрасываем дефолтные маршруты
	http.DefaultServeMux = http.NewServeMux()

	// Создаем обработчик для восстановления от паник
	panicRecoveryMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rvr := recover(); rvr != nil {
					atomic.AddInt64(&errCounter, 1)
					logger.Error("Recovered from panic in request", zap.Any("panic", rvr))

					// Возвращаем безопасный ответ даже в случае паники
					query := r.URL.Query()
					rndStr := query.Get("rnd")
					if rndStr == "" {
						rndStr = strconv.Itoa(rand.Intn(100000))
					}

					SafeJSONResponse(w, rndStr, "Internal server panic")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}

	// Обработчик для корневого пути
	rootHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Connection", "keep-alive")
		w.Write([]byte("API is running. Use /check?left=N to start a test."))
	})

	// Обработчик для /check
	checkHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Увеличиваем счетчик атомарно
		atomic.AddInt64(&counter, 1)

		// Получаем параметры left и rnd
		query := r.URL.Query()
		leftStr := query.Get("left")
		rndStr := query.Get("rnd")

		// Если left не указан, используем 0
		left := 0
		if leftStr != "" {
			var err error
			left, err = strconv.Atoi(leftStr)
			if err != nil {
				logger.Error("Invalid left parameter", zap.String("left", leftStr), zap.Error(err))
				SafeJSONResponse(w, rndStr, "Invalid left parameter")
				return
			}
		}

		// Ограничиваем глубину рекурсии
		if left > 1000 {
			logger.Warn("Excessive recursion depth limited", zap.Int("left", left))
			left = 1000
		}

		// Если rnd не указан, генерируем случайное число
		if rndStr == "" {
			rndStr = strconv.Itoa(rand.Intn(100000))
		}

		// Если left > 0, делаем рекурсивный вызов
		if left > 0 {
			// Формируем URL для рекурсивного вызова
			nextURL := fmt.Sprintf("http://127.0.0.1:8080/check?left=%d&rnd=%s", left-1, rndStr)

			// Создаем запрос с контекстом и таймаутом
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			req, reqErr := http.NewRequestWithContext(ctx, "GET", nextURL, nil)
			if reqErr != nil {
				atomic.AddInt64(&errCounter, 1)
				logger.Error("Error creating request", zap.Error(reqErr))
				SafeJSONResponse(w, rndStr, "Error creating request")
				return
			}

			// Устанавливаем заголовок keep-alive
			req.Header.Set("Connection", "keep-alive")

			// Выполняем запрос с повторными попытками для ошибок соединения
			var resp *http.Response
			var err error
			maxRetries := 3

			for retry := 0; retry <= maxRetries; retry++ {

				resp, err = stdlibHTTPClient.Do(req)
				if err == nil {
					break
				}

				// Если ошибка не связана с соединением, прекращаем попытки
				if !isStdlibConnectionClosed(err) {
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
				SafeJSONResponse(w, rndStr, "Error making recursive call")
				return
			}
			defer resp.Body.Close()

			// Проверяем статус ответа
			if resp.StatusCode != http.StatusOK {
				atomic.AddInt64(&errCounter, 1)
				logger.Error("Non-OK status from recursive call",
					zap.Int("status", resp.StatusCode),
					zap.String("url", nextURL))

				// Читаем тело ответа для логирования
				body, _ := io.ReadAll(resp.Body)
				if len(body) > 100 {
					logger.Error("Error response body", zap.String("body", string(body[:100])+"..."))
				} else if len(body) > 0 {
					logger.Error("Error response body", zap.String("body", string(body)))
				}

				SafeJSONResponse(w, rndStr, "Recursive call returned non-OK status")
				return
			}

			// Читаем тело ответа
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				atomic.AddInt64(&errCounter, 1)
				logger.Error("Error reading response body", zap.Error(err))
				SafeJSONResponse(w, rndStr, "Error reading response body")
				return
			}

			// Явно устанавливаем все критические заголовки перед отправкой
			w.Header().Set("Connection", "keep-alive")
			w.Header().Set("Keep-Alive", "timeout=3600, max=10000")
			w.Header().Set("X-Content-Type-Options", "nosniff")

			// Копируем заголовки из ответа
			for key, values := range resp.Header {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}

			// Гарантируем, что Content-Type установлен
			if w.Header().Get("Content-Type") == "" {
				w.Header().Set("Content-Type", "application/json")
			}

			// Устанавливаем статус код и отправляем тело
			w.WriteHeader(resp.StatusCode)

			// Отправляем тело с повторными попытками при необходимости
			written, err := w.Write(body)
			if err != nil {
				logger.Error("Error writing response",
					zap.Error(err),
					zap.Int("written_bytes", written),
					zap.Int("body_size", len(body)))
			} else if written != len(body) {
				logger.Error("Incomplete response write",
					zap.Int("written_bytes", written),
					zap.Int("body_size", len(body)))
			}
		} else {
			// Если left = 0, возвращаем rnd в формате JSON
			SafeJSONResponse(w, rndStr, "")
		}
	})

	// Регистрируем обработчики с промежуточным ПО
	http.Handle("/", panicRecoveryMiddleware(rootHandler))
	http.Handle("/check", panicRecoveryMiddleware(checkHandler))

	// Запускаем сервер с оптимизированными настройками
	server := &http.Server{
		Addr:              "127.0.0.1:8080",
		ReadTimeout:       120 * time.Second,
		WriteTimeout:      120 * time.Second,
		ReadHeaderTimeout: 60 * time.Second,
		IdleTimeout:       3600 * time.Second, // Увеличиваем до 1 часа
		MaxHeaderBytes:    1 << 20,            // 1 МБ
		// Не устанавливаем Handler, т.к. используем глобальный http.DefaultServeMux
	}

	// Запускаем сервер
	return server.ListenAndServe()
}

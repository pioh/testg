package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
	"github.com/valyala/fasthttp"
)

// Мьютекс для доступа к файлам результатов
var fileMutex sync.Mutex

// isConnectionClosed проверяет, связана ли ошибка с закрытием соединения
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

// NewTester создает новый tester
func NewTester(baseURL string, testDuration time.Duration, maxParallelism, maxDepth int, resultsFile, fullStatsFile, appManagerPath, appName string) *Tester {
	// Устанавливаем максимальное количество процессоров
	runtime.GOMAXPROCS(runtime.NumCPU())

	return &Tester{
		BaseURL:        baseURL,
		TestDuration:   testDuration,
		MaxParallelism: maxParallelism,
		MaxDepth:       maxDepth,
		ResultsFile:    resultsFile,
		FullStatsFile:  fullStatsFile,
		AppManagerPath: appManagerPath,
		AppName:        appName,
		ResourceStats:  NewResourceStats(),
		Client: &fasthttp.Client{
			MaxConnsPerHost:               1000000,            // Увеличено до 1 миллиона
			MaxIdleConnDuration:           3600 * time.Second, // 1 час
			ReadTimeout:                   120 * time.Second,
			WriteTimeout:                  120 * time.Second,
			MaxConnWaitTimeout:            60 * time.Second,
			MaxResponseBodySize:           100 * 1024 * 1024, // 100 МБ
			MaxIdemponentCallAttempts:     8,
			DisableHeaderNamesNormalizing: true,
			NoDefaultUserAgentHeader:      true,
			Dial: (&fasthttp.TCPDialer{
				Concurrency:      maxParallelism, // Устанавливаем на основе параллелизма
				DNSCacheDuration: 10 * time.Minute,
			}).Dial,
			DialDualStack:   true,      // Добавляем поддержку двойного стека (IPv4/IPv6)
			ReadBufferSize:  64 * 1024, // Увеличиваем размер буфера чтения
			WriteBufferSize: 64 * 1024, // Увеличиваем размер буфера записи
		},
		SecondBySecond:         make([]SecondStat, 0, 500), // Увеличиваем вместимость
		StopResourceCollection: make(chan struct{}),
	}
}

// RunMatrixTest запускает тестирование для конкретной комбинации параллелизма и глубины
func (t *Tester) RunMatrixTest(parallelism, depth int) *TestResult {
	color.Cyan("=== Тест с параллельностью=%d, глубиной=%d ===", parallelism, depth)

	// Запускаем приложение перед тестом
	if err := t.StartApplication(); err != nil {
		log.Printf("Ошибка запуска приложения: %v", err)
		return nil
	}
	log.Printf("Приложение успешно запущено, переходим к тестированию")
	defer t.StopApplication()

	fmt.Printf("Запуск теста на %s...\n", t.TestDuration)

	// Инициализируем метрики
	latency := NewLatencyMetric()
	var requests, success, failures, mismatches int64

	// Добавляем счетчик успешных обработок для первой пачки
	var firstBatchCompleted atomic.Int64
	var firstBatchSize int64 = int64(parallelism)
	var firstBatchStart time.Time
	var firstBatchStartSet int64

	// Время начала теста для корректного расчета FirstResp
	testStart := time.Now()

	// Настройка буферов каналов в зависимости от параллелизма
	bufferSize := parallelism * 4
	if bufferSize > 1000000 {
		bufferSize = 1000000 // Ограничиваем максимальный размер буфера
	}

	// Создаем каналы для работы с буфером
	workCh := make(chan struct{}, bufferSize)

	// Создаем контекст с отменой для корректной остановки всех запросов
	ctx, cancel := context.WithTimeout(context.Background(), t.TestDuration*10)
	defer cancel() // Гарантируем отмену контекста при выходе из функции

	// Ограничиваем вывод логов для высокой параллельности
	logThreshold := 1000
	if parallelism > 10000 {
		logThreshold = 100000 // Реже логировать при очень высокой параллельности
	}

	// Используем пулы объектов для улучшения производительности
	reqPool := &sync.Pool{
		New: func() interface{} {
			return fasthttp.AcquireRequest()
		},
	}
	respPool := &sync.Pool{
		New: func() interface{} {
			return fasthttp.AcquireResponse()
		},
	}

	// Инициализируем случайный генератор для каждой горутины
	randSource := rand.NewSource(time.Now().UnixNano())

	// Запускаем воркеры в соответствии с параллелизмом
	var wg sync.WaitGroup
	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// Создаем локальный генератор случайных чисел для каждой горутины
			localRand := rand.New(randSource)

			var localStartedOnce bool

			// Предварительно формируем базовый URL для ускорения
			baseURLWithPath := t.BaseURL + "/check?left=" + strconv.Itoa(depth) + "&rnd="

			for {
				select {
				case <-workCh:
					// Проверяем отмену контекста
					if ctx.Err() != nil {
						return
					}

					// При первом реальном запросе запускаем часы для пачки
					if !localStartedOnce {
						if atomic.CompareAndSwapInt64(&firstBatchStartSet, 0, 1) {
							firstBatchStart = time.Now() // первый реальный выстрел
						}
						localStartedOnce = true
					}

					// Выполняем запрос
					start := time.Now()
					randomNum := localRand.Intn(1000000)
					url := baseURLWithPath + strconv.Itoa(randomNum)

					// Получаем объекты из пула
					req := reqPool.Get().(*fasthttp.Request)
					resp := respPool.Get().(*fasthttp.Response)

					// Возвращаем объекты в пул после использования
					defer func() {
						req.Reset()
						resp.Reset()
						reqPool.Put(req)
						respPool.Put(resp)
					}()

					req.SetRequestURI(url)

					// Добавляем заголовки для улучшения обработки соединений
					req.Header.Set("Connection", "keep-alive")
					req.Header.Set("Keep-Alive", "timeout=3600, max=10000")

					// Выполняем запрос с таймаутом, подходящим для глубины запроса
					var err error
					maxRetries := 2 // Максимум 2 повторные попытки

					// Определяем, нужно ли логировать ошибки
					logErrors := true
					if requests > int64(logThreshold) && (requests%int64(logThreshold) != 0) {
						logErrors = false
					}

					var didCall bool
					for retry := 0; retry <= maxRetries; retry++ {
						// Проверяем отмену контекста перед выполнением запроса
						if ctx.Err() != nil {
							return
						}

						if depth > 64 {
							// Для большой глубины используем более длительный таймаут
							// Увеличиваем таймаут пропорционально глубине, но не меньше 5 секунд
							timeout := time.Duration(depth) * 100 * time.Millisecond
							if timeout < 5*time.Second {
								timeout = 5 * time.Second
							}
							err = t.Client.DoTimeout(req, resp, timeout)
						} else {
							err = t.Client.Do(req, resp)
						}

						// Если нет ошибки или ошибка не связана с соединением, выходим из цикла
						if err == nil || !isConnectionClosed(err) {
							didCall = true
							break
						}

						// Проверяем отмену контекста после выполнения запроса
						if ctx.Err() != nil {
							return
						}
					}

					if didCall {
						atomic.AddInt64(&requests, 1) // запрос действительно ушёл в сеть
					}

					duration := time.Since(start)

					if err != nil {
						if logErrors {
							log.Printf("Ошибка запроса: %v", err)
						}
						atomic.AddInt64(&failures, 1)
						// Инкрементируем счётчик завершённых запросов для первой пачки при любом результате
						cur := firstBatchCompleted.Add(1)
						if cur == firstBatchSize && latency.FirstBatchTime == 0 {
							latency.FirstBatchTime = time.Since(firstBatchStart)
						}
						continue
					}

					body := resp.Body()
					if resp.StatusCode() != 200 {
						if logErrors {
							log.Printf("Неуспешный статус: %d", resp.StatusCode())
						}
						atomic.AddInt64(&failures, 1)
						// Инкрементируем счётчик завершённых запросов для первой пачки при любом результате
						cur := firstBatchCompleted.Add(1)
						if cur == firstBatchSize && latency.FirstBatchTime == 0 {
							latency.FirstBatchTime = time.Since(firstBatchStart)
						}
					} else if strings.Contains(string(body), strconv.Itoa(randomNum)) {
						atomic.AddInt64(&success, 1)

						// Первый успешный ответ - фиксируем время от начала теста
						if latency.FirstResp == 0 {
							latency.FirstResp = time.Since(testStart)
						}

						// Инкрементируем счётчик завершённых запросов для первой пачки
						cur := firstBatchCompleted.Add(1)
						if cur == firstBatchSize && latency.FirstBatchTime == 0 {
							latency.FirstBatchTime = time.Since(firstBatchStart)
							log.Printf("Время выполнения первой пачки (%d запросов): %s",
								firstBatchSize, latency.FirstBatchTime)
						}
					} else {
						if logErrors {
							log.Printf("Несовпадение ответа: %s (ожидался %d)", string(body), randomNum)
						}
						atomic.AddInt64(&mismatches, 1)
						// Инкрементируем счётчик завершённых запросов для первой пачки при любом результате
						cur := firstBatchCompleted.Add(1)
						if cur == firstBatchSize && latency.FirstBatchTime == 0 {
							latency.FirstBatchTime = time.Since(firstBatchStart)
						}
					}

					// Добавляем образец времени только для успешных запросов
					if resp.StatusCode() == 200 && err == nil {
						latency.AddSample(duration)
					}

				case <-ctx.Done():
					return
				}
			}
		}(i)
	}

	// Начинаем подавать задачи с адаптивной скоростью
	feedDoneCh := make(chan struct{})
	go func() {
		defer close(feedDoneCh)

		// Адаптивное замедление для очень больших нагрузок
		sleepDuration := time.Duration(1) // 1 наносекунда по умолчанию
		if parallelism > 10000 {
			sleepDuration = 0 // Для больших нагрузок не добавляем задержки
		}

		for {
			select {
			case <-ctx.Done():
				return
			default:
				select {
				case workCh <- struct{}{}:
					if sleepDuration > 0 {
						// time.Sleep(sleepDuration)
					}
				case <-ctx.Done():
					return
				default:
					// Канал заполнен, не ждем
					runtime.Gosched() // Даем другим горутинам возможность выполниться
				}
			}
		}
	}()

	log.Printf("Ожидаем завершения теста...")
	// Запускаем таймер для завершения теста
	testEndTimer := time.NewTimer(t.TestDuration)
	<-testEndTimer.C
	log.Printf("Время теста истекло, отправляем сигнал завершения")

	// Отменяем контекст для остановки всех запросов
	cancel()

	log.Printf("Сигнал завершения получен, ожидаем завершения воркеров...")

	// Устанавливаем таймаут для ожидания воркеров
	wgDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(wgDone)
	}()

	// Увеличиваем таймаут для больших параллельностей
	workerTimeout := 5 * time.Second
	if parallelism > 10000 {
		workerTimeout = 15 * time.Second
	} else if parallelism > 50000 {
		workerTimeout = 30 * time.Second
	}

	select {
	case <-wgDone:
		log.Printf("Все воркеры завершили работу")
	case <-time.After(workerTimeout):
		log.Printf("Таймаут ожидания воркеров (%s), продолжаем работу", workerTimeout)
	}

	// Вычисляем метрики
	latency.Calculate()
	testDuration := latency.FinishTime.Sub(latency.StartTime)
	done := success + failures + mismatches
	if done == 0 {
		done = 1
	}
	rps := float64(done) / testDuration.Seconds()

	// Если по какой-то причине время первой пачки не было установлено и тест завершен
	if latency.FirstBatchTime == 0 {
		// пачка не закрылась – пишем фактическую длительность теста
		latency.FirstBatchTime = testDuration
	}

	// Останавливаем приложение и получаем статистику ресурсов
	log.Printf("Останавливаем приложение после теста...")
	t.StopApplication()

	fmt.Printf("\n=== Результаты теста ===\n")
	fmt.Printf("Параллельность: %d, Глубина: %d\n", parallelism, depth)
	fmt.Printf("Время теста: %.2f секунд\n", testDuration.Seconds())
	fmt.Printf("Всего запросов: %d\n", requests)
	fmt.Printf("Успешных запросов: %d (%.2f%%)\n", success, float64(success)/float64(requests)*100)
	fmt.Printf("Неудачных запросов: %d (%.2f%%)\n", failures, float64(failures)/float64(requests)*100)
	fmt.Printf("Несовпадений: %d (%.2f%%)\n", mismatches, float64(mismatches)/float64(requests)*100)
	fmt.Printf("Время первого ответа: %s\n", latency.FirstResp)
	fmt.Printf("Время выполнения первой пачки (%d запросов): %s\n", parallelism, latency.FirstBatchTime)
	fmt.Printf("RPS: %.2f\n", rps)
	fmt.Printf("Среднее время отклика: %s\n", latency.Mean)
	fmt.Printf("Медиана: %s\n", latency.Median)
	fmt.Printf("P90/P95/P99: %s/%s/%s\n", latency.P90, latency.P95, latency.P99)
	fmt.Printf("Среднее CPU: %.2f%%, Максимальное CPU: %.2f%%\n", t.ResourceStats.AvgCPU, t.ResourceStats.MaxCPU)
	fmt.Printf("Средняя память: %.2f МБ, Максимальная память: %.2f МБ\n", t.ResourceStats.AvgMemory, t.ResourceStats.MaxMemory)
	fmt.Printf("Время запуска приложения: %.2f с\n", t.ResourceStats.StartupTime)
	fmt.Println()

	// Копируем статистику по секундам
	t.SecondStatMutex.Lock()
	secondBySecond := make([]SecondStat, len(t.SecondBySecond))
	copy(secondBySecond, t.SecondBySecond)
	t.SecondBySecond = t.SecondBySecond[:0] // Очищаем для следующего теста
	t.SecondStatMutex.Unlock()

	return &TestResult{
		Parallelism:    parallelism,
		Depth:          depth,
		Latency:        latency,
		Resources:      t.ResourceStats,
		AppName:        t.AppName,
		AppPID:         t.CurrentAppPID,
		Requests:       requests,
		Success:        success,
		Failures:       failures,
		Mismatches:     mismatches,
		RPS:            rps,
		TestDuration:   testDuration,
		SecondBySecond: secondBySecond,
	}
}

// updateSecondStat обновляет статистику за секунду
func (t *Tester) updateSecondStat(second, totalRequests int) {
	t.SecondStatMutex.Lock()
	defer t.SecondStatMutex.Unlock()

	// Если это первая секунда или новая секунда
	if len(t.SecondBySecond) == 0 || t.SecondBySecond[len(t.SecondBySecond)-1].Second != second {
		prevRequests := 0
		if len(t.SecondBySecond) > 0 {
			prevRequests = t.SecondBySecond[len(t.SecondBySecond)-1].Requests
		}

		currentRequests := totalRequests - prevRequests
		rps := float64(currentRequests)

		t.SecondBySecond = append(t.SecondBySecond, SecondStat{
			Second:   second,
			Requests: currentRequests,
			RPS:      rps,
		})
	}
}

// PerformWarmup выполняет один запрос для прогрева приложения
func (t *Tester) PerformWarmup() {
	log.Printf("Выполняем прогрев приложения %s одним запросом...", t.AppName)

	// Создаем простой URL для проверки с минимальной глубиной
	url := t.BaseURL + "/check?left=1&rnd=" + strconv.Itoa(rand.Intn(1000))

	// Выполняем запрос
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(url)
	req.Header.Set("Connection", "keep-alive")

	err := t.Client.DoTimeout(req, resp, 10*time.Second)

	if err != nil {
		log.Printf("Предупреждение: ошибка при прогреве: %v", err)
	} else {
		log.Printf("Прогрев выполнен успешно, статус: %d", resp.StatusCode())
	}
}

// RunMatrixTests запускает полное матричное тестирование
func (t *Tester) RunMatrixTests(showTableAfterEachTest bool) {
	color.Blue("Начинаем тестирование матрицы параметров для приложения '%s'...", t.AppName)

	// Инициализируем файлы результатов (только при первом запуске)
	GlobalTestResults.Mutex.Lock()
	isFirst := len(GlobalTestResults.Results) == 0
	GlobalTestResults.Mutex.Unlock()

	if isFirst {
		if err := t.InitResultFiles(); err != nil {
			log.Fatalf("Ошибка инициализации файлов результатов: %v", err)
		}
	}

	// Создаем локальную переменную для хранения результатов текущего приложения
	var appResults []*TestResult

	// Перебираем матрицу параметров
main_loop:
	for parallelism := 32; parallelism <= t.MaxParallelism && parallelism <= 128000; parallelism *= 2 {
		// Тестируем сначала с depth=1, затем сразу переходим к depth=16
		for depth := 1; depth <= t.MaxDepth; {
			// Запускаем тест с текущими параметрами
			result := t.RunMatrixTest(parallelism, depth)

			if result == nil {
				log.Printf("Ошибка выполнения теста для параллельности=%d, глубины=%d", parallelism, depth)
				// После depth=1 переходим к 16, иначе умножаем на 2
				if depth == 1 {
					depth = 16
				} else {
					depth *= 2
				}
				continue
			}

			// Добавляем результат в глобальный и локальный списки
			GlobalTestResults.Mutex.Lock()
			GlobalTestResults.Results = append(GlobalTestResults.Results, result)
			GlobalTestResults.Mutex.Unlock()

			appResults = append(appResults, result)

			// Сохраняем результат
			if err := t.SaveResult(result); err != nil {
				log.Printf("Ошибка сохранения результатов: %v", err)
			}

			// Показываем обновленную таблицу результатов после каждого теста, если требуется
			if showTableAfterEachTest {
				PrintResultsTable(appResults)
			}

			// Рассчитываем процент ошибок
			totalRequests := result.Success + result.Failures + result.Mismatches
			if totalRequests == 0 {
				totalRequests = 1 // Избегаем деления на ноль
			}
			errorRate := float64(result.Failures+result.Mismatches) / float64(totalRequests) * 100

			// Если первый запрос занимает больше TestDuration или процент ошибок > 50%,
			// останавливаем увеличение глубины
			if result.Latency.FirstBatchTime > t.TestDuration || errorRate > 50 {
				color.Red("Достигнут предел производительности для глубины=%d при параллельности=%d", depth, parallelism)
				color.Yellow("Время обработки первой пачки: %s, процент ошибок: %.2f%%", result.Latency.FirstBatchTime, errorRate)

				if depth == 1 {
					color.Red("Достигнут предел параллельности=%d", parallelism)
					break main_loop
				}
				break
			}

			// После depth=1 переходим к 16, иначе умножаем на 2
			if depth == 1 {
				depth = 16
			} else {
				depth *= 2
			}
		}
	}

	// Выводим итоговую таблицу для текущего приложения
	fmt.Println("\n=== ИТОГОВЫЕ РЕЗУЛЬТАТЫ ТЕСТИРОВАНИЯ ДЛЯ", t.AppName, "===")
	PrintResultsTable(appResults)
	fmt.Printf("\nПолные результаты доступны в файлах:\n")
	fmt.Printf("- %s (CSV для анализа)\n", t.ResultsFile)
	fmt.Printf("- %s (Подробная статистика)\n", t.FullStatsFile)
}

// PrintResultsTable выводит таблицу с результатами тестов
func PrintResultsTable(results []*TestResult) {
	// Проверяем, активен ли терминальный UI
	if termUI != nil && termUI.active {
		// Если терминальный UI активен, не выводим таблицу в консоль
		return
	}

	// Если терминальный интерфейс не активен, выводим таблицу
	if len(results) == 0 {
		color.Yellow("Нет результатов для отображения.")
		return
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{
		"Приложение",
		"Параллельность",
		"Глубина",
		"Запросы",
		"Ошибки",
		"RPS",
		"Время 1-го",
		"Время пачки",
		"Среднее",
		"P90",
		"CPU ядра",
		"Память",
		"Запуск",
	})
	table.SetBorder(false)
	table.SetColumnColor(
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgHiCyanColor},
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgHiBlueColor},
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgHiGreenColor},
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgWhiteColor},
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgMagentaColor},
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgYellowColor},
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgCyanColor},
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgHiCyanColor},
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgMagentaColor},
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgRedColor},
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgHiYellowColor},
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgHiMagentaColor},
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgHiCyanColor},
	)

	for _, result := range results {
		// Вычисляем процент ошибок
		totalErrors := result.Failures + result.Mismatches
		errorRateStr := fmt.Sprintf("%.1f%%", float64(totalErrors)/float64(result.Requests)*100)

		// Форматируем время
		firstRespTime := formatTimeReadable(result.Latency.FirstResp)
		firstBatchTime := formatTimeReadable(result.Latency.FirstBatchTime)
		meanTime := formatTimeReadable(result.Latency.Mean)
		p90Time := formatTimeReadable(result.Latency.P90)

		// Форматируем CPU
		cpuInfo := fmt.Sprintf("%.0f%% / %.0f%%", result.Resources.AvgCPU, result.Resources.MaxCPU)

		// Форматируем память
		memoryInfo := fmt.Sprintf("%.1f МБ", result.Resources.MaxMemory)

		// Форматируем RPS
		rpsStr := fmt.Sprintf("%.2f", result.RPS)

		table.Append([]string{
			result.AppName,
			strconv.Itoa(result.Parallelism),
			strconv.Itoa(result.Depth),
			humanize.Comma(result.Requests),
			errorRateStr,
			rpsStr,
			firstRespTime,
			firstBatchTime,
			meanTime,
			p90Time,
			cpuInfo,
			memoryInfo,
			fmt.Sprintf("%.2f с", result.Resources.StartupTime),
		})
	}

	table.Render()
}

// formatTimeReadable форматирует время в более удобном для чтения формате
func formatTimeReadable(d time.Duration) string {
	if d < time.Microsecond {
		return fmt.Sprintf("%d нс", d.Nanoseconds())
	} else if d < time.Millisecond {
		return fmt.Sprintf("%.2f мкс", float64(d.Nanoseconds())/1000)
	} else if d < time.Second {
		return fmt.Sprintf("%.2f мс", float64(d.Nanoseconds())/1000000)
	} else {
		return fmt.Sprintf("%.2f с", d.Seconds())
	}
}

// InitResultFiles инициализирует файлы результатов
func (t *Tester) InitResultFiles() error {
	// Убедимся, что директории существуют
	if err := os.MkdirAll(filepath.Dir(t.ResultsFile), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(t.FullStatsFile), 0755); err != nil {
		return err
	}

	// Создаем файл результатов CSV
	file, err := os.Create(t.ResultsFile)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	header := []string{
		"Приложение", "Параллельность", "Глубина", "Запросы_всего", "Запросы_успешные",
		"Запросы_неудачные", "Запросы_несовпадения", "Первый_запрос_мс", "Первая_пачка_мс",
		"RPS", "Среднее_время_мс", "Мин_время_мс", "Макс_время_мс",
		"Медиана_мс", "P90_мс", "P95_мс", "P99_мс", "Тест_длительность_с",
		"Среднее_CPU", "Макс_CPU", "Средняя_память_МБ", "Макс_память_МБ",
		"Время_запуска_с",
	}
	if err := writer.Write(header); err != nil {
		return err
	}
	writer.Flush()

	// Создаем файл полной статистики
	fullStats, err := os.Create(t.FullStatsFile)
	if err != nil {
		return err
	}
	defer fullStats.Close()

	fmt.Fprintf(fullStats, "=== РЕЗУЛЬТАТЫ ТЕСТИРОВАНИЯ МАТРИЦЫ ПАРАМЕТРОВ ===\n")
	fmt.Fprintf(fullStats, "Дата: %s\n\n", time.Now().Format(time.RFC1123))

	return nil
}

// SaveResult сохраняет результат теста в файлы
func (t *Tester) SaveResult(result *TestResult) error {
	// Блокируем доступ к файлам результатов
	fileMutex.Lock()
	defer fileMutex.Unlock()

	// Добавляем запись в CSV
	file, err := os.OpenFile(t.ResultsFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	record := []string{
		result.AppName,
		strconv.Itoa(result.Parallelism),
		strconv.Itoa(result.Depth),
		strconv.FormatInt(result.Requests, 10),
		strconv.FormatInt(result.Success, 10),
		strconv.FormatInt(result.Failures, 10),
		strconv.FormatInt(result.Mismatches, 10),
		strconv.FormatInt(result.Latency.FirstResp.Milliseconds(), 10),
		strconv.FormatInt(result.Latency.FirstBatchTime.Milliseconds(), 10), // Добавляем время первой пачки
		strconv.FormatFloat(result.RPS, 'f', 2, 64),
		strconv.FormatInt(result.Latency.Mean.Milliseconds(), 10),
		strconv.FormatInt(result.Latency.Min.Milliseconds(), 10),
		strconv.FormatInt(result.Latency.Max.Milliseconds(), 10),
		strconv.FormatInt(result.Latency.Median.Milliseconds(), 10),
		strconv.FormatInt(result.Latency.P90.Milliseconds(), 10),
		strconv.FormatInt(result.Latency.P95.Milliseconds(), 10),
		strconv.FormatInt(result.Latency.P99.Milliseconds(), 10),
		strconv.FormatFloat(result.TestDuration.Seconds(), 'f', 2, 64),
		strconv.FormatFloat(result.Resources.AvgCPU, 'f', 2, 64),
		strconv.FormatFloat(result.Resources.MaxCPU, 'f', 2, 64),
		strconv.FormatFloat(result.Resources.AvgMemory, 'f', 2, 64),
		strconv.FormatFloat(result.Resources.MaxMemory, 'f', 2, 64),
		strconv.FormatFloat(result.Resources.StartupTime, 'f', 2, 64),
	}
	if err := writer.Write(record); err != nil {
		return err
	}
	writer.Flush()

	// Добавляем подробную статистику
	fullStats, err := os.OpenFile(t.FullStatsFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer fullStats.Close()

	fmt.Fprintf(fullStats, "=== Тест: Приложение=%s, Параллельность=%d, Глубина=%d ===\n",
		result.AppName, result.Parallelism, result.Depth)
	fmt.Fprintf(fullStats, "Время теста: %.2f секунд\n", result.TestDuration.Seconds())
	fmt.Fprintf(fullStats, "Всего запросов: %d\n", result.Requests)
	fmt.Fprintf(fullStats, "Успешных запросов: %d (%.2f%%)\n", result.Success, float64(result.Success)/float64(result.Requests)*100)
	fmt.Fprintf(fullStats, "Неудачных запросов: %d (%.2f%%)\n", result.Failures, float64(result.Failures)/float64(result.Requests)*100)
	fmt.Fprintf(fullStats, "Несовпадений: %d (%.2f%%)\n", result.Mismatches, float64(result.Mismatches)/float64(result.Requests)*100)
	fmt.Fprintf(fullStats, "Время первого ответа: %s\n", result.Latency.FirstResp)
	fmt.Fprintf(fullStats, "Время выполнения первой пачки (%d запросов): %s\n", result.Parallelism, result.Latency.FirstBatchTime)
	fmt.Fprintf(fullStats, "RPS: %.2f\n", result.RPS)
	fmt.Fprintf(fullStats, "Среднее время отклика: %s\n", result.Latency.Mean)
	fmt.Fprintf(fullStats, "Минимальное время: %s\n", result.Latency.Min)
	fmt.Fprintf(fullStats, "Максимальное время: %s\n", result.Latency.Max)
	fmt.Fprintf(fullStats, "Медиана: %s\n", result.Latency.Median)
	fmt.Fprintf(fullStats, "P90: %s\n", result.Latency.P90)
	fmt.Fprintf(fullStats, "P95: %s\n", result.Latency.P95)
	fmt.Fprintf(fullStats, "P99: %s\n", result.Latency.P99)
	fmt.Fprintf(fullStats, "Среднее CPU: %.2f%%\n", result.Resources.AvgCPU)
	fmt.Fprintf(fullStats, "Максимальное CPU: %.2f%%\n", result.Resources.MaxCPU)
	fmt.Fprintf(fullStats, "Средняя память: %.2f МБ\n", result.Resources.AvgMemory)
	fmt.Fprintf(fullStats, "Максимальная память: %.2f МБ\n", result.Resources.MaxMemory)
	fmt.Fprintf(fullStats, "Время запуска приложения: %.2f с\n", result.Resources.StartupTime)
	fmt.Fprintf(fullStats, "\nСекундные показатели:\n")

	for _, stat := range result.SecondBySecond {
		fmt.Fprintf(fullStats, "Секунда %d: %d запросов, %.2f rps\n", stat.Second, stat.Requests, stat.RPS)
	}

	fmt.Fprintf(fullStats, "-----------------------------------\n\n")
	return nil
}

// StartApplication запускает приложение через app-manager
func (t *Tester) StartApplication() error {
	color.Cyan("=== Запуск приложения '%s' через app-manager ===", t.AppName)
	startTime := time.Now()

	// Выводим полную команду, которую будем запускать
	cmdStr := t.AppManagerPath + " start " + t.AppName
	log.Printf("Запускаем команду: %s", cmdStr)

	// Создаем команду
	cmd := exec.Command(t.AppManagerPath, "start", t.AppName)

	// Создаем пайпы для stdout и stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("ошибка создания stdout pipe: %v", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("ошибка создания stderr pipe: %v", err)
	}

	// Запускаем команду без ожидания ее завершения
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ошибка запуска app-manager: %v", err)
	}

	log.Printf("Процесс app-manager запущен, считываем вывод")

	// Создаем каналы для чтения вывода и ошибок с таймаутом
	outCh := make(chan string, 1)
	errCh := make(chan string, 1)
	doneCh := make(chan struct{})

	// Читаем stdout асинхронно
	go func() {
		outBytes, err := io.ReadAll(stdout)
		if err != nil {
			log.Printf("Ошибка чтения из stdout: %v", err)
		}
		outCh <- string(outBytes)
	}()

	// Читаем stderr асинхронно
	go func() {
		errBytes, err := io.ReadAll(stderr)
		if err != nil {
			log.Printf("Ошибка чтения из stderr: %v", err)
		}
		errCh <- string(errBytes)
	}()

	// Ожидаем завершения команды в отдельной горутине
	go func() {
		err := cmd.Wait()
		if err != nil {
			log.Printf("Процесс app-manager завершился с ошибкой: %v", err)
		} else {
			log.Printf("Процесс app-manager завершился успешно")
		}
		close(doneCh)
	}()

	// Ожидаем вывод и завершение с таймаутом
	var stdoutStr, stderrStr string
	timeout := time.After(30 * time.Second)

	select {
	case <-doneCh:
		// Процесс завершился, получаем вывод
		stdoutStr = <-outCh
		stderrStr = <-errCh
		log.Printf("Процесс завершен, получен вывод")
	case <-timeout:
		// Превышен таймаут, прерываем процесс
		if cmd.Process != nil {
			log.Printf("Таймаут ожидания app-manager, убиваем процесс")
			cmd.Process.Kill()
		}
		return fmt.Errorf("таймаут запуска приложения через app-manager (превышено 30 секунд)")
	}

	// Объединяем stdout и stderr
	output := stdoutStr
	if stderrStr != "" {
		output += "\n" + stderrStr
	}

	log.Printf("Получен вывод от app-manager через %.2f сек", time.Since(startTime).Seconds())

	// Удаляем ANSI коды из вывода
	re := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	cleanOutput := re.ReplaceAllString(output, "")

	// Выводим чистый вывод в лог для отладки
	log.Printf("Вывод app-manager:\n%s", cleanOutput)

	// Разбиваем вывод на строки
	lines := strings.Split(cleanOutput, "\n")

	// Ищем строку, содержащую JSON
	var jsonLine string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "{") && strings.HasSuffix(line, "}") {
			jsonLine = line
			log.Printf("Найдена JSON строка: %s", jsonLine)
			break
		}
	}

	// Если нашли строку с JSON
	if jsonLine != "" {
		var response AppManagerResponse
		if err := json.Unmarshal([]byte(jsonLine), &response); err != nil {
			log.Printf("Предупреждение: не удалось распарсить JSON из вывода app-manager: %v, строка: %s", err, jsonLine)
		} else {
			// Успешно распарсили JSON
			t.CurrentAppPID = response.PID
			t.ResourceStats = NewResourceStats()
			t.ResourceStats.StartupTime = response.StartupTime

			color.Green("Приложение запущено, PID: %d, время запуска: %.2f секунд", response.PID, response.StartupTime)

			// Запускаем сбор метрик
			log.Printf("Запускаем сбор метрик ресурсов для PID %d", response.PID)
			t.StartResourceCollection()

			return nil
		}
	}

	// Если мы здесь, то не смогли распарсить JSON, ищем PID и время запуска в тексте
	pidRegex := regexp.MustCompile(`PID (\d+)`)
	timeRegex := regexp.MustCompile(`за ([0-9.]+) секунд`)

	var pid int
	var startupTime float64

	// Ищем PID и время запуска в выводе
	for _, line := range lines {
		if match := pidRegex.FindStringSubmatch(line); len(match) > 1 {
			pid, _ = strconv.Atoi(match[1])
			log.Printf("Найден PID в тексте: %d", pid)
		}
		if match := timeRegex.FindStringSubmatch(line); len(match) > 1 {
			startupTime, _ = strconv.ParseFloat(match[1], 64)
			log.Printf("Найдено время запуска в тексте: %.2f сек", startupTime)
		}
	}

	if pid > 0 {
		t.CurrentAppPID = pid
		t.ResourceStats = NewResourceStats()
		t.ResourceStats.StartupTime = startupTime

		color.Green("Приложение запущено, PID: %d, время запуска: %.2f секунд", pid, startupTime)

		// Запускаем сбор метрик
		log.Printf("Запускаем сбор метрик ресурсов для PID %d", pid)
		t.StartResourceCollection()
		return nil
	}

	return fmt.Errorf("не удалось найти информацию о запущенном приложении в выводе app-manager")
}

// StopApplication останавливает приложение через app-manager
func (t *Tester) StopApplication() error {
	if t.CurrentAppPID == 0 {
		return nil // Нет запущенного приложения
	}

	color.Yellow("=== Остановка приложения с PID %d ===", t.CurrentAppPID)

	// Останавливаем сбор метрик если он был запущен
	select {
	case <-t.StopResourceCollection:
		log.Printf("Канал StopResourceCollection был уже закрыт")
	default:
		close(t.StopResourceCollection)
		// Даем немного времени для завершения горутин сбора метрик
		time.Sleep(100 * time.Millisecond)
	}

	// Создаем новый канал для следующего запуска
	t.StopResourceCollection = make(chan struct{})

	// Вычисляем статистику по ресурсам
	t.ResourceStats.Calculate()

	// Проверяем существует ли еще процесс
	checkCmd := exec.Command("ps", "-p", strconv.Itoa(t.CurrentAppPID))
	if err := checkCmd.Run(); err != nil {
		log.Printf("Процесс %d уже не существует, пропускаем остановку", t.CurrentAppPID)
		t.CurrentAppPID = 0
		return nil
	}

	// Устанавливаем таймаут для остановки приложения
	stoppedCh := make(chan struct{})
	var err error

	go func() {
		// Выполняем команду остановки через app-manager
		cmd := exec.Command(t.AppManagerPath, "stop", strconv.Itoa(t.CurrentAppPID))
		log.Printf("Выполняем команду остановки: %s", cmd.String())
		output, cmdErr := cmd.CombinedOutput()
		if cmdErr != nil {
			log.Printf("Предупреждение при остановке приложения: %v, вывод: %s", cmdErr, string(output))
			err = cmdErr
		}
		close(stoppedCh)
	}()

	// Ждем завершения с таймаутом
	select {
	case <-stoppedCh:
		log.Printf("Приложение успешно остановлено через app-manager")
	case <-time.After(5 * time.Second):
		log.Printf("Таймаут остановки через app-manager, пробуем остановить напрямую")
		// Пробуем SIGTERM
		killCmd := exec.Command("kill", "-15", strconv.Itoa(t.CurrentAppPID))
		if killErr := killCmd.Run(); killErr != nil {
			log.Printf("Ошибка при отправке SIGTERM: %v", killErr)
			// Пробуем SIGKILL
			kill9Cmd := exec.Command("kill", "-9", strconv.Itoa(t.CurrentAppPID))
			if kill9Err := kill9Cmd.Run(); kill9Err != nil {
				log.Printf("Ошибка при отправке SIGKILL: %v", kill9Err)
			} else {
				log.Printf("Процесс успешно остановлен с помощью SIGKILL")
			}
		} else {
			log.Printf("Процесс успешно остановлен с помощью SIGTERM")
		}
	}

	// Проверяем, что процесс действительно остановлен
	time.Sleep(100 * time.Millisecond)
	finalCheck := exec.Command("ps", "-p", strconv.Itoa(t.CurrentAppPID))
	if finalCheck.Run() == nil {
		log.Printf("ВНИМАНИЕ: процесс %d всё еще существует после попыток остановки!", t.CurrentAppPID)
		// Последняя попытка с SIGKILL
		exec.Command("kill", "-9", strconv.Itoa(t.CurrentAppPID)).Run()
	}

	// Очищаем PID
	t.CurrentAppPID = 0
	return err
}

// StartResourceCollection запускает периодический сбор метрик использования ресурсов
func (t *Tester) StartResourceCollection() {
	if t.CurrentAppPID == 0 {
		log.Printf("Невозможно запустить сбор метрик: PID приложения не установлен")
		return
	}

	log.Printf("Запускаем сбор метрик для PID %d", t.CurrentAppPID)

	// Сначала останавливаем предыдущий сбор если он был запущен
	select {
	case <-t.StopResourceCollection:
		log.Printf("Сначала остановим предыдущий сбор метрик")
	default:
		// Канал не закрыт, все в порядке
	}

	// Всегда создаем новый канал для нового сбора
	t.StopResourceCollection = make(chan struct{})

	go func() {
		log.Printf("Горутина сбора метрик запущена")
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		// Пробуем сначала проверить доступность процесса и логируем полную команду
		testCmd := exec.Command("ps", "-p", strconv.Itoa(t.CurrentAppPID), "-o", "pid", "--no-headers")
		log.Printf("Проверка процесса командой: %s", testCmd.String())

		// Устанавливаем таймаут для проверки
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		testCmd = exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(t.CurrentAppPID), "-o", "pid", "--no-headers")
		testOutput, err := testCmd.CombinedOutput()

		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("Таймаут проверки процесса %d через ps", t.CurrentAppPID)
			// Переходим к альтернативному методу
			t.startResourceCollectionViaTop()
			return
		}

		if err != nil {
			errorMsg := fmt.Sprintf("Процесс %d недоступен для мониторинга через ps: %v\nВывод: %s",
				t.CurrentAppPID, err, string(testOutput))
			log.Printf(errorMsg)

			// Попробуем альтернативную команду top для проверки
			topCtx, topCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer topCancel()

			topTestCmd := exec.CommandContext(topCtx, "top", "-b", "-n", "1", "-p", strconv.Itoa(t.CurrentAppPID))
			topOutput, topErr := topTestCmd.CombinedOutput()

			if topCtx.Err() == context.DeadlineExceeded {
				log.Printf("Таймаут проверки процесса %d через top", t.CurrentAppPID)
				return
			}

			if topErr != nil {
				log.Printf("Процесс %d также недоступен через top: %v\nВывод: %s",
					t.CurrentAppPID, topErr, string(topOutput))
				// Просто записываем ошибку и продолжаем работу
				return
			}

			log.Printf("Процесс %d доступен через top, будем использовать альтернативный метод сбора метрик", t.CurrentAppPID)
			// Используем метод сбора через top
			t.startResourceCollectionViaTop()
			return
		}

		log.Printf("Процесс %d доступен, начинаем сбор метрик через ps", t.CurrentAppPID)

		for {
			select {
			case <-ticker.C:
				// Получаем использование CPU и памяти для процесса с таймаутом
				cmdCtx, cmdCancel := context.WithTimeout(context.Background(), 1*time.Second)
				cmdStr := fmt.Sprintf("ps -p %d -o %%cpu,%%mem,rss --no-headers", t.CurrentAppPID)
				cmd := exec.CommandContext(cmdCtx, "ps", "-p", strconv.Itoa(t.CurrentAppPID), "-o", "%cpu,%mem,rss", "--no-headers")

				// Используем CombinedOutput вместо Output для получения как stdout, так и stderr
				output, err := cmd.CombinedOutput()
				cmdCancel()

				if cmdCtx.Err() == context.DeadlineExceeded {
					log.Printf("Таймаут получения метрик через ps, пропускаем запись")
					continue
				}

				if err != nil {
					errorMsg := fmt.Sprintf("Ошибка получения метрик через ps: %v\nКоманда: %s\nВывод: %s",
						err, cmdStr, string(output))
					log.Printf(errorMsg)
					continue
				}

				// Парсим вывод
				fields := strings.Fields(string(output))
				if len(fields) < 3 {
					log.Printf("Некорректный формат вывода ps: получено %d полей, ожидалось минимум 3: %s",
						len(fields), string(output))
					continue
				}

				// Парсим CPU использование
				cpu, err := strconv.ParseFloat(fields[0], 64)
				if err != nil {
					log.Printf("Ошибка парсинга CPU: %v (строка: %s)", err, fields[0])
					continue
				}

				// Парсим использование памяти
				_, err = strconv.ParseFloat(fields[1], 64) // Процент памяти нам не нужен
				if err != nil {
					log.Printf("Ошибка парсинга %%MEM: %v (строка: %s)", err, fields[1])
					continue
				}

				// Парсим RSS (в KB)
				rss, err := strconv.ParseFloat(fields[2], 64)
				if err != nil {
					log.Printf("Ошибка парсинга RSS: %v (строка: %s)", err, fields[2])
					continue
				}

				// Конвертируем RSS в MB
				memMB := rss / 1024.0

				// Добавляем метрику
				t.ResourceStats.AddMetric(cpu, memMB)

			case <-t.StopResourceCollection:
				log.Printf("Получен сигнал остановки сбора метрик для PID %d", t.CurrentAppPID)
				return
			}
		}
	}()
}

// startResourceCollectionViaTop запускает альтернативный сбор метрик через команду top
func (t *Tester) startResourceCollectionViaTop() {
	if t.CurrentAppPID == 0 {
		return
	}

	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// Используем top в batch режиме для получения метрик
				cmd := exec.Command("top", "-b", "-n", "1", "-p", strconv.Itoa(t.CurrentAppPID))
				output, err := cmd.CombinedOutput()
				if err != nil {
					log.Printf("Ошибка получения метрик через top: %v\nВывод: %s",
						err, string(output))
					// Останавливаем все тестирование
					t.StopApplication()
					os.Exit(1)
					return
				}

				// Парсим вывод top
				lines := strings.Split(string(output), "\n")

				// Ищем строку с информацией о нашем процессе
				var processLine string
				for _, line := range lines {
					if strings.Contains(line, strconv.Itoa(t.CurrentAppPID)) {
						processLine = line
						break
					}
				}

				if processLine == "" {
					log.Printf("Не удалось найти информацию о процессе %d в выводе top", t.CurrentAppPID)
					// Останавливаем все тестирование
					t.StopApplication()
					os.Exit(1)
					return
				}

				// Парсим строку с метриками
				// Типичный формат: PID USER PR NI VIRT RES SHR S %CPU %MEM TIME+ COMMAND
				fields := strings.Fields(processLine)
				if len(fields) < 10 {
					log.Printf("Недостаточно полей в выводе top: %s", processLine)
					// Останавливаем все тестирование
					t.StopApplication()
					os.Exit(1)
					return
				}

				// Ищем индексы колонок %CPU и %MEM
				cpuIndex := -1
				memIndex := -1
				resIndex := -1

				// Сначала ищем заголовки
				for _, line := range lines {
					if strings.Contains(line, "%CPU") && strings.Contains(line, "%MEM") {
						headers := strings.Fields(line)
						for j, header := range headers {
							if header == "%CPU" {
								cpuIndex = j
							} else if header == "%MEM" {
								memIndex = j
							} else if header == "RES" {
								resIndex = j
							}
						}
						break
					}
				}

				if cpuIndex == -1 || memIndex == -1 || resIndex == -1 {
					log.Printf("Не удалось найти нужные колонки в выводе top. CPU: %d, MEM: %d, RES: %d",
						cpuIndex, memIndex, resIndex)
					// Останавливаем все тестирование
					t.StopApplication()
					os.Exit(1)
					return
				}

				// Теперь парсим метрики
				if cpuIndex < len(fields) && memIndex < len(fields) && resIndex < len(fields) {
					cpu, cpuErr := strconv.ParseFloat(fields[cpuIndex], 64)
					if cpuErr != nil {
						log.Printf("Ошибка парсинга CPU из top: %v, значение: %s", cpuErr, fields[cpuIndex])
						cpu = 0
					}

					// Исходное значение mem не используется, поэтому просто парсим и проверяем ошибку
					_, memErr := strconv.ParseFloat(fields[memIndex], 64)
					if memErr != nil {
						log.Printf("Ошибка парсинга MEM из top: %v, значение: %s", memErr, fields[memIndex])
					}

					// Разбираемся с единицами измерения памяти
					res := fields[resIndex]
					var memMB float64

					// Может содержать суффикс 'g', 'm', 'k'
					if strings.HasSuffix(res, "g") {
						val, err := strconv.ParseFloat(res[:len(res)-1], 64)
						if err == nil {
							memMB = val * 1024 // GB to MB
						}
					} else if strings.HasSuffix(res, "m") {
						val, err := strconv.ParseFloat(res[:len(res)-1], 64)
						if err == nil {
							memMB = val
						}
					} else if strings.HasSuffix(res, "k") {
						val, err := strconv.ParseFloat(res[:len(res)-1], 64)
						if err == nil {
							memMB = val / 1024 // KB to MB
						}
					} else {
						val, err := strconv.ParseFloat(res, 64)
						if err == nil {
							memMB = val / 1024 // Предполагаем, что это KB
						}
					}

					// Добавляем метрику
					t.ResourceStats.AddMetric(cpu, memMB)
					log.Printf("Собраны метрики через top: CPU=%.2f%%, MEM=%.2f MB", cpu, memMB)
				} else {
					log.Printf("Некорректный формат данных от top, индексы выходят за границы")
					// Останавливаем все тестирование
					t.StopApplication()
					os.Exit(1)
					return
				}

			case <-t.StopResourceCollection:
				return
			}
		}
	}()
}

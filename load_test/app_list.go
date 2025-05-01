package main

import (
	"bytes"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
)

// GetAppList возвращает список всех доступных приложений
func GetAppList(appManagerPath string) ([]string, error) {
	var out bytes.Buffer
	cmd := exec.Command(appManagerPath, "list")
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	// Регулярное выражение для удаления ANSI-цветов
	re := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	cleanOutput := re.ReplaceAllString(out.String(), "")

	var apps []string
	// Парсим вывод команды list
	lines := strings.Split(cleanOutput, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Пропускаем пустые строки и заголовки
		if line == "" ||
			strings.HasPrefix(line, "Доступные") ||
			strings.HasPrefix(line, "Available") ||
			strings.HasPrefix(line, "===") {
			continue
		}

		// Для строк, содержащих информацию о приложении, берем только имя
		if strings.Contains(line, " - ") {
			parts := strings.SplitN(line, " - ", 2)
			if len(parts) >= 1 {
				appName := strings.TrimSpace(parts[0])
				if appName != "" {
					apps = append(apps, appName)
				}
			}
		}
	}

	log.Printf("Найдено %d приложений для тестирования: %v", len(apps), apps)
	return apps, nil
}

// GlobalTestResults хранит общие результаты всех тестов
var GlobalTestResults struct {
	Results []*TestResult
	Mutex   sync.Mutex
}

// RunAppMatrixTest запускает тестирование для одного приложения
func RunAppMatrixTest(
	app string,
	baseURL string,
	duration time.Duration,
	maxParallelism int,
	maxDepth int,
	commonResultsFile string, // Общий файл для всех приложений
	commonStatsFile string, // Общий файл детальной статистики
	appManagerPath string,
	showTableAfterEachTest bool, // Показывать ли таблицу после каждого теста
) {
	// Создаем тестер для приложения
	tester := NewTester(
		baseURL,
		duration,
		maxParallelism,
		maxDepth,
		commonResultsFile,
		commonStatsFile,
		appManagerPath,
		app,
	)

	// Защищаем выполнение тестов через recover, чтобы в случае паники
	// не прерывалось тестирование других приложений
	defer func() {
		if r := recover(); r != nil {
			color.Red("!!! КРИТИЧЕСКАЯ ОШИБКА при тестировании приложения %s: %v", app, r)
			log.Printf("Ожидание 5 секунд перед продолжением...")
			time.Sleep(5 * time.Second)
		}
	}()

	// Запускаем тест с обработкой ошибок запуска приложения
	color.Cyan("Начинаем тестирование приложения %s...", app)

	// Пробуем сначала запустить приложение, чтобы проверить, работает ли оно
	err := tester.StartApplication()
	if err != nil {
		color.Red("!!! ОШИБКА запуска приложения %s: %v", app, err)
		log.Printf("Ожидание 5 секунд перед продолжением...")
		time.Sleep(5 * time.Second)
		return
	}

	// Выполняем прогрев приложения одним запросом
	tester.PerformWarmup()

	// Останавливаем тестовый запуск приложения
	if err := tester.StopApplication(); err != nil {
		color.Yellow("Предупреждение: проблема при остановке приложения %s: %v", app, err)
	}

	// Запускаем полное тестирование
	tester.RunMatrixTests(showTableAfterEachTest)
}

// PrintGlobalResultsTable выводит таблицу с полными результатами всех тестов
func PrintGlobalResultsTable() {
	GlobalTestResults.Mutex.Lock()
	defer GlobalTestResults.Mutex.Unlock()

	if len(GlobalTestResults.Results) == 0 {
		color.Yellow("Пока нет результатов тестирования.")
		return
	}

	// Проверяем, активен ли терминальный UI
	activeTermUI := false
	if termUI != nil && termUI.active {
		activeTermUI = true
		// Если терминальный UI активен, он сам отобразит результаты
		// Но принудительно обновим его
		termUI.UpdateResults(GlobalTestResults.Results)
		return
	}

	// Выводим таблицу только если терминальный UI не активен
	if !activeTermUI {
		color.Blue("\n=== ИТОГОВЫЕ РЕЗУЛЬТАТЫ ТЕСТИРОВАНИЯ ДЛЯ ВСЕХ ПРИЛОЖЕНИЙ ===")
		PrintResultsTable(GlobalTestResults.Results)
	}
}

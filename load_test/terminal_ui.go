package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/buger/goterm"
	"github.com/dustin/go-humanize"
	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
)

// TerminalUI управляет интерфейсом терминала с закрепленной таблицей и логами
type TerminalUI struct {
	// Кольцевой буфер для хранения последних логов
	logBuffer      []string
	logBufferSize  int
	logBufferIndex int

	// Мьютекс для безопасного обновления состояния
	mutex sync.Mutex

	// Логгер для перехвата и сохранения логов
	logger *log.Logger

	// Трубы для перехвата логов
	logReader *io.PipeReader
	logWriter *io.PipeWriter

	// Исходные вывод и логгер
	originalOutput *os.File
	originalLogger *log.Logger

	// Флаг активности UI
	active bool

	// Канал для сигнала остановки
	stopChan chan struct{}

	// Высота раздела логов
	logHeight int

	// Последнее обновление таблицы
	lastUpdate time.Time
}

// NewTerminalUI создает новый интерфейс терминала
func NewTerminalUI(logHeight int) *TerminalUI {
	// Создаем трубу для перехвата логов
	r, w := io.Pipe()

	// Сохраняем оригинальный логгер и выход
	originalOutput := os.Stdout
	originalLogger := log.Default()

	// Создаем новый экземпляр UI
	ui := &TerminalUI{
		logBuffer:      make([]string, logHeight),
		logBufferSize:  logHeight,
		logBufferIndex: 0,
		logReader:      r,
		logWriter:      w,
		originalOutput: originalOutput,
		originalLogger: originalLogger,
		active:         false,
		stopChan:       make(chan struct{}),
		logHeight:      logHeight,
		lastUpdate:     time.Now(),
	}

	// Создаем новый логгер, который будет писать в нашу трубу
	ui.logger = log.New(io.MultiWriter(originalOutput, w), "", log.LstdFlags)

	return ui
}

// Start запускает UI
func (ui *TerminalUI) Start() {
	ui.mutex.Lock()
	defer ui.mutex.Unlock()

	if ui.active {
		return
	}

	// Устанавливаем наш логгер как текущий
	log.SetOutput(io.MultiWriter(ui.originalOutput, ui.logWriter))

	// Запускаем горутину для чтения логов из трубы
	go ui.readLogs()

	// Запускаем горутину для периодического обновления экрана
	go ui.updateScreen()

	ui.active = true
}

// Stop останавливает UI и возвращает стандартный вывод
func (ui *TerminalUI) Stop() {
	ui.mutex.Lock()
	defer ui.mutex.Unlock()

	if !ui.active {
		return
	}

	close(ui.stopChan)
	log.SetOutput(ui.originalOutput)
	goterm.Clear()
	goterm.Flush()
	ui.active = false
}

// UpdateResults обновляет таблицу результатов
func (ui *TerminalUI) UpdateResults(results []*TestResult) {
	ui.mutex.Lock()
	defer ui.mutex.Unlock()

	if !ui.active {
		PrintResultsTable(results)
		return
	}

	// Очищаем экран
	goterm.Clear()

	// Определяем размер терминала
	width := goterm.Width()
	height := goterm.Height() - 10 // Оставляем запас в 10 строк внизу экрана

	// Рисуем заголовок
	title := color.HiYellowString("=== РЕЗУЛЬТАТЫ ТЕСТИРОВАНИЯ ===")
	goterm.MoveCursor(1, 1)
	goterm.Printf("%s\n", title)

	// Вычисляем допустимую высоту для таблицы
	tableHeight := height - ui.logHeight - 3 // 3 строки для разделителей и заголовка
	if tableHeight < 3 {
		tableHeight = 3 // Минимум - заголовок и одна строка
	}

	// Создаем таблицу с результатами
	tableString := renderResultsTable(results, tableHeight)
	goterm.MoveCursor(1, 3)
	goterm.Printf("%s", tableString)

	// Рисуем разделитель
	separatorY := height - ui.logHeight - 1
	goterm.MoveCursor(1, separatorY)
	separator := color.CyanString(strings.Repeat("=", width))
	goterm.Printf("%s\n", separator)

	// Рисуем заголовок для логов
	goterm.MoveCursor(1, separatorY+1)
	goterm.Printf("%s\n", color.HiCyanString("ПОСЛЕДНИЕ ЛОГИ:"))

	// Выводим последние логи
	for i := 0; i < ui.logBufferSize; i++ {
		idx := (ui.logBufferIndex + i) % ui.logBufferSize
		if ui.logBuffer[idx] != "" {
			y := separatorY + 2 + i
			if y <= height {
				goterm.MoveCursor(1, y)
				logLine := ui.logBuffer[idx]
				if len(logLine) > width {
					logLine = logLine[:width-3] + "..."
				}
				goterm.Printf("%s", logLine)
			}
		}
	}

	// Обновляем экран
	goterm.Flush()

	// Запоминаем время последнего обновления
	ui.lastUpdate = time.Now()
}

// renderResultsTable рендерит таблицу результатов с ограничением по высоте
func renderResultsTable(results []*TestResult, maxHeight int) string {
	if len(results) == 0 {
		return color.YellowString("Нет результатов для отображения.")
	}

	// Создаем буфер для записи таблицы
	var buf strings.Builder

	// Создаем таблицу
	table := tablewriter.NewWriter(&buf)
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
	table.SetAutoFormatHeaders(true)
	table.SetHeaderColor(
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgHiWhiteColor},
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

	// Вычисляем, сколько последних строк показывать
	numRows := len(results)
	startIdx := 0

	// Учитываем, что таблица с заголовком занимает 3 строки + строка данных для каждого результата
	if numRows+3 > maxHeight {
		// Оставляем 2 строки на заголовок и разделители
		startIdx = numRows - (maxHeight - 3)
		if startIdx < 0 {
			startIdx = 0
		}
	}

	// Добавляем строки результатов, начиная с вычисленного индекса
	for i := startIdx; i < numRows; i++ {
		result := results[i]

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
			fmt.Sprintf("%d", result.Parallelism),
			fmt.Sprintf("%d", result.Depth),
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
	return buf.String()
}

// readLogs читает логи из трубы и добавляет их в кольцевой буфер
func (ui *TerminalUI) readLogs() {
	buf := make([]byte, 1024)
	var line strings.Builder

	for {
		select {
		case <-ui.stopChan:
			return
		default:
			n, err := ui.logReader.Read(buf)
			if err != nil {
				if err != io.EOF {
					fmt.Fprintf(os.Stderr, "Ошибка чтения логов: %v\n", err)
				}
				return
			}

			for i := 0; i < n; i++ {
				b := buf[i]
				if b == '\n' {
					ui.addLogLine(line.String())
					line.Reset()
				} else {
					line.WriteByte(b)
				}
			}
		}
	}
}

// addLogLine добавляет строку лога в кольцевой буфер
func (ui *TerminalUI) addLogLine(line string) {
	ui.mutex.Lock()
	defer ui.mutex.Unlock()

	ui.logBuffer[ui.logBufferIndex] = line
	ui.logBufferIndex = (ui.logBufferIndex + 1) % ui.logBufferSize
}

// updateScreen периодически обновляет экран
func (ui *TerminalUI) updateScreen() {
	ticker := time.NewTicker(16 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Проверяем, не обновляли ли мы экран недавно
			if time.Since(ui.lastUpdate) >= 16*time.Millisecond {
				GlobalTestResults.Mutex.Lock()
				results := make([]*TestResult, len(GlobalTestResults.Results))
				copy(results, GlobalTestResults.Results)
				GlobalTestResults.Mutex.Unlock()

				ui.UpdateResults(results)
			}
		case <-ui.stopChan:
			return
		}
	}
}

// RedirectLogger переопределяет стандартный логгер на использование нашего UI
func (ui *TerminalUI) RedirectLogger() {
	log.SetOutput(io.MultiWriter(ui.originalOutput, ui.logWriter))
}

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/fatih/color"
	"github.com/spf13/pflag"
)

// Глобальные переменные для доступа из разных частей программы
var (
	// termUI это глобальный экземпляр интерактивного терминального интерфейса
	termUI *TerminalUI
)

// Структура для хранения информации о лимитах системы
type SystemLimits struct {
	MaxOpenFiles     int
	MaxUserProcesses int
	TCPMaxSynBacklog int
	MaxMapCount      int
	SomaxConn        int
}

// Функция для получения системных лимитов
func checkSystemLimits() *SystemLimits {
	limits := &SystemLimits{}

	// Проверяем лимит открытых файлов
	ulimitCmd := exec.Command("bash", "-c", "ulimit -n")
	output, err := ulimitCmd.Output()
	if err == nil {
		if val, err := strconv.Atoi(strings.TrimSpace(string(output))); err == nil {
			limits.MaxOpenFiles = val
		}
	}

	// Проверяем лимит процессов
	ulimitProcCmd := exec.Command("bash", "-c", "ulimit -u")
	output, err = ulimitProcCmd.Output()
	if err == nil {
		if val, err := strconv.Atoi(strings.TrimSpace(string(output))); err == nil {
			limits.MaxUserProcesses = val
		}
	}

	// Проверяем параметры ядра для TCP
	sysParamCmd := exec.Command("bash", "-c", "sysctl net.ipv4.tcp_max_syn_backlog | awk '{print $3}'")
	output, err = sysParamCmd.Output()
	if err == nil {
		if val, err := strconv.Atoi(strings.TrimSpace(string(output))); err == nil {
			limits.TCPMaxSynBacklog = val
		}
	}

	// Проверяем максимальное количество отображений памяти
	mapCountCmd := exec.Command("bash", "-c", "sysctl vm.max_map_count | awk '{print $3}'")
	output, err = mapCountCmd.Output()
	if err == nil {
		if val, err := strconv.Atoi(strings.TrimSpace(string(output))); err == nil {
			limits.MaxMapCount = val
		}
	}

	// Проверяем максимальное количество соединений
	somaxConnCmd := exec.Command("bash", "-c", "sysctl net.core.somaxconn | awk '{print $3}'")
	output, err = somaxConnCmd.Output()
	if err == nil {
		if val, err := strconv.Atoi(strings.TrimSpace(string(output))); err == nil {
			limits.SomaxConn = val
		}
	}

	return limits
}

// Функция для проверки достаточности лимитов и вывода рекомендаций
func logSystemLimits(limits *SystemLimits) {
	color.HiYellow("=== ПРОВЕРКА СИСТЕМНЫХ ЛИМИТОВ ===")

	// Проверяем и выводим информацию о лимите открытых файлов
	fileStatus := color.GreenString("OK")
	if limits.MaxOpenFiles < 65536 {
		fileStatus = color.RedString("НИЗКИЙ")
	} else if limits.MaxOpenFiles < 100000 {
		fileStatus = color.YellowString("ДОСТАТОЧНЫЙ")
	}
	fmt.Printf("Лимит открытых файлов (ulimit -n): %d %s\n", limits.MaxOpenFiles, fileStatus)

	// Проверяем и выводим информацию о лимите процессов
	procStatus := color.GreenString("OK")
	if limits.MaxUserProcesses < 10000 {
		procStatus = color.RedString("НИЗКИЙ")
	} else if limits.MaxUserProcesses < 30000 {
		procStatus = color.YellowString("ДОСТАТОЧНЫЙ")
	}
	fmt.Printf("Лимит процессов пользователя (ulimit -u): %d %s\n", limits.MaxUserProcesses, procStatus)

	// Проверяем и выводим TCP backlog
	backlogStatus := color.GreenString("OK")
	if limits.TCPMaxSynBacklog < 1024 {
		backlogStatus = color.RedString("НИЗКИЙ")
	} else if limits.TCPMaxSynBacklog < 4096 {
		backlogStatus = color.YellowString("ДОСТАТОЧНЫЙ")
	}
	fmt.Printf("TCP max syn backlog: %d %s\n", limits.TCPMaxSynBacklog, backlogStatus)

	// Проверяем и выводим somaxconn
	somaxStatus := color.GreenString("OK")
	if limits.SomaxConn < 1024 {
		somaxStatus = color.RedString("НИЗКИЙ")
	} else if limits.SomaxConn < 4096 {
		somaxStatus = color.YellowString("ДОСТАТОЧНЫЙ")
	}
	fmt.Printf("net.core.somaxconn: %d %s\n", limits.SomaxConn, somaxStatus)

	// Проверяем и выводим max_map_count
	mapCountStatus := color.GreenString("OK")
	if limits.MaxMapCount < 262144 {
		mapCountStatus = color.RedString("НИЗКИЙ")
	} else if limits.MaxMapCount < 500000 {
		mapCountStatus = color.YellowString("ДОСТАТОЧНЫЙ")
	}
	fmt.Printf("vm.max_map_count: %d %s\n", limits.MaxMapCount, mapCountStatus)

	// Вывод предупреждений и рекомендаций, если лимиты слишком низкие
	if limits.MaxOpenFiles < 100000 || limits.MaxUserProcesses < 30000 ||
		limits.TCPMaxSynBacklog < 4096 || limits.SomaxConn < 4096 ||
		limits.MaxMapCount < 500000 {
		color.Red("\nВНИМАНИЕ! Системные лимиты могут ограничивать производительность тестов.")
		fmt.Println("Рекомендуемые значения для нагрузочного тестирования:")
		fmt.Println("  Без прав sudo (только для текущей сессии):")
		fmt.Println("  ulimit -n 1000000        # Лимит открытых файлов (требуется соответствующий hard limit)")
		fmt.Println("  ulimit -u 100000         # Лимит процессов (требуется соответствующий hard limit)")
		fmt.Println("")
		fmt.Println("  С правами sudo:")
		fmt.Println("  sudo sysctl -w net.ipv4.tcp_max_syn_backlog=8192")
		fmt.Println("  sudo sysctl -w net.core.somaxconn=8192")
		fmt.Println("  sudo sysctl -w vm.max_map_count=1000000")
		fmt.Println("\nПроверьте текущие жесткие лимиты командами:")
		fmt.Println("  ulimit -Hn               # Жесткий лимит открытых файлов")
		fmt.Println("  ulimit -Hu               # Жесткий лимит процессов")
		fmt.Println("\nДля постоянных изменений добавьте параметры в /etc/sysctl.conf и /etc/security/limits.conf")
	}

	fmt.Println()
}

// Функция для попытки повышения системных лимитов
func tryRaiseLimits(limits *SystemLimits) {
	color.Yellow("=== ПОПЫТКА ПОВЫШЕНИЯ СИСТЕМНЫХ ЛИМИТОВ ДЛЯ ВЫСОКОНАГРУЖЕННОГО ТЕСТИРОВАНИЯ ===")

	// Проверяем, доступен ли sudo без пароля
	checkSudoCmd := exec.Command("sudo", "-n", "true")
	sudoAvailable := checkSudoCmd.Run() == nil

	// Первый этап: установка жестких лимитов через sudo, если доступно
	if sudoAvailable {
		fmt.Println("Шаг 1: Установка жестких лимитов через sudo")

		// Установка жесткого лимита на открытые файлы
		fmt.Printf("Попытка установить жесткий лимит открытых файлов (nofile)... ")
		cmd := exec.Command("sudo", "bash", "-c", "echo '* hard nofile 10000000' > /etc/security/limits.d/99-loadtest-limits.conf")
		if out, err := cmd.CombinedOutput(); err != nil {
			color.Red("Ошибка: %v, вывод: %s\n", err, string(out))
		} else {
			color.Green("Успешно\n")
		}

		// Установка жесткого лимита на процессы
		fmt.Printf("Попытка установить жесткий лимит процессов (nproc)... ")
		cmd = exec.Command("sudo", "bash", "-c", "echo '* hard nproc 500000' >> /etc/security/limits.d/99-loadtest-limits.conf")
		if out, err := cmd.CombinedOutput(); err != nil {
			color.Red("Ошибка: %v, вывод: %s\n", err, string(out))
		} else {
			color.Green("Успешно\n")
		}

		// Установка мягкого лимита на открытые файлы
		fmt.Printf("Попытка установить мягкий лимит открытых файлов (nofile)... ")
		cmd = exec.Command("sudo", "bash", "-c", "echo '* soft nofile 10000000' >> /etc/security/limits.d/99-loadtest-limits.conf")
		if out, err := cmd.CombinedOutput(); err != nil {
			color.Red("Ошибка: %v, вывод: %s\n", err, string(out))
		} else {
			color.Green("Успешно\n")
		}

		// Установка мягкого лимита на процессы
		fmt.Printf("Попытка установить мягкий лимит процессов (nproc)... ")
		cmd = exec.Command("sudo", "bash", "-c", "echo '* soft nproc 500000' >> /etc/security/limits.d/99-loadtest-limits.conf")
		if out, err := cmd.CombinedOutput(); err != nil {
			color.Red("Ошибка: %v, вывод: %s\n", err, string(out))
		} else {
			color.Green("Успешно\n")
		}

		// Применение лимитов к текущему пользователю через pam_limits
		fmt.Println("Установленные лимиты будут действовать при следующем входе в систему.")
		fmt.Println("Для применения некоторых лимитов без перезахода может потребоваться перезапуск сессии.")
	}

	// Второй этап: повышение лимитов для текущего процесса
	fmt.Println("\nШаг 2: Повышение лимитов для текущего процесса")

	// Повышение лимита открытых файлов
	fmt.Printf("Попытка повысить лимит открытых файлов до 10000000... ")

	// Получаем текущий жесткий лимит
	var rLimit syscall.Rlimit
	err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		color.Red("Ошибка получения текущего лимита: %v\n", err)
	} else {
		// Запоминаем текущий жесткий лимит
		hardLimit := rLimit.Max

		// Устанавливаем новое значение, не превышающее жесткий лимит
		newLimit := uint64(10000000)
		if newLimit > hardLimit {
			newLimit = hardLimit
		}

		rLimit.Cur = newLimit
		err = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit)
		if err != nil {
			color.Red("Ошибка: %v\n", err)
			fmt.Printf("Жесткий лимит файлов: %d, пытались установить: %d\n", hardLimit, newLimit)
		} else {
			color.Green("Успешно установлено значение %d\n", newLimit)
		}
	}

	// Повышение лимита процессов
	fmt.Printf("Попытка повысить лимит процессов до 500000... ")

	// Получаем текущий жесткий лимит
	var rLimitProc syscall.Rlimit
	// RLIMIT_NPROC это 6 в Linux
	err = syscall.Getrlimit(6, &rLimitProc)
	if err != nil {
		color.Red("Ошибка получения текущего лимита: %v\n", err)
	} else {
		// Запоминаем текущий жесткий лимит
		hardLimit := rLimitProc.Max

		// Устанавливаем новое значение, не превышающее жесткий лимит
		newLimit := uint64(500000)
		if newLimit > hardLimit {
			newLimit = hardLimit
		}

		rLimitProc.Cur = newLimit
		err = syscall.Setrlimit(6, &rLimitProc)
		if err != nil {
			color.Red("Ошибка: %v\n", err)
			fmt.Printf("Жесткий лимит процессов: %d, пытались установить: %d\n", hardLimit, newLimit)
		} else {
			color.Green("Успешно установлено значение %d\n", newLimit)
		}
	}

	// Третий этап: повышение системных параметров через sudo
	fmt.Println("\nШаг 3: Повышение параметров ядра")

	if sudoAvailable {
		// Массив параметров для установки
		kernelParams := []struct {
			name  string
			value string
		}{
			{"net.ipv4.tcp_max_syn_backlog", "65536"},
			{"net.core.somaxconn", "65536"},
			{"net.core.netdev_max_backlog", "65536"},
			{"net.ipv4.tcp_mem", "25600 51200 102400"},
			{"net.ipv4.tcp_rmem", "4096 87380 16777216"},
			{"net.ipv4.tcp_wmem", "4096 65536 16777216"},
			{"net.ipv4.ip_local_port_range", "1024 65535"},
			{"vm.max_map_count", "5000000"},
			{"vm.swappiness", "10"},
			{"net.ipv4.tcp_fin_timeout", "30"},
			{"net.ipv4.tcp_tw_reuse", "1"},
		}

		for _, param := range kernelParams {
			fmt.Printf("Установка %s = %s... ", param.name, param.value)
			cmd := exec.Command("sudo", "sysctl", "-w", fmt.Sprintf("%s=%s", param.name, param.value))
			if out, err := cmd.CombinedOutput(); err != nil {
				color.Red("Ошибка: %v, вывод: %s\n", err, string(out))
			} else {
				color.Green("Успешно\n")
			}
		}

		// Опционально: сохранение параметров для загрузки при перезагрузке
		fmt.Printf("Сохранение параметров ядра во временный файл... ")
		cmd := exec.Command("sudo", "bash", "-c", "echo '# Временные параметры для высоконагруженного тестирования' > /etc/sysctl.d/99-loadtest.conf")
		if out, err := cmd.CombinedOutput(); err != nil {
			color.Red("Ошибка: %v, вывод: %s\n", err, string(out))
		} else {
			color.Green("Успешно\n")

			// Добавляем каждый параметр в файл
			for _, param := range kernelParams {
				cmd := exec.Command("sudo", "bash", "-c", fmt.Sprintf("echo '%s = %s' >> /etc/sysctl.d/99-loadtest.conf", param.name, param.value))
				cmd.Run() // Игнорируем ошибки
			}
		}
	} else {
		color.Yellow("Sudo без пароля недоступен, пропускаем повышение системных параметров ядра")
	}

	// Проверяем, были ли лимиты успешно изменены
	newLimits := checkSystemLimits()
	fmt.Println("\nИтоговые значения лимитов:")
	fmt.Printf("Лимит открытых файлов: %d (было %d)\n", newLimits.MaxOpenFiles, limits.MaxOpenFiles)
	fmt.Printf("Лимит процессов: %d (было %d)\n", newLimits.MaxUserProcesses, limits.MaxUserProcesses)
	fmt.Printf("TCP max syn backlog: %d (было %d)\n", newLimits.TCPMaxSynBacklog, limits.TCPMaxSynBacklog)
	fmt.Printf("net.core.somaxconn: %d (было %d)\n", newLimits.SomaxConn, limits.SomaxConn)
	fmt.Printf("vm.max_map_count: %d (было %d)\n", newLimits.MaxMapCount, limits.MaxMapCount)

	if sudoAvailable {
		color.Yellow("\nВНИМАНИЕ: Некоторые изменения требуют перезахода в систему или перезагрузки.")
		fmt.Println("Для удаления временных файлов после тестирования выполните:")
		fmt.Println("  sudo rm /etc/sysctl.d/99-loadtest.conf /etc/security/limits.d/99-loadtest-limits.conf")
		fmt.Println("  sudo sysctl --system")
	}

	fmt.Println()
}

// Проверяет, поддерживает ли текущий терминал необходимые возможности для интерактивного UI
func checkTerminalSupport() bool {
	// Проверяем, запущено ли приложение в терминале
	if !isTerminal(os.Stdout.Fd()) {
		return false
	}

	// Проверяем переменную окружения TERM
	term := os.Getenv("TERM")
	if term == "dumb" || term == "" {
		return false
	}

	return true
}

// isTerminal проверяет, является ли файловый дескриптор терминалом
func isTerminal(fd uintptr) bool {
	var termios syscall.Termios
	_, _, err := syscall.Syscall6(syscall.SYS_IOCTL, fd, syscall.TCGETS, uintptr(unsafe.Pointer(&termios)), 0, 0, 0)
	return err == 0
}

// main функция - точка входа в программу
func main() {
	// Проверяем системные лимиты и выводим информацию о них
	limits := checkSystemLimits()
	logSystemLimits(limits)

	// Пытаемся повысить лимиты, если они слишком низкие
	if limits.MaxOpenFiles < 100000 || limits.MaxUserProcesses < 30000 ||
		limits.TCPMaxSynBacklog < 4096 || limits.SomaxConn < 4096 ||
		limits.MaxMapCount < 500000 {

		tryRaiseLimits(limits)
	}

	// Определяем флаги командной строки
	baseURL := pflag.String("url", "http://localhost:8080", "Базовый URL для тестирования")
	duration := pflag.Duration("duration", 2*time.Second, "Продолжительность каждого теста")
	maxParallelism := pflag.Int("max-parallelism", 32768, "Максимальный уровень параллелизма (2^15)")
	maxDepth := pflag.Int("max-depth", 128, "Максимальная глубина (2^7)")
	resultsFile := pflag.String("results", "matrix-results.csv", "Файл результатов CSV")
	statsFile := pflag.String("stats", "full-stats.txt", "Файл подробной статистики")
	appManagerPath := pflag.String("app-manager", "./app-manager.sh", "Путь к скрипту app-manager")
	appName := pflag.String("app", "", "Имя приложения для тестирования из списка app-manager (по умолчанию - все)")
	listApps := pflag.Bool("list-apps", false, "Показать список доступных приложений")
	singleTest := pflag.Bool("single-test", false, "Выполнить только один тест без полной матрицы")
	parallelismValue := pflag.Int("parallelism", 1, "Значение параллелизма для одиночного теста")
	depthValue := pflag.Int("depth", 1, "Значение глубины для одиночного теста")
	showTable := pflag.Bool("show-table", true, "Показывать таблицу результатов после каждого теста")
	useTerminalUI := pflag.Bool("terminal-ui", true, "Использовать интерактивный интерфейс терминала с логами")
	logHeight := pflag.Int("log-height", 20, "Высота области логов в интерактивном режиме (внизу экрана всегда остается запас в 10 строк)")

	pflag.Parse()

	// Создаем папку для статистики, если она не существует
	statDir := "stat"
	if err := os.MkdirAll(statDir, 0755); err != nil {
		log.Fatalf("Ошибка создания папки статистики: %v", err)
	}

	// Создаем уникальную подпапку для текущего запуска
	runDir := createRunDirectory(statDir)

	// Проверяем, что скрипт app-manager существует и доступен
	if _, err := os.Stat(*appManagerPath); os.IsNotExist(err) {
		log.Fatalf("Ошибка: скрипт app-manager не найден по пути %s", *appManagerPath)
	}

	// Делаем скрипт исполняемым, если он не исполняемый
	os.Chmod(*appManagerPath, 0755)

	// Если запрошен список приложений
	if *listApps {
		cmd := exec.Command(*appManagerPath, "list")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Fatalf("Ошибка получения списка приложений: %v", err)
		}
		return
	}

	// Создаем общие файлы результатов для всех приложений
	commonResultsFile := filepath.Join(runDir, *resultsFile)
	commonStatsFile := filepath.Join(runDir, *statsFile)

	// Определяем список приложений для тестирования
	var appsToTest []string

	if *appName != "" {
		// Используем указанное приложение
		appsToTest = []string{*appName}
	} else {
		// Получаем список всех доступных приложений
		var err error
		appsToTest, err = GetAppList(*appManagerPath)
		if err != nil {
			log.Fatalf("Ошибка получения списка приложений: %v", err)
		}

		if len(appsToTest) == 0 {
			log.Fatalf("Не найдено доступных приложений для тестирования")
		}

		log.Printf("Будет выполнено тестирование для %d приложений: %s", len(appsToTest), strings.Join(appsToTest, ", "))
	}

	// Информация о начале тестирования
	startTime := time.Now()
	color.Green("=== НАЧАЛО ТЕСТИРОВАНИЯ (Май 2025) ===")
	color.Green("Базовый URL: %s", *baseURL)
	color.Green("Время теста: %s", *duration)
	color.Green("Макс. параллельность: %d", *maxParallelism)
	color.Green("Макс. глубина: %d", *maxDepth)
	color.Green("Результаты: %s", commonResultsFile)
	fmt.Println()

	// Инициализируем интерактивный интерфейс терминала, если он включен
	if *useTerminalUI && checkTerminalSupport() {
		termUI = NewTerminalUI(*logHeight)
		termUI.Start()
		defer termUI.Stop()
	} else if *useTerminalUI {
		// Если интерактивный режим запрошен, но не поддерживается
		log.Println("Интерактивный режим терминала не поддерживается, используем обычный вывод")
		*useTerminalUI = false
	}

	// Запускаем тесты для каждого приложения
	for i, app := range appsToTest {
		if i > 0 {
			// Добавляем разделитель между тестами разных приложений
			color.Blue("\n\n===================================================")
			color.Blue("===================================================\n\n")
		}

		color.HiYellow("ЗАПУСК ТЕСТИРОВАНИЯ ДЛЯ ПРИЛОЖЕНИЯ: %s (%d из %d)", app, i+1, len(appsToTest))

		if *singleTest {
			// Для одиночного теста создаем тестер и выполняем только один тест
			tester := NewTester(
				*baseURL,
				*duration,
				*maxParallelism, // для создания тестера используем максимальные значения
				*maxDepth,
				commonResultsFile,
				commonStatsFile,
				*appManagerPath,
				app,
			)

			// Запускаем один тест с указанными параметрами
			color.Cyan("Запускаем одиночный тест с параллелизмом=%d, глубиной=%d для приложения %s",
				*parallelismValue, *depthValue, app)

			// Инициализируем файлы результатов при первом запуске
			if i == 0 {
				if err := tester.InitResultFiles(); err != nil {
					log.Printf("Ошибка при инициализации файлов результатов: %v", err)
				}
			}

			// Выполняем прогрев приложения одним запросом
			tester.PerformWarmup()

			// Запускаем тест с защитой от паники
			var result *TestResult
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("Паника при выполнении теста: %v", r)
					}
				}()
				result = tester.RunMatrixTest(*parallelismValue, *depthValue)
			}()

			if result != nil {
				log.Printf("Тест завершен успешно, сохраняем результаты")
				if err := tester.SaveResult(result); err != nil {
					log.Printf("Ошибка при сохранении результатов: %v", err)
				}

				// Добавляем результат в глобальный список
				GlobalTestResults.Mutex.Lock()
				GlobalTestResults.Results = append(GlobalTestResults.Results, result)
				GlobalTestResults.Mutex.Unlock()

				// Показываем обновленную таблицу, если нужно
				if *showTable {
					PrintGlobalResultsTable()
				}
			} else {
				log.Printf("Тест завершился без результатов")
			}

			color.Green("Тестирование приложения %s завершено", app)
		} else {
			// Запускаем полное матричное тестирование для текущего приложения
			RunAppMatrixTest(
				app,
				*baseURL,
				*duration,
				*maxParallelism,
				*maxDepth,
				commonResultsFile,
				commonStatsFile,
				*appManagerPath,
				*showTable,
			)
		}

		// Выводим таблицу результатов для всех приложений
		if i < len(appsToTest)-1 {
			if *useTerminalUI {
				// В терминальном интерфейсе обновление происходит автоматически
				// time.Sleep(2 * time.Second) // Даем время на просмотр текущих результатов
			} else {
				PrintGlobalResultsTable()
			}
			color.Cyan("=== Переходим к следующему приложению ===")
			fmt.Println()
		}
	}

	// Выводим итоговую информацию
	totalDuration := time.Since(startTime)
	color.HiGreen("\nТестирование всех приложений завершено!")
	color.HiGreen("Результаты сохранены в папке: %s", runDir)
	color.Green("Всего протестировано приложений: %d", len(appsToTest))
	color.Green("Общее время тестирования: %s", totalDuration)

	// Выводим полную итоговую таблицу
	fmt.Println()

	// Если используем терминальный UI, то даем время на просмотр последних результатов
	if *useTerminalUI {
		// time.Sleep(5 * time.Second)
	} else {
		PrintGlobalResultsTable()
	}
}

// Создает новую папку для текущего запуска с инкрементальным номером
func createRunDirectory(statDir string) string {
	// Проверяем существующие папки запусков
	entries, err := os.ReadDir(statDir)
	if err != nil {
		log.Printf("Ошибка чтения папки статистики: %v", err)
		entries = []os.DirEntry{}
	}

	// Находим максимальный номер существующих папок
	maxRun := 0
	for _, entry := range entries {
		if entry.IsDir() {
			var runNum int
			if _, err := fmt.Sscanf(entry.Name(), "run%d", &runNum); err == nil {
				if runNum > maxRun {
					maxRun = runNum
				}
			}
		}
	}

	// Создаем новую папку с инкрементальным номером
	newRunNum := maxRun + 1
	runDir := filepath.Join(statDir, fmt.Sprintf("run%d", newRunNum))

	if err := os.MkdirAll(runDir, 0755); err != nil {
		log.Fatalf("Ошибка создания папки запуска: %v", err)
	}

	return runDir
}

// Очистка имени файла от ANSI-последовательностей и других некорректных символов
func cleanFileName(name string) string {
	// Удаляем ANSI escape последовательности
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	name = ansiRegex.ReplaceAllString(name, "")

	// Заменяем недопустимые символы в именах файлов
	forbidden := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}
	for _, char := range forbidden {
		name = strings.ReplaceAll(name, char, "_")
	}

	return name
}

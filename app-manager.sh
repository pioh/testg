#!/bin/bash

# Опции для безопасного выполнения
set -eo pipefail # выход при ошибках и ошибках в пайплайне

# Цвета для вывода
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Файл для хранения информации о запущенных приложениях в корне проекта
RUNNING_APPS_FILE="./running_apps.txt"
# Файл для хранения логов приложений
LOG_FILE="./app_output.log"

# Переменная для хранения PID запущенного приложения в интерактивном режиме
INTERACTIVE_APP_PID=""
# Убиваем зомби-tail'ы перед любыми операциями, чтобы не подрядились мусорные слежки
pkill -f "tail -F $LOG_FILE" >/dev/null 2>&1 || true

# Очищаем лог-файл при старте скрипта
: >"$LOG_FILE"

# Загружаем конфигурацию приложений из отдельного файла
if [[ ! -f ./apps.sh ]]; then
    echo -e "${RED}Ошибка: файл конфигурации apps.sh не найден${NC}"
    exit 1
fi

source ./apps.sh

# Функция для логирования ошибок и выхода
error_exit() {
    echo -e "${RED}ОШИБКА: $1${NC}" >&2
    exit 1
}

# Функция для получения PID процесса, слушающего порт 8080
get_port_pid() {
    # Получить PID процесса, который слушает на порту 8080
    local pid=$(ss -tulpn 2>/dev/null | grep ':8080' | grep -oP 'pid=\K[0-9]+' || true)

    if [[ -z "$pid" ]]; then
        echo ""
        return 1
    elif [[ $(echo "$pid" | wc -l) -gt 1 ]]; then
        # Если несколько PID, берем первый и логируем предупреждение
        echo -e "${YELLOW}Обнаружено несколько процессов на порту 8080: ${pid}${NC}" >&2
        echo "$(echo "$pid" | head -1)"
        return 2
    else
        echo "$pid"
        return 0
    fi
}

# Функция для обработки сигнала Ctrl+C в интерактивном режиме
handle_sigint() {
    echo -e "\n${YELLOW}Получен сигнал завершения. Останавливаем приложение...${NC}"

    if [[ -n "$INTERACTIVE_APP_PID" ]] && kill -0 $INTERACTIVE_APP_PID >/dev/null 2>&1; then
        stop_app_by_pid "$INTERACTIVE_APP_PID"
    else
        # Проверяем, есть ли процесс на порту 8080
        local port_pid=$(get_port_pid)
        local port_status=$?

        if [[ $port_status -eq 0 && -n "$port_pid" ]]; then
            echo -e "${YELLOW}Обнаружен процесс на порту 8080 с PID ${port_pid}. Останавливаем...${NC}"
            stop_app_by_pid "$port_pid"
        else
            stop_all_apps
        fi
    fi
    # Если осталась слежка за логом — добиваем её
    if [[ -n "$tail_pid" ]]; then
        kill "$tail_pid" >/dev/null 2>&1 || true
    fi
    exit 0
}

# Функция для запуска приложения
start_app() {
    local app_name=$1
    local interactive_mode=$2
    local start_command=${APPS[$app_name]}

    if [[ -z "$start_command" ]]; then
        error_exit "Неизвестное приложение ${app_name}. Используйте: $0 list для просмотра доступных приложений"
    fi

    echo -e "${GREEN}Запуск приложения: ${app_name}${NC}"
    echo -e "${BLUE}Команда: ${start_command}${NC}"

    # Обновляем лог-файл перед запуском
    : >"$LOG_FILE"

    # Останавливаем все запущенные приложения перед запуском нового
    stop_all_apps || error_exit "Не удалось остановить запущенные приложения"

    # Проверяем, что на порту 8080 ничего не запущено
    local existing_pid=$(get_port_pid)
    local port_status=$?

    if [[ $port_status -eq 0 && -n "$existing_pid" ]]; then
        echo -e "${YELLOW}Порт 8080 уже занят процессом с PID ${existing_pid}. Попытка остановить...${NC}"
        kill -9 "$existing_pid" >/dev/null 2>&1 || true
        sleep 2

        # Повторная проверка
        existing_pid=$(get_port_pid)
        port_status=$?
        if [[ $port_status -eq 0 && -n "$existing_pid" ]]; then
            error_exit "Не удалось освободить порт 8080, процесс ${existing_pid} всё еще активен"
        fi
    fi

    # Измеряем время старта
    local start_time=$(date +%s.%N)
    local app_pid=""

    # Запускаем приложение без отображения вывода на экран
    echo -e "${YELLOW}Запуск приложения...${NC}"

    # Запускаем процесс в фоне, перенаправляем все выводы в лог-файл
    eval "$start_command &> $LOG_FILE &"

    echo -e "${YELLOW}Ожидаем запуска приложения...${NC}"

    # Ожидаем, пока приложение не будет готово принимать запросы
    local timeout=120 # Увеличиваем таймаут до 120 секунд
    local counter=0

    while [[ $counter -lt $timeout ]]; do
        sleep 1
        counter=$((counter + 1))

        # Проверяем PID на порту 8080
        local real_pid=$(get_port_pid)
        port_status=$?

        if [[ $port_status -eq 0 && -n "$real_pid" ]]; then
            echo -e "${GREEN}Обнаружено приложение с PID ${real_pid} на порту 8080${NC}"
            # Считаем что любой процесс, найденный на порту 8080 после запуска команды,
            # должен относиться к нашему приложению (если предварительно порт был свободен)
            app_pid="$real_pid"
            break
        fi
    done

    # Если не удалось определить PID на порту 8080 за timeout
    if [[ -z "$app_pid" ]]; then
        echo -e "${RED}Не удалось определить PID приложения на порту 8080 за ${timeout} секунд!${NC}"

        # Вывод логов только в интерактивном режиме или при критической ошибке
        if [[ "$interactive_mode" == true ]]; then
            echo -e "${RED}Последние строки вывода:${NC}"
            tail -n 20 "$LOG_FILE" 2>/dev/null
        fi

        # Убиваем все процессы, которые были запущены недавно и могут относиться к нашей команде
        pkill -f "$start_command" || true

        error_exit "Таймаут ожидания приложения на порту 8080"
    fi

    # Сохраняем PID приложения для интерактивного режима
    INTERACTIVE_APP_PID=$app_pid

    # Проверяем доступность приложения
    local timeout_requests=30 # секунд
    local counter=0
    local successful_requests=0
    local required_successful_requests=100
    local start_check_time=$(date +%s.%N)

    echo -e "${YELLOW}Проверка доступности приложения (параллельно, left=0):${NC}"

    # Параллельная проверка доступности
    while [[ $(echo "$counter < $timeout_requests" | bc -l) -eq 1 && $successful_requests -lt $required_successful_requests ]]; do
        # Запускаем несколько запросов параллельно
        local batch_size=$((required_successful_requests - successful_requests))
        if [[ $batch_size -gt 100 ]]; then
            batch_size=100
        fi

        echo -n "Запускаем $batch_size параллельных запросов: "

        # Массив для хранения PID процессов curl
        local pids=()
        local results_file=$(mktemp)

        # Запускаем запросы параллельно
        for ((i = 0; i < batch_size; i++)); do
            (curl -s "http://localhost:8080/check?left=0" >/dev/null && echo "1" >>"$results_file" || echo "0" >>"$results_file") &
            pids+=($!)
        done

        # Ожидаем завершения всех запросов
        for pid in "${pids[@]}"; do
            wait $pid || true
        done

        # Подсчитываем успешные запросы
        local batch_success=$(grep -c "1" "$results_file" || echo "0")
        successful_requests=$((successful_requests + batch_success))

        # Очищаем временный файл
        rm -f "$results_file"

        # Выводим статус
        for ((i = 0; i < batch_success; i++)); do
            echo -n "+"
        done
        for ((i = 0; i < (batch_size - batch_success); i++)); do
            echo -n "."
        done
        echo " ($successful_requests/$required_successful_requests)"

        # Если все запросы успешны, выходим из цикла
        if [[ $successful_requests -ge $required_successful_requests ]]; then
            break
        fi

        # Увеличиваем счетчик времени
        counter=$(echo "$(date +%s.%N) - $start_check_time" | bc -l)

        # Проверяем, жив ли еще процесс приложения
        if ! ps -p $app_pid >/dev/null 2>&1; then
            echo -e "${RED}Процесс с PID ${app_pid} завершился неожиданно во время проверки!${NC}"
            # Вывод логов только в интерактивном режиме или при ошибке
            if [[ "$interactive_mode" == true ]]; then
                echo -e "${RED}Последние строки вывода:${NC}"
                tail -n 20 "$LOG_FILE" 2>/dev/null
            fi
            error_exit "Процесс завершился неожиданно во время проверки"
        fi
    done
    echo ""

    local end_time=$(date +%s.%N)
    local startup_time=$(echo "$end_time - $start_time" | bc -l)

    if [[ $successful_requests -ge $required_successful_requests ]]; then
        echo -e "${GREEN}Приложение успешно запущено за ${startup_time} секунд!${NC}"
        echo -e "${GREEN}Успешно выполнено ${successful_requests} запросов${NC}"

        # Финальная проверка PID через ss
        local final_pid=$(get_port_pid)
        port_status=$?

        if [[ $port_status -eq 0 && -n "$final_pid" && "$final_pid" = "$app_pid" ]]; then
            echo -e "${GREEN}Подтверждено: PID ${app_pid} слушает на порту 8080${NC}"
        else
            echo -e "${RED}ОШИБКА: PID на порту 8080 изменился или отсутствует!${NC}"
            [[ $port_status -eq 0 ]] && echo -e "${RED}Текущий PID на порту: ${final_pid}, ожидаемый: ${app_pid}${NC}"
            kill -9 "$app_pid" >/dev/null 2>&1 || true
            error_exit "PID на порту 8080 не соответствует ожидаемому"
        fi

        # Сохраняем информацию о запущенном приложении в файл в корне проекта
        echo "$app_pid:$app_name:$(date +%s)" >>"$RUNNING_APPS_FILE"

        # Выводим PID и время запуска в формате JSON или продолжаем интерактивный режим
        if [[ "$interactive_mode" = true ]]; then
            echo -e "${GREEN}Приложение запущено в интерактивном режиме. Нажмите Ctrl+C для завершения.${NC}"
            echo -e "${BLUE}Вывод приложения:${NC}"

            # В интерактивном режиме просто запускаем tail для отслеживания логов
            tail --quiet -F "$LOG_FILE" &
            tail_pid=$!

            # Ждем, пока пользователь не нажмет Ctrl+C или приложение не завершится
            echo -e "${YELLOW}Приложение запущено с PID ${app_pid}. Для остановки нажмите Ctrl+C...${NC}"

            # Простой цикл для отслеживания состояния приложения
            while kill -0 $app_pid >/dev/null 2>&1; do
                sleep 1
            done

            # Останавливаем tail если приложение завершилось само
            if ps -p $tail_pid >/dev/null 2>&1; then
                kill $tail_pid >/dev/null 2>&1 || true
            fi

            echo -e "${YELLOW}Приложение завершилось.${NC}"

            # Удаляем информацию из файла
            if [[ -f "$RUNNING_APPS_FILE" ]]; then
                grep -v "^$app_pid:" "$RUNNING_APPS_FILE" >"${RUNNING_APPS_FILE}.tmp" || true
                mv "${RUNNING_APPS_FILE}.tmp" "$RUNNING_APPS_FILE"
            fi
        else
            echo "{\"pid\":$app_pid,\"startup_time\":$startup_time,\"app_name\":\"$app_name\"}"
        fi

        return 0
    else
        echo -e "${RED}Не удалось запустить приложение в течение $timeout_requests секунд!${NC}"
        echo -e "${RED}Успешно выполнено только ${successful_requests} запросов из ${required_successful_requests}${NC}"

        # Вывод логов только в интерактивном режиме или при критической ошибке
        if [[ "$interactive_mode" == true && -f "$LOG_FILE" ]]; then
            echo -e "${RED}Последние строки вывода:${NC}"
            tail -n 20 "$LOG_FILE" 2>/dev/null
        fi

        # Пытаемся остановить приложение
        if [[ -n "$app_pid" ]]; then
            kill -9 "$app_pid" >/dev/null 2>&1 || true
        fi

        # На всякий случай убираем все процессы, связанные с командой запуска
        pkill -f "$start_command" || true

        error_exit "Не удалось подтвердить доступность приложения"
    fi
}

# Функция для остановки приложения по PID
stop_app_by_pid() {
    local pid=$1
    if [[ -z "$pid" ]]; then
        error_exit "Не указан PID"
    fi

    if ! ps -p "$pid" >/dev/null 2>&1; then
        echo -e "${YELLOW}Процесс с PID $pid уже не существует${NC}"
        # Удаляем информацию из файла
        if [[ -f "$RUNNING_APPS_FILE" ]]; then
            grep -v "^$pid:" "$RUNNING_APPS_FILE" >"${RUNNING_APPS_FILE}.tmp" || true
            mv "${RUNNING_APPS_FILE}.tmp" "$RUNNING_APPS_FILE"
        fi
        return 0
    fi

    # Проверяем, слушает ли процесс порт 8080
    local port_pid=$(get_port_pid)
    local port_status=$?
    local is_listening=false

    if [[ $port_status -eq 0 && -n "$port_pid" && "$port_pid" = "$pid" ]]; then
        echo -e "${YELLOW}Процесс с PID $pid слушает порт 8080${NC}"
        is_listening=true
    fi

    echo -e "${YELLOW}Останавливаем приложение с PID: ${pid}...${NC}"
    kill "$pid" >/dev/null 2>&1 || true

    # Ожидаем завершения процесса
    local counter=0
    while ps -p "$pid" >/dev/null 2>&1 && [[ $counter -lt 5 ]]; do
        sleep 1
        counter=$((counter + 1))
    done

    # Если процесс все еще жив, используем SIGKILL
    if ps -p "$pid" >/dev/null 2>&1; then
        echo -e "${RED}Приложение не завершилось, принудительно завершаем...${NC}"
        kill -9 "$pid" >/dev/null 2>&1 || true
        sleep 1
    fi

    # Удаляем информацию из файла
    if [[ -f "$RUNNING_APPS_FILE" ]]; then
        grep -v "^$pid:" "$RUNNING_APPS_FILE" >"${RUNNING_APPS_FILE}.tmp" || true
        mv "${RUNNING_APPS_FILE}.tmp" "$RUNNING_APPS_FILE"
    fi

    # Проверяем, что процесс действительно остановлен
    if ! ps -p "$pid" >/dev/null 2>&1; then
        echo -e "${GREEN}Приложение остановлено.${NC}"

        # Если процесс слушал порт 8080, проверяем что порт освободился
        if [[ "$is_listening" = true ]]; then
            local new_port_pid=$(get_port_pid)
            local new_port_status=$?

            if [[ $new_port_status -eq 0 && -n "$new_port_pid" ]]; then
                echo -e "${RED}ВНИМАНИЕ: Порт 8080 всё еще занят процессом ${new_port_pid}${NC}"
                if [[ "$new_port_pid" != "$pid" ]]; then
                    echo -e "${YELLOW}Это другой процесс. Возможно, запущено несколько экземпляров приложения.${NC}"
                else
                    echo -e "${RED}Странно, процесс с тем же PID всё еще слушает порт. Пытаемся остановить принудительно...${NC}"
                    kill -9 "$pid" >/dev/null 2>&1 || true
                    sleep 1

                    local final_check=$(get_port_pid)
                    if [[ $? -eq 0 && -n "$final_check" ]]; then
                        echo -e "${RED}Не удалось освободить порт 8080!${NC}"
                        return 1
                    fi
                fi
            else
                echo -e "${GREEN}Порт 8080 успешно освобожден${NC}"
            fi
        fi

        return 0
    else
        echo -e "${RED}Не удалось остановить приложение с PID ${pid}${NC}"
        return 1
    fi
}

# Функция для остановки приложения по имени
stop_app_by_name() {
    local app_name=$1
    local found=false
    local pids=()

    if [[ -z "$app_name" ]]; then
        error_exit "Не указано имя приложения"
    fi

    if [[ -f "$RUNNING_APPS_FILE" ]]; then
        while IFS=: read -r pid name timestamp; do
            if [[ "$name" = "$app_name" ]]; then
                pids+=("$pid")
                found=true
            fi
        done <"$RUNNING_APPS_FILE" || true
    fi

    if [[ "$found" = false ]]; then
        echo -e "${YELLOW}Нет запущенных приложений с именем ${app_name}${NC}"
        return 1
    fi

    local result=0
    for pid in "${pids[@]}"; do
        stop_app_by_pid "$pid" || result=1
    done

    return $result
}

# Функция для остановки всех приложений
stop_all_apps() {
    local found=false
    local pids=()
    local valid_pids=()

    # Сначала проверяем, есть ли процессы на порту 8080
    local port_pid=$(get_port_pid)
    local port_status=$?
    local port_occupied=false
    local port_pid_info=""

    if [[ $port_status -eq 0 && -n "$port_pid" ]]; then
        echo -e "${YELLOW}Обнаружен процесс с PID ${port_pid} на порту 8080${NC}"
        # Пытаемся вытащить имя и таймштамп из файла, иначе помечаем unknown
        if [[ -f "$RUNNING_APPS_FILE" ]]; then
            name=$(grep -E "^${port_pid}:" "$RUNNING_APPS_FILE" | cut -d':' -f2)
            timestamp=$(grep -E "^${port_pid}:" "$RUNNING_APPS_FILE" | cut -d':' -f3)
        fi
        name=${name:-unknown}
        timestamp=${timestamp:-$(date +%s)}
        pids+=("${port_pid}:${name}:${timestamp}")
        found=true
    fi

    if [[ -f "$RUNNING_APPS_FILE" ]]; then
        # Читаем все PIDs из файла
        while IFS=: read -r pid name timestamp; do
            # Проверяем, не добавили ли мы уже этот PID из порта 8080
            if [[ -n "$pid" && "$pid" != "$port_pid" ]]; then
                pids+=("$pid:$name:$timestamp")
                found=true
            fi
        done <"$RUNNING_APPS_FILE" || true
    fi

    if [[ "$found" = false ]]; then
        echo -e "${YELLOW}Нет запущенных приложений${NC}"
        return 0
    fi

    # Очищаем файл перед обновлением
    >"$RUNNING_APPS_FILE"

    # Проверяем каждый процесс и останавливаем только существующие
    local result=0
    for pid_info in "${pids[@]}"; do
        IFS=: read -r pid name timestamp <<<"$pid_info"

        if [[ -n "$pid" ]] && ps -p "$pid" >/dev/null 2>&1; then
            echo -e "${YELLOW}Останавливаем процесс ${name} с PID ${pid}...${NC}"
            kill "$pid" >/dev/null 2>&1 || true

            # Ожидаем завершения процесса
            local counter=0
            while ps -p "$pid" >/dev/null 2>&1 && [[ $counter -lt 5 ]]; do
                sleep 1
                counter=$((counter + 1))
            done

            # Если процесс все еще жив, используем SIGKILL
            if ps -p "$pid" >/dev/null 2>&1; then
                echo -e "${RED}Приложение ${name} не завершилось, принудительно завершаем...${NC}"
                kill -9 "$pid" >/dev/null 2>&1 || true
                sleep 1
            fi

            if ! ps -p "$pid" >/dev/null 2>&1; then
                echo -e "${GREEN}Приложение ${name} остановлено.${NC}"
            else
                echo -e "${RED}Не удалось остановить приложение ${name} с PID ${pid}${NC}"
                # Если процесс всё еще запущен, добавляем его обратно в файл
                echo "$pid:$name:$timestamp" >>"$RUNNING_APPS_FILE"
                result=1
            fi
        else
            echo -e "${YELLOW}Процесс ${name} с PID ${pid} уже не существует${NC}"
        fi
    done

    # Финальная проверка порта 8080
    port_pid=$(get_port_pid)
    port_status=$?

    if [[ $port_status -eq 0 && -n "$port_pid" ]]; then
        echo -e "${RED}ВНИМАНИЕ: После остановки всех приложений, порт 8080 всё еще занят процессом ${port_pid}${NC}"
        echo -e "${YELLOW}Принудительно останавливаем процесс...${NC}"
        kill -9 "$port_pid" >/dev/null 2>&1 || true
        sleep 1

        # Повторная проверка
        local check_port=$(get_port_pid)
        port_status=$?

        if [[ $port_status -eq 0 && -n "$check_port" ]]; then
            echo -e "${RED}Не удалось освободить порт 8080!${NC}"
            result=1
        else
            echo -e "${GREEN}Порт 8080 успешно освобожден${NC}"
        fi
    fi

    return $result
}

# Функция для проверки работоспособности приложения по PID
check_app() {
    local pid=$1

    if [[ -z "$pid" ]]; then
        error_exit "Не указан PID"
    fi

    if ! ps -p "$pid" >/dev/null 2>&1; then
        echo -e "${RED}Процесс с PID $pid не существует${NC}"
        return 1
    fi

    # Проверяем, слушает ли процесс порт 8080
    local port_pid=$(get_port_pid)
    local port_status=$?

    if [[ $port_status -eq 0 && -n "$port_pid" && "$port_pid" = "$pid" ]]; then
        echo -e "${GREEN}Процесс с PID $pid слушает порт 8080${NC}"
    else
        echo -e "${YELLOW}Процесс с PID $pid не слушает порт 8080${NC}"
    fi

    # Проверяем доступность по HTTP
    local http_status
    http_status=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/ 2>/dev/null || echo "failed")

    if [[ "$http_status" == "200" ]]; then
        echo -e "${GREEN}Приложение с PID $pid работает и отвечает на HTTP запросы${NC}"
        return 0
    elif [[ "$http_status" != "failed" ]]; then
        echo -e "${YELLOW}Приложение с PID $pid отвечает с кодом $http_status${NC}"
        return 0
    else
        echo -e "${RED}Приложение с PID $pid не отвечает на HTTP запросы${NC}"
        return 1
    fi
}

# Функция для вывода списка запущенных приложений
list_running_apps() {
    echo -e "${BLUE}Запущенные приложения:${NC}"
    local found=false

    # Проверяем, есть ли процесс на порту 8080
    local port_pid=$(get_port_pid)
    local port_status=$?
    local port_occupied=false
    local port_pid_info=""

    if [[ $port_status -eq 0 && -n "$port_pid" ]]; then
        port_occupied=true
        echo -e "${YELLOW}Порт 8080 занят процессом с PID: ${port_pid}${NC}"
        found=true
    fi

    if [[ ! -f "$RUNNING_APPS_FILE" || ! -s "$RUNNING_APPS_FILE" ]]; then
        if [[ "$port_occupied" = false ]]; then
            echo -e "${YELLOW}Нет запущенных приложений${NC}"
            return 0
        fi
    fi

    echo -e "PID\tИмя\t\tВремя запуска\t\tЗапущено\t\tПорт 8080"
    echo -e "---\t---\t\t------------\t\t--------\t\t--------"

    # Если есть процесс на порту 8080, но его нет в списке, добавляем отметку
    if [[ "$port_occupied" = true ]]; then
        port_pid_info="ДА"
    fi

    # Проверяем записи из файла
    if [[ -f "$RUNNING_APPS_FILE" ]]; then
        while IFS=: read -r pid name timestamp; do
            if [[ -n "$pid" ]] && ps -p "$pid" >/dev/null 2>&1; then
                start_date=$(date -d @$timestamp '+%Y-%m-%d %H:%M:%S' 2>/dev/null || echo "Unknown")
                running_seconds=$(($(date +%s) - timestamp))
                running_time=$(printf '%02d:%02d:%02d' $((running_seconds / 3600)) $((running_seconds % 3600 / 60)) $((running_seconds % 60)))

                # Проверяем, совпадает ли PID с процессом на порту 8080
                local port_status="нет"
                if [[ "$port_occupied" = true && "$pid" = "$port_pid" ]]; then
                    port_status="${GREEN}ДА${NC}"
                    port_pid_info="найден в списке ($name)"
                fi

                echo -e "${GREEN}$pid\t$name\t$start_date\t$running_time\t\t$port_status${NC}"

                # Вывести текущее использование CPU и памяти
                if command -v ps >/dev/null 2>&1; then
                    cpu=$(ps -p "$pid" -o %cpu= 2>/dev/null | tr -d ' ' || echo "N/A")
                    mem=$(ps -p "$pid" -o %mem= 2>/dev/null | tr -d ' ' || echo "N/A")
                    rss=$(ps -p "$pid" -o rss= 2>/dev/null | tr -d ' ' || echo "0")
                    rss_mb=$(echo "scale=2; $rss/1024" | bc 2>/dev/null || echo "N/A")

                    echo -e "   CPU: ${YELLOW}${cpu}%${NC} | Память: ${YELLOW}${mem}%${NC} (${rss_mb} МБ)"
                fi

                found=true
            else
                # Удаляем информацию о несуществующих процессах
                grep -v "^$pid:" "$RUNNING_APPS_FILE" >"${RUNNING_APPS_FILE}.tmp" || true
                mv "${RUNNING_APPS_FILE}.tmp" "$RUNNING_APPS_FILE"
                echo -e "${RED}$pid\t$name\t$start_date\t(процесс завершен)${NC}"
            fi
        done <"$RUNNING_APPS_FILE" || true
    fi

    # Если процесс на порту 8080 не найден в списке, но существует
    if [[ "$port_occupied" = true && "$port_pid_info" = "ДА" ]]; then
        echo -e "${YELLOW}ВНИМАНИЕ: Процесс с PID ${port_pid} слушает порт 8080, но не найден в списке запущенных приложений!${NC}"
        echo -e "${YELLOW}Возможно, это приложение было запущено не через app-manager.sh.${NC}"

        # Вывести информацию о процессе
        if command -v ps >/dev/null 2>&1; then
            cmd=$(ps -p $port_pid -o command= 2>/dev/null || echo "Нет информации")
            user=$(ps -p $port_pid -o user= 2>/dev/null || echo "Нет информации")
            started=$(ps -p $port_pid -o lstart= 2>/dev/null || echo "Нет информации")

            echo -e "${YELLOW}Информация о процессе:${NC}"
            echo -e "   PID: ${RED}${port_pid}${NC}"
            echo -e "   Пользователь: ${RED}${user}${NC}"
            echo -e "   Команда: ${RED}${cmd}${NC}"
            echo -e "   Запущен: ${RED}${started}${NC}"

            cpu=$(ps -p "$port_pid" -o %cpu= 2>/dev/null | tr -d ' ' || echo "N/A")
            mem=$(ps -p "$port_pid" -o %mem= 2>/dev/null | tr -d ' ' || echo "N/A")
            rss=$(ps -p "$port_pid" -o rss= 2>/dev/null | tr -d ' ' || echo "0")
            rss_mb=$(echo "scale=2; $rss/1024" | bc 2>/dev/null || echo "N/A")

            echo -e "   CPU: ${RED}${cpu}%${NC} | Память: ${RED}${mem}%${NC} (${rss_mb} МБ)"
        fi
    fi

    if [[ "$found" = false ]]; then
        echo -e "${YELLOW}Нет запущенных приложений${NC}"
    fi
}

# Функция для вывода списка доступных приложений для запуска
list_available_apps() {
    echo -e "${BLUE}Доступные приложения для тестирования:${NC}"
    local has_apps=false

    for app in "${!APPS[@]}"; do
        echo -e "${GREEN}${app}${NC} - ${APPS[$app]}"
        has_apps=true
    done

    if [[ "$has_apps" = false ]]; then
        echo -e "${YELLOW}Нет доступных приложений в конфигурации.${NC}"
        echo -e "${YELLOW}Добавьте приложения в файл apps.sh${NC}"
        return 1
    fi

    return 0
}

# Функция вывода справки
show_help() {
    echo -e "${BLUE}Менеджер приложений для тестирования производительности${NC}"
    echo -e "Использование: $0 [команда] [параметры]"
    echo -e ""
    echo -e "Команды:"
    echo -e "  ${GREEN}start [app_name]${NC} - Запустить приложение (возвращает JSON с PID)"
    echo -e "  ${GREEN}start -s [app_name]${NC} - Запустить приложение в интерактивном режиме с выводом логов (Ctrl+C для остановки)"
    echo -e "  ${GREEN}stop [pid|app_name]${NC} - Остановить приложение по PID или имени"
    echo -e "  ${GREEN}stopall${NC} - Остановить все запущенные приложения"
    echo -e "  ${GREEN}check [pid]${NC} - Проверить работоспособность приложения"
    echo -e "  ${GREEN}ps${NC} - Показать список запущенных приложений с их статусом"
    echo -e "  ${GREEN}list${NC} - Показать список доступных приложений для запуска"
    echo -e "  ${GREEN}help${NC} - Показать эту справку"
    echo -e ""
    echo -e "Примеры:"
    echo -e "  $0 start java-vthreads   # Запустить Java приложение с виртуальными потоками"
    echo -e "  $0 start -s go-fasthttp  # Запустить Go приложение в интерактивном режиме"
    echo -e "  $0 stop 1234             # Остановить приложение с PID 1234"
    echo -e "  $0 stop java-vthreads    # Остановить все запущенные приложения типа java-vthreads"
    echo -e "  $0 ps                    # Показать все запущенные приложения"
}

# Обработка сигналов для корректного завершения
trap 'echo -e "\n${RED}Получен сигнал завершения, выход...${NC}"; exit 1' SIGTERM SIGINT

# Обработка аргументов командной строки
main() {
    if [[ $# -eq 0 ]]; then
        show_help
        exit 0
    fi

    case "$1" in
    start)
        interactive_mode=false
        app_name=""

        # Обработка параметров запуска
        shift
        while [[ $# -gt 0 ]]; do
            case "$1" in
            -s)
                interactive_mode=true
                shift
                ;;
            *)
                app_name="$1"
                shift
                ;;
            esac
        done

        if [[ -z "$app_name" ]]; then
            error_exit "Не указано имя приложения. Используйте: $0 start [-s] [app_name]"
        fi

        # Установка обработчика сигнала для интерактивного режима
        if [[ "$interactive_mode" = true ]]; then
            trap handle_sigint SIGINT
        fi

        start_app "$app_name" "$interactive_mode"
        exit $?
        ;;
    stop)
        if [[ $# -lt 2 ]]; then
            error_exit "Не указан PID или имя приложения. Используйте: $0 stop [pid|app_name]"
        fi
        # Проверяем, является ли аргумент числом (PID)
        if [[ "$2" =~ ^[0-9]+$ ]]; then
            stop_app_by_pid "$2"
        else
            stop_app_by_name "$2"
        fi
        exit $?
        ;;
    stopall)
        stop_all_apps
        exit $?
        ;;
    check)
        if [[ $# -lt 2 ]]; then
            error_exit "Не указан PID. Используйте: $0 check [pid]"
        fi
        check_app "$2"
        exit $?
        ;;
    ps)
        list_running_apps
        exit 0
        ;;
    list)
        list_available_apps
        exit $?
        ;;
    help | --help | -h)
        show_help
        exit 0
        ;;
    *)
        error_exit "Неизвестная команда: $1"
        ;;
    esac

    exit 0
}

# Запускаем основную функцию
main "$@"

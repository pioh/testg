// Глобальные переменные для хранения графиков и данных
let baseRpsChart = null,
  maxDepthChart = null,
  rpsVsLatencyChart = null,
  resourceUsageChart = null;
let heatmapChart = null;
let resultsTable;
let columnVisibility = {};
let lastData = null;

// Цвета для различных типов приложений
const appColors = {
  java: {
    base: "#5382a1",
    light: "#8caac1",
    transparent: "rgba(83, 130, 161, 0.2)",
  },
  go: {
    base: "#00add8",
    light: "#66c9e6",
    transparent: "rgba(0, 173, 216, 0.2)",
  },
  graalvm: {
    base: "#e76f00",
    light: "#f0a966",
    transparent: "rgba(231, 111, 0, 0.2)",
  },
};

// Типы приложений и их группировка
const appTypes = {
  "java-virtualThreads": "java",
  "java-fixedThreads24": "java",
  "java-fixedThreads1024": "java",
  "java-fixedThreads24AsyncNIO": "java",
  "graalvm-native-virtualThreads": "graalvm",
  "graalvm-native-fixedThreads24": "graalvm",
  "graalvm-native-fixedThreads1024": "graalvm",
  "graalvm-native-fixedThreads24AsyncNIO": "graalvm",
  "go-fasthttp": "go",
  "go-stdlib": "go",
  "go-fasthttp-maxprocs1": "go",
  "go-stdlib-maxprocs1": "go",
};

// Инициализация при загрузке страницы
document.addEventListener("DOMContentLoaded", function () {
  // Загрузка данных при старте
  fetchData();

  // Обработчик кнопки обновления
  document.getElementById("refreshBtn").addEventListener("click", fetchData);

  // Обработчик переключения логарифмической шкалы RPS
  document
    .getElementById("log-scale-rps")
    .addEventListener("change", function () {
      if (lastData) {
        updateBaseRpsChart(lastData.appGroups, lastData.appNames);
      }
    });

  // Обработчик переключения группировки по типу
  document
    .getElementById("group-by-type")
    .addEventListener("change", function () {
      if (lastData) {
        displayKeyMetrics(lastData);
      }
    });

  // Обработчики для фильтров максимальной глубины
  document.querySelectorAll("[data-type]").forEach((button) => {
    button.addEventListener("click", function () {
      document.querySelectorAll("[data-type]").forEach((btn) => {
        btn.classList.remove("active");
      });
      this.classList.add("active");
      if (lastData) {
        updateMaxDepthChart(lastData, this.dataset.type);
      }
    });
  });

  // Обработчик изменения параллельности для графика RPS vs Latency
  document
    .getElementById("parallel-selector")
    .addEventListener("change", function () {
      if (lastData) {
        updateRpsVsLatencyChart(lastData, parseInt(this.value));
      }
    });

  // Обработчик изменения глубины для графика ресурсов
  document
    .getElementById("resource-depth-selector")
    .addEventListener("change", function () {
      if (lastData) {
        updateResourceUsageChart(lastData, parseInt(this.value));
      }
    });

  // Обработчик изменения метрики тепловой карты
  document
    .getElementById("heatmap-metric")
    .addEventListener("change", function () {
      if (lastData) {
        updateHeatmap(lastData);
      }
    });

  // Обработчик изменения типа приложения тепловой карты
  document
    .getElementById("heatmap-app-type")
    .addEventListener("change", function () {
      if (lastData) {
        updateHeatmap(lastData);
      }
    });

  // Настройка модального окна для колонок таблицы
  document
    .getElementById("toggle-columns-btn")
    .addEventListener("click", function () {
      populateColumnToggles();
      new bootstrap.Modal(document.getElementById("columnsModal")).show();
    });

  document
    .getElementById("apply-columns")
    .addEventListener("click", function () {
      applyColumnVisibility();
      bootstrap.Modal.getInstance(
        document.getElementById("columnsModal")
      ).hide();
    });
});

// Основная функция загрузки данных
async function fetchData() {
  try {
    document.getElementById("refreshBtn").disabled = true;
    document.getElementById("refreshBtn").innerHTML =
      '<i class="bi bi-arrow-clockwise"></i> Загрузка...';

    const response = await fetch("/api/data");
    if (!response.ok) {
      throw new Error("Не удалось получить данные");
    }

    const responseData = await response.json();

    if (responseData.error) {
      console.error("Ошибка получения данных:", responseData.error);
      return;
    }

    // Обновляем время последнего обновления
    updateLastUpdateTime();

    // Обрабатываем и отображаем данные
    processData(responseData);

    document.getElementById("refreshBtn").disabled = false;
    document.getElementById("refreshBtn").innerHTML =
      '<i class="bi bi-arrow-clockwise"></i> Обновить';
  } catch (error) {
    console.error("Ошибка при загрузке данных:", error);
    document.getElementById("refreshBtn").disabled = false;
    document.getElementById("refreshBtn").innerHTML =
      '<i class="bi bi-arrow-clockwise"></i> Обновить';
  }
}

// Функция обработки данных
function processData(responseData) {
  const { data, runDir } = responseData;

  // Проверяем, есть ли данные
  if (!data || data.length === 0) {
    console.error("Нет данных для отображения");
    return;
  }

  try {
    // Подготавливаем данные для графиков и анализа
    const preparedData = prepareDataForAnalysis(data);

    // Сохраняем данные для использования в обработчиках событий
    lastData = preparedData;

    // Удаляем блок ключевых метрик полностью (скрываем его)
    document.querySelector(".row:has(#key-metrics-container)").style.display =
      "none";

    // Находим и отображаем аномалии (не более 5)
    const anomalies = findAnomalies(preparedData);
    displayAnomalies(anomalies.slice(0, 5));

    // Обновляем графики
    updateCharts(preparedData);

    // Обновляем тепловую карту
    updateHeatmap(preparedData);

    // Обновляем таблицу
    updateTable(data);

    // Отображаем путь к последней папке со статистикой
    console.log("Данные загружены из:", runDir);
  } catch (error) {
    console.error("Ошибка при обработке данных:", error);
  }
}

// Функция форматирования времени последнего обновления
function updateLastUpdateTime() {
  const now = new Date();
  const options = {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    day: "numeric",
    month: "short",
    year: "numeric",
  };

  const formattedTime = now.toLocaleString("ru-RU", options);
  document.querySelector(".last-updated").textContent =
    "Обновлено: " + formattedTime;
}

// Функция подготовки данных для анализа
function prepareDataForAnalysis(rawData) {
  // Нормализуем данные для анализа
  const normalizedData = rawData.map((row) => {
    return {
      ...row,
      Приложение: row.Приложение,
      AppType: appTypes[row.Приложение] || "unknown",
      Параллельность: parseInt(row.Параллельность),
      Глубина: parseInt(row.Глубина),
      RPS: parseFloat(row.RPS),
      Среднее_время_мс: parseFloat(row.Среднее_время_мс),
      Макс_время_мс: parseFloat(row.Макс_время_мс),
      Мин_время_мс: parseFloat(row.Мин_время_мс),
      Медиана_мс: parseFloat(row.Медиана_мс),
      P90_мс: parseFloat(row.P90_мс),
      P95_мс: parseFloat(row.P95_мс),
      P99_мс: parseFloat(row.P99_мс),
      Первый_запрос_мс: parseFloat(row.Первый_запрос_мс),
      Первая_пачка_мс: parseFloat(row.Первая_пачка_мс),
      Среднее_CPU: parseFloat(row.Среднее_CPU),
      Макс_CPU: parseFloat(row.Макс_CPU),
      Средняя_память_МБ: parseFloat(row.Средняя_память_МБ),
      Макс_память_МБ: parseFloat(row.Макс_память_МБ),
      Запросы_всего: parseInt(row.Запросы_всего),
      Запросы_успешные: parseInt(row.Запросы_успешные),
      Запросы_неудачные: parseInt(row.Запросы_неудачные),
      Запросы_несовпадения: parseInt(row.Запросы_несовпадения),
      Время_запуска_с: parseFloat(row.Время_запуска_с),
    };
  });

  // Получаем список приложений
  const appNames = [...new Set(normalizedData.map((row) => row.Приложение))];

  // Группировка данных по приложениям
  const appGroups = {};
  const appTypeGroups = {
    java: [],
    go: [],
    graalvm: [],
  };

  appNames.forEach((app) => {
    appGroups[app] = {
      all: normalizedData.filter((row) => row.Приложение === app),
      byParallelism: {},
      byDepth: {},
    };

    const appType = appTypes[app] || "unknown";
    appTypeGroups[appType].push(app);
  });

  // Заполняем группировки по параллельности и глубине
  normalizedData.forEach((row) => {
    const app = row.Приложение;
    const parallelism = row.Параллельность;
    const depth = row.Глубина;

    if (!appGroups[app].byParallelism[parallelism]) {
      appGroups[app].byParallelism[parallelism] = [];
    }

    if (!appGroups[app].byDepth[depth]) {
      appGroups[app].byDepth[depth] = [];
    }

    appGroups[app].byParallelism[parallelism].push(row);
    appGroups[app].byDepth[depth].push(row);
  });

  // Уникальные значения параллельности и глубины (для фильтров)
  const parallelValues = [
    ...new Set(normalizedData.map((row) => row.Параллельность)),
  ].sort((a, b) => a - b);
  const depthValues = [
    ...new Set(normalizedData.map((row) => row.Глубина)),
  ].sort((a, b) => a - b);

  // Базовые метрики для каждого приложения
  const appMetrics = {};
  appNames.forEach((app) => {
    const appData = appGroups[app].all;
    const baseline = appData.find(
      (row) => row.Параллельность === 32 && row.Глубина === 1
    );
    const maxRps = appData.reduce(
      (max, row) => (row.RPS > max ? row.RPS : max),
      0
    );
    const maxDepth = {};

    parallelValues.forEach((parallel) => {
      const rows = appData
        .filter((row) => row.Параллельность === parallel)
        .sort((a, b) => b.Глубина - a.Глубина);

      if (rows.length > 0) {
        const successfulRows = rows.filter(
          (row) =>
            row.Запросы_неудачные === 0 &&
            row.Запросы_несовпадения === 0 &&
            row.Запросы_успешные === row.Запросы_всего
        );

        maxDepth[parallel] =
          successfulRows.length > 0
            ? successfulRows[successfulRows.length - 1].Глубина
            : 0;
      } else {
        maxDepth[parallel] = 0;
      }
    });

    appMetrics[app] = {
      baselineRps: baseline ? baseline.RPS : 0,
      maxRps: maxRps,
      maxDepth: maxDepth,
      meanCpu:
        appData.reduce((sum, row) => sum + row.Среднее_CPU, 0) / appData.length,
      meanMemory:
        appData.reduce((sum, row) => sum + row.Средняя_память_МБ, 0) /
        appData.length,
      startupTime:
        appData.reduce((sum, row) => sum + row.Время_запуска_с, 0) /
        appData.length,
    };
  });

  return {
    data: normalizedData,
    appNames,
    appGroups,
    appTypeGroups,
    parallelValues,
    depthValues,
    appMetrics,
  };
}

// Отображение графиков
function updateCharts(preparedData) {
  try {
    // Уничтожаем существующие графики, если они есть, перед созданием новых
    if (baseRpsChart instanceof Chart) {
      baseRpsChart.destroy();
      baseRpsChart = null;
    }

    if (maxDepthChart instanceof Chart) {
      maxDepthChart.destroy();
      maxDepthChart = null;
    }

    if (rpsVsLatencyChart instanceof Chart) {
      rpsVsLatencyChart.destroy();
      rpsVsLatencyChart = null;
    }

    if (resourceUsageChart instanceof Chart) {
      resourceUsageChart.destroy();
      resourceUsageChart = null;
    }

    // Обновляем все графики
    updateBaseRpsChart(preparedData);
    updateMaxDepthChart(preparedData, "java"); // По умолчанию показываем Java
    updateRpsVsLatencyChart(preparedData, 32); // Параллельность 32 по умолчанию
    updateResourceUsageChart(preparedData, 1); // Глубина 1 по умолчанию
  } catch (error) {
    console.error("Ошибка при обновлении графиков:", error);
  }
}

// Форматирование числа
function formatNumber(number, decimals = 0) {
  if (number === null || number === undefined || isNaN(number)) return "N/A";

  const absNumber = Math.abs(number);

  if (absNumber >= 1000000) {
    return (number / 1000000).toFixed(decimals) + "M";
  } else if (absNumber >= 1000) {
    return (number / 1000).toFixed(decimals) + "K";
  } else {
    return number.toFixed(decimals);
  }
}

// Функция получения цвета для приложения
function getAppColor(app, opacity = 1) {
  const appType = appTypes[app] || "unknown";
  const colors = appColors[appType] || {
    base: "#777",
    light: "#aaa",
    transparent: "rgba(119, 119, 119, 0.2)",
  };

  if (opacity < 1) {
    return `rgba(${hexToRgb(colors.base).join(",")},${opacity})`;
  }

  return colors.base;
}

// Вспомогательная функция для конвертации HEX в RGB
function hexToRgb(hex) {
  const shorthandRegex = /^#?([a-f\d])([a-f\d])([a-f\d])$/i;
  hex = hex.replace(shorthandRegex, function (m, r, g, b) {
    return r + r + g + g + b + b;
  });

  const result = /^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i.exec(hex);
  return result
    ? [
        parseInt(result[1], 16),
        parseInt(result[2], 16),
        parseInt(result[3], 16),
      ]
    : [0, 0, 0];
}

// Определение типа приложения по имени
function getAppType(appName) {
  if (appName.startsWith("java-")) return "java";
  if (appName.startsWith("go-")) return "go";
  if (appName.startsWith("graalvm-")) return "graalvm";
  return "unknown";
}

// Отображение ключевых метрик
function displayKeyMetrics(data) {
  const container = document.getElementById("key-metrics-container");
  container.innerHTML = "";

  const { appNames, appMetrics, appTypeGroups } = data;
  const groupByType = document.getElementById("group-by-type").checked;

  if (groupByType) {
    // Группировка по типу приложения
    const appTypes = Object.keys(appTypeGroups);

    for (const type of appTypes) {
      if (appTypeGroups[type].length === 0) continue;

      const typeApps = appTypeGroups[type];
      const typeHeader = document.createElement("div");
      typeHeader.className = "col-12 app-type-header app-type-" + type;
      typeHeader.textContent =
        type === "java" ? "Java" : type === "go" ? "Go" : "GraalVM";
      container.appendChild(typeHeader);

      const row = document.createElement("div");
      row.className = "row mb-3";

      // Добавляем метрики для каждого приложения
      for (const app of typeApps) {
        createMetricCard(app, appMetrics[app], row);
      }

      container.appendChild(row);
    }
  } else {
    // Без группировки - все приложения в одной строке
    const row = document.createElement("div");
    row.className = "row";

    for (const app of appNames) {
      createMetricCard(app, appMetrics[app], row);
    }

    container.appendChild(row);
  }
}

// Создание карточки метрики для приложения
function createMetricCard(app, metrics, container) {
  const appType = appTypes[app] || "unknown";
  const colDiv = document.createElement("div");
  colDiv.className = "col-md-4 col-lg-3 mb-3";

  const maxRpsValue = formatNumber(metrics.maxRps, 0);
  const avgMemory = formatNumber(metrics.meanMemory, 0);
  const startupTime = metrics.startupTime.toFixed(2);

  const cardHtml = `
    <div class="metric-card app-${appType}">
      <h5 class="metric-title" title="${app}">${app}</h5>
      <div class="row">
        <div class="col-6">
          <div class="mb-3">
            <div class="metric-title">Макс. RPS</div>
            <div class="metric-value">${maxRpsValue}</div>
          </div>
        </div>
        <div class="col-6">
          <div class="mb-3">
            <div class="metric-title">Память (МБ)</div>
            <div class="metric-value">${avgMemory}</div>
          </div>
        </div>
      </div>
      <div class="metric-info">
        <strong>Время запуска:</strong> ${startupTime}с
      </div>
    </div>
  `;

  colDiv.innerHTML = cardHtml;
  container.appendChild(colDiv);
}

// Обновление графика базового RPS (глубина 1)
function updateBaseRpsChart(data) {
  if (!data || !data.appNames || !Array.isArray(data.appNames)) {
    console.error("Неверные данные для графика RPS");
    return;
  }

  try {
    const ctx = document.getElementById("baseRpsChart").getContext("2d");
    const { appNames, appGroups } = data;

    // Подготовка данных для графика RPS
    const appLabels = [];
    const rpsValues = [];

    for (const app of appNames) {
      const depth1Data = appGroups[app]?.byDepth[1] || [];

      if (depth1Data.length > 0) {
        const maxRpsRow = depth1Data.reduce(
          (max, row) => (row.RPS > max.RPS ? row : max),
          depth1Data[0]
        );

        appLabels.push(app);
        rpsValues.push(maxRpsRow.RPS);
      }
    }

    // Сортируем данные по убыванию RPS
    const sortedIndices = rpsValues
      .map((_, i) => i)
      .sort((a, b) => rpsValues[b] - rpsValues[a]);

    const sortedLabels = sortedIndices.map((i) => appLabels[i]);
    const sortedValues = sortedIndices.map((i) => rpsValues[i]);
    const sortedColors = sortedIndices.map((i) => getAppColor(appLabels[i]));

    const isLogScale = document.getElementById("log-scale-rps").checked;

    if (baseRpsChart instanceof Chart) {
      baseRpsChart.destroy();
    }

    baseRpsChart = new Chart(ctx, {
      type: "bar",
      data: {
        labels: sortedLabels,
        datasets: [
          {
            label: "Максимальный RPS (глубина 1)",
            data: sortedValues,
            backgroundColor: sortedColors,
            borderColor: sortedColors,
            borderWidth: 1,
          },
        ],
      },
      options: {
        indexAxis: "y",
        responsive: true,
        maintainAspectRatio: false,
        plugins: {
          legend: {
            display: false,
          },
          tooltip: {
            callbacks: {
              label: function (context) {
                return `RPS: ${formatNumber(context.raw, 0)}`;
              },
            },
          },
        },
        scales: {
          x: {
            type: isLogScale ? "logarithmic" : "linear",
            beginAtZero: true,
            title: {
              display: true,
              text: "Запросов в секунду",
            },
          },
          y: {
            title: {
              display: false,
            },
          },
        },
      },
    });
  } catch (error) {
    console.error("Ошибка при создании графика RPS:", error);
  }
}

// Обновление графика максимальной глубины при разной параллельности
function updateMaxDepthChart(data, appTypeFilter) {
  if (!data || !data.appNames) return;

  try {
    const ctx = document.getElementById("maxDepthChart").getContext("2d");
    const { appNames, appMetrics, parallelValues } = data;

    // Фильтрация приложений по типу
    const filteredApps = appNames.filter(
      (app) => appTypeFilter === "all" || appTypes[app] === appTypeFilter
    );

    // Подготовка данных для графика
    const datasets = [];

    for (const app of filteredApps) {
      const depthData = [];

      for (const parallel of parallelValues) {
        const maxDepth = appMetrics[app]?.maxDepth[parallel] || 0;
        depthData.push(maxDepth);
      }

      datasets.push({
        label: app,
        data: depthData,
        borderColor: getAppColor(app),
        backgroundColor: getAppColor(app, 0.1),
        borderWidth: 2,
        tension: 0.3,
        pointRadius: 4,
        pointHoverRadius: 6,
      });
    }

    if (maxDepthChart instanceof Chart) {
      maxDepthChart.destroy();
    }

    maxDepthChart = new Chart(ctx, {
      type: "line",
      data: {
        labels: parallelValues,
        datasets: datasets,
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        plugins: {
          legend: {
            position: "bottom",
            labels: {
              boxWidth: 12,
              usePointStyle: true,
            },
          },
          tooltip: {
            callbacks: {
              title: function (context) {
                return `Параллельность: ${context[0].label}`;
              },
            },
          },
        },
        scales: {
          y: {
            beginAtZero: true,
            title: {
              display: true,
              text: "Максимальная глубина",
            },
          },
          x: {
            title: {
              display: true,
              text: "Параллельность",
            },
          },
        },
      },
    });
  } catch (error) {
    console.error("Ошибка при создании графика макс. глубины:", error);
  }
}

// Обновление графика RPS vs Latency
function updateRpsVsLatencyChart(data, parallelism) {
  if (!data || !data.appNames) return;

  try {
    const ctx = document.getElementById("rpsVsLatencyChart").getContext("2d");
    const { appNames, appGroups } = data;

    // Подготовка данных для графика
    const datasets = [];

    for (const app of appNames) {
      const parallelData = appGroups[app]?.byParallelism[parallelism] || [];

      if (parallelData.length > 0) {
        const chartData = parallelData.map((row) => ({
          x: row.RPS,
          y: row.P99_мс,
          depth: row.Глубина,
        }));

        // Сортировка по увеличению глубины для правильного отображения линии
        chartData.sort((a, b) => a.depth - b.depth);

        datasets.push({
          label: app,
          data: chartData,
          borderColor: getAppColor(app),
          backgroundColor: getAppColor(app, 0.1),
          borderWidth: 2,
          tension: 0.2,
          pointRadius: 4,
          pointHoverRadius: 6,
        });
      }
    }

    if (rpsVsLatencyChart instanceof Chart) {
      rpsVsLatencyChart.destroy();
    }

    rpsVsLatencyChart = new Chart(ctx, {
      type: "scatter",
      data: {
        datasets: datasets,
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        plugins: {
          legend: {
            position: "bottom",
            labels: {
              boxWidth: 12,
              usePointStyle: true,
            },
          },
          tooltip: {
            callbacks: {
              label: function (context) {
                const point = context.raw;
                return `${context.dataset.label}: RPS=${formatNumber(
                  point.x,
                  0
                )}, P99=${formatNumber(point.y, 0)}мс, Глубина=${point.depth}`;
              },
            },
          },
        },
        scales: {
          x: {
            type: "logarithmic",
            title: {
              display: true,
              text: "RPS (запросов в секунду)",
            },
          },
          y: {
            type: "logarithmic",
            title: {
              display: true,
              text: "P99 латентность (мс)",
            },
          },
        },
      },
    });
  } catch (error) {
    console.error("Ошибка при создании графика RPS vs Latency:", error);
  }
}

// Обновление графика использования ресурсов
function updateResourceUsageChart(data, depth) {
  if (!data || !data.appNames) return;

  try {
    const ctx = document.getElementById("resourceUsageChart").getContext("2d");
    const { appNames, appGroups } = data;

    // Подготовка данных для графика
    const chartData = [];

    for (const app of appNames) {
      const depthData = appGroups[app]?.byDepth[depth] || [];

      if (depthData.length > 0) {
        // Берем данные с наименьшей параллельностью для сравнения базового потребления
        const baseRow = depthData.reduce(
          (min, row) => (row.Параллельность < min.Параллельность ? row : min),
          depthData[0]
        );

        chartData.push({
          app: app,
          cpu: baseRow.Среднее_CPU,
          memory: baseRow.Средняя_память_МБ,
        });
      }
    }

    // Сортировка по потреблению памяти
    chartData.sort((a, b) => b.memory - a.memory);

    const labels = chartData.map((item) => item.app);
    const cpuData = chartData.map((item) => item.cpu / 10); // Нормализация CPU (деление на 10)
    const memoryData = chartData.map((item) => item.memory);
    const colors = chartData.map((item) => getAppColor(item.app));

    if (resourceUsageChart instanceof Chart) {
      resourceUsageChart.destroy();
    }

    resourceUsageChart = new Chart(ctx, {
      type: "bar",
      data: {
        labels: labels,
        datasets: [
          {
            label: "Память (МБ)",
            data: memoryData,
            backgroundColor: colors.map((color) => `${color}80`),
            borderColor: colors,
            borderWidth: 1,
            order: 2,
          },
          {
            label: "CPU (%)",
            data: cpuData,
            backgroundColor: colors.map((color) => `${color}40`),
            borderColor: colors,
            borderWidth: 1,
            order: 1,
          },
        ],
      },
      options: {
        indexAxis: "y",
        responsive: true,
        maintainAspectRatio: false,
        plugins: {
          legend: {
            position: "bottom",
          },
          tooltip: {
            callbacks: {
              label: function (context) {
                if (context.dataset.label === "CPU (%)") {
                  return `CPU: ${(context.raw * 10).toFixed(0)}%`;
                } else {
                  return `Память: ${context.raw.toFixed(0)} МБ`;
                }
              },
            },
          },
        },
        scales: {
          x: {
            beginAtZero: true,
            title: {
              display: true,
              text: "Значение (МБ для памяти, % x 10 для CPU)",
            },
          },
          y: {
            title: {
              display: false,
            },
          },
        },
      },
    });
  } catch (error) {
    console.error("Ошибка при создании графика ресурсов:", error);
  }
}

// Функция поиска аномалий в данных
function findAnomalies(data) {
  const { data: rawData, appNames, appGroups, appMetrics } = data;
  const anomalies = [];

  // 1. Высокий процент ошибок
  const errorAnomalies = rawData.filter((row) => row.Запросы_неудачные > 0);
  if (errorAnomalies.length > 0) {
    const highErrorApps = {};

    errorAnomalies.forEach((row) => {
      const errorRate = row.Запросы_неудачные / row.Запросы_всего;
      if (errorRate > 0.1) {
        // Более 10% ошибок
        if (!highErrorApps[row.Приложение]) {
          highErrorApps[row.Приложение] = [];
        }
        highErrorApps[row.Приложение].push({
          parallel: row.Параллельность,
          depth: row.Глубина,
          errorRate: errorRate,
        });
      }
    });

    for (const app in highErrorApps) {
      anomalies.push({
        type: "severe",
        title: `Высокий процент ошибок в ${app}`,
        description: `Обнаружен высокий процент ошибок в приложении ${app} при следующих конфигурациях:`,
        details: highErrorApps[app]
          .map(
            (item) =>
              `Параллельность=${item.parallel}, Глубина=${
                item.depth
              }, Ошибки=${(item.errorRate * 100).toFixed(1)}%`
          )
          .join("; "),
      });
    }
  }

  // 2. Необычно высокая латентность
  const highLatencyThreshold = 5000; // 5 секунд
  const latencyAnomalies = rawData.filter(
    (row) =>
      row.P99_мс > highLatencyThreshold &&
      row.Глубина <= 16 &&
      row.Запросы_неудачные === 0
  );

  if (latencyAnomalies.length > 0) {
    const groupedByApp = {};

    latencyAnomalies.forEach((row) => {
      if (!groupedByApp[row.Приложение]) {
        groupedByApp[row.Приложение] = [];
      }
      groupedByApp[row.Приложение].push({
        parallel: row.Параллельность,
        depth: row.Глубина,
        latency: row.P99_мс,
      });
    });

    for (const app in groupedByApp) {
      anomalies.push({
        type: "warning",
        title: `Высокая латентность P99 в ${app}`,
        description: `Необычно высокая латентность P99 (>${highLatencyThreshold}мс) при низкой глубине:`,
        details: groupedByApp[app]
          .map(
            (item) =>
              `Параллельность=${item.parallel}, Глубина=${
                item.depth
              }, P99=${item.latency.toFixed(0)}мс`
          )
          .join("; "),
      });
    }
  }

  // 3. Резкое падение RPS при увеличении параллельности
  for (const app of appNames) {
    const depth1Data = (appGroups[app].byDepth[1] || []).sort(
      (a, b) => a.Параллельность - b.Параллельность
    );

    if (depth1Data.length < 3) continue;

    for (let i = 1; i < depth1Data.length; i++) {
      const prev = depth1Data[i - 1];
      const curr = depth1Data[i];

      const rpsDropRate = (prev.RPS - curr.RPS) / prev.RPS;

      if (rpsDropRate > 0.4 && curr.Параллельность / prev.Параллельность <= 4) {
        anomalies.push({
          type: "warning",
          title: `Резкое падение RPS в ${app}`,
          description: `При глубине 1, увеличение параллельности с ${
            prev.Параллельность
          } до ${curr.Параллельность} привело к падению RPS на ${(
            rpsDropRate * 100
          ).toFixed(0)}%`,
          details: `С ${formatNumber(prev.RPS, 0)} до ${formatNumber(
            curr.RPS,
            0
          )} RPS`,
        });
        break;
      }
    }
  }

  // 4. Необычно высокое потребление памяти
  const memoryThreshold = 5000; // 5 ГБ
  const highMemoryRows = rawData.filter(
    (row) => row.Средняя_память_МБ > memoryThreshold && row.Глубина <= 16
  );

  if (highMemoryRows.length > 0) {
    const groupedByApp = {};

    highMemoryRows.forEach((row) => {
      if (!groupedByApp[row.Приложение]) {
        groupedByApp[row.Приложение] = [];
      }
      groupedByApp[row.Приложение].push({
        parallel: row.Параллельность,
        depth: row.Глубина,
        memory: row.Средняя_память_МБ,
      });
    });

    for (const app in groupedByApp) {
      anomalies.push({
        type: "warning",
        title: `Высокое потребление памяти в ${app}`,
        description: `Приложение использует более ${
          memoryThreshold / 1000
        }ГБ памяти при низкой глубине:`,
        details: groupedByApp[app]
          .map(
            (item) =>
              `Параллельность=${item.parallel}, Глубина=${
                item.depth
              }, Память=${(item.memory / 1000).toFixed(1)}ГБ`
          )
          .join("; "),
      });
    }
  }

  // 5. Выдающиеся результаты по эффективности
  const topRpsApp = appNames.reduce(
    (top, app) =>
      appMetrics[app].baselineRps > (appMetrics[top]?.baselineRps || 0)
        ? app
        : top,
    appNames[0]
  );

  const topRpsValue = appMetrics[topRpsApp].baselineRps;

  anomalies.push({
    type: "positive",
    title: `Лучший базовый RPS: ${topRpsApp}`,
    description: `Приложение ${topRpsApp} показало наилучшую базовую производительность в ${formatNumber(
      topRpsValue,
      0
    )} RPS при глубине 1`,
    details: `Это в ${(
      topRpsValue /
      (appMetrics[appNames.find((a) => a !== topRpsApp)]?.baselineRps || 1)
    ).toFixed(1)}x раз быстрее среднего значения`,
  });

  // 6. Неожиданно стабильная работа при высокой нагрузке
  const stableApps = appNames.filter((app) => {
    const highParallelRows = (appGroups[app].byParallelism[8192] || []).filter(
      (row) => row.Запросы_неудачные === 0 && row.Глубина >= 16
    );
    return highParallelRows.length > 0;
  });

  if (stableApps.length > 0) {
    anomalies.push({
      type: "interesting",
      title: `Стабильность при экстремальной нагрузке`,
      description: `${stableApps.join(
        ", "
      )} показали стабильную работу при параллельности 8192 и глубине ≥16`,
      details: `Это указывает на хорошую масштабируемость и стабильность под нагрузкой`,
    });
  }

  return anomalies;
}

// Отображение аномалий - ограничиваем количество
function displayAnomalies(anomalies) {
  const container = document.getElementById("anomalies-container");
  container.innerHTML = "";

  if (!anomalies || anomalies.length === 0) {
    container.innerHTML = '<p class="text-muted">Аномалий не обнаружено</p>';
    return;
  }

  // Ограничиваем количество аномалий до 5 для более компактного отображения
  const limitedAnomalies = anomalies.slice(0, 5);

  for (const anomaly of limitedAnomalies) {
    const card = document.createElement("div");
    card.className = `anomaly-card anomaly-${anomaly.type}`;

    card.innerHTML = `
      <div class="anomaly-title">${anomaly.title}</div>
      <div class="anomaly-description">${anomaly.description}</div>
      <div class="anomaly-details">${anomaly.details}</div>
    `;

    container.appendChild(card);
  }

  // Если есть больше аномалий, показываем счетчик
  if (anomalies.length > 5) {
    const moreInfo = document.createElement("div");
    moreInfo.className = "text-muted mt-2";
    moreInfo.textContent = `И еще ${anomalies.length - 5} других аномалий`;
    container.appendChild(moreInfo);
  }
}

// Обновление тепловой карты
function updateHeatmap(data) {
  if (!data || !data.appNames) return;

  try {
    const { appNames, appGroups } = data;
    const metric = document.getElementById("heatmap-metric").value;
    const appTypeFilter = document.getElementById("heatmap-app-type").value;

    // Фильтрация приложений по типу
    const filteredApps = appNames.filter(
      (app) => appTypeFilter === "all" || appTypes[app] === appTypeFilter
    );

    // Подготовка данных для тепловой карты
    const heatmapContainer = document.getElementById("heatmapContainer");
    heatmapContainer.innerHTML = "";

    if (heatmapChart) {
      // ApexCharts не имеет метода destroy()
      heatmapChart.destroy();
      heatmapChart = null;
    }

    // Получение уникальных значений параллельности и глубины
    const parallelValues = [
      ...new Set(data.data.map((row) => row.Параллельность)),
    ].sort((a, b) => a - b);
    const depthValues = [...new Set(data.data.map((row) => row.Глубина))].sort(
      (a, b) => a - b
    );

    // Создание данных для тепловой карты
    const series = [];

    for (const app of filteredApps) {
      const appData = appGroups[app]?.all || [];
      const heatmapData = [];

      for (const parallel of parallelValues) {
        for (const depth of depthValues) {
          const row = appData.find(
            (r) => r.Параллельность === parallel && r.Глубина === depth
          );

          if (row) {
            const value = row[metric];
            heatmapData.push({
              x: `P=${parallel}`,
              y: `D=${depth}`,
              value: value,
            });
          }
        }
      }

      series.push({
        name: app,
        data: heatmapData,
      });
    }

    // Определение подходящей шкалы цветов в зависимости от метрики
    let colorScale = ["#00A100", "#FFC300", "#FF5733", "#C70039"];

    if (metric === "RPS") {
      colorScale = ["#C70039", "#FF5733", "#FFC300", "#00A100"];
    } else if (metric === "Запросы_неудачные") {
      colorScale = ["#00A100", "#FF5733", "#C70039"];
    }

    // Создание тепловой карты
    heatmapChart = new ApexCharts(heatmapContainer, {
      series: series,
      chart: {
        height: 500,
        type: "heatmap",
        toolbar: {
          show: true,
        },
      },
      dataLabels: {
        enabled: false,
      },
      colors: colorScale,
      title: {
        text: `Тепловая карта: ${getMetricLabel(metric)}`,
        align: "left",
      },
      plotOptions: {
        heatmap: {
          enableShades: true,
          shadeIntensity: 0.5,
          radius: 0,
          useFillColorAsStroke: true,
          colorScale: {
            ranges: [
              {
                from: 0,
                to: getMaxValue(data.data, metric) / 3,
                color: colorScale[0],
                name: "Низкий",
              },
              {
                from: getMaxValue(data.data, metric) / 3,
                to: (getMaxValue(data.data, metric) * 2) / 3,
                color: colorScale[1],
                name: "Средний",
              },
              {
                from: (getMaxValue(data.data, metric) * 2) / 3,
                to: getMaxValue(data.data, metric),
                color: colorScale[2],
                name: "Высокий",
              },
            ],
          },
        },
      },
      legend: {
        position: "bottom",
      },
      tooltip: {
        y: {
          formatter: function (value) {
            return formatValue(value, metric);
          },
        },
      },
    });

    heatmapChart.render();
  } catch (error) {
    console.error("Ошибка при создании тепловой карты:", error);
    document.getElementById("heatmapContainer").innerHTML =
      '<div class="alert alert-danger">Ошибка при создании тепловой карты. Пожалуйста, обновите страницу.</div>';
  }
}

// Получение метки для метрики
function getMetricLabel(metric) {
  const labels = {
    RPS: "Запросов в секунду",
    Среднее_время_мс: "Среднее время отклика (мс)",
    P99_мс: "P99 время отклика (мс)",
    Среднее_CPU: "Использование CPU (%)",
    Средняя_память_МБ: "Использование памяти (МБ)",
    Запросы_неудачные: "Неудачные запросы",
  };

  return labels[metric] || metric;
}

// Форматирование значения для отображения
function formatValue(value, metric) {
  if (metric === "RPS") {
    return formatNumber(value, 0);
  } else if (metric === "Среднее_CPU") {
    return value.toFixed(0) + "%";
  } else if (metric === "Средняя_память_МБ") {
    return formatNumber(value, 0) + " МБ";
  } else if (metric.includes("_мс")) {
    return value.toFixed(0) + " мс";
  } else {
    return value.toString();
  }
}

// Получение максимального значения метрики в данных
function getMaxValue(data, metric) {
  return data.reduce((max, row) => Math.max(max, row[metric] || 0), 0);
}

// Функция обновления таблицы с данными
function updateTable(data) {
  try {
    // Заполняем селекторы для фильтров таблицы
    populateTableFilters(data);

    // Если таблица уже инициализирована, уничтожаем ее
    if (resultsTable) {
      resultsTable.destroy();
    }

    // Группировка данных по параллельности и глубине для сравнения внутри групп
    const groupedData = {};
    data.forEach((row) => {
      const key = `${row.Параллельность}-${row.Глубина}`;
      if (!groupedData[key]) {
        groupedData[key] = [];
      }
      groupedData[key].push({ ...row });
    });

    // Функция для определения мин/макс значений в группе
    function getMinMaxInGroup(groupKey, field) {
      if (!groupedData[groupKey]) return { min: 0, max: 0 };

      const values = groupedData[groupKey].map((row) => parseFloat(row[field]));
      return {
        min: Math.min(...values),
        max: Math.max(...values),
      };
    }

    // Форматируем данные для таблицы
    const formattedData = data.map((row) => {
      return {
        ...row,
        groupKey: `${row.Параллельность}-${row.Глубина}`,
        RPS: parseFloat(row.RPS).toFixed(2),
        Среднее_время_мс: parseFloat(row.Среднее_время_мс).toFixed(2),
        Мин_время_мс: parseFloat(row.Мин_время_мс).toFixed(2),
        Макс_время_мс: parseFloat(row.Макс_время_мс).toFixed(2),
        Медиана_мс: parseFloat(row.Медиана_мс).toFixed(2),
        P90_мс: parseFloat(row.P90_мс).toFixed(2),
        P95_мс: parseFloat(row.P95_мс).toFixed(2),
        P99_мс: parseFloat(row.P99_мс).toFixed(2),
        Среднее_CPU: parseFloat(row.Среднее_CPU).toFixed(2),
        Макс_CPU: parseFloat(row.Макс_CPU).toFixed(2),
        Средняя_память_МБ: parseFloat(row.Средняя_память_МБ).toFixed(2),
        Макс_память_МБ: parseFloat(row.Макс_память_МБ).toFixed(2),
        Время_запуска_с: parseFloat(row.Время_запуска_с).toFixed(2),
      };
    });

    // Колонки таблицы
    const columns = [
      { data: "Приложение", title: "Приложение" },
      { data: "Параллельность", title: "Паралл." },
      { data: "Глубина", title: "Глубина" },
      { data: "RPS", title: "RPS" },
      { data: "Среднее_время_мс", title: "Ср. время (мс)" },
      { data: "P99_мс", title: "P99 (мс)" },
      { data: "Запросы_всего", title: "Запр. всего" },
      { data: "Запросы_успешные", title: "Успеш." },
      { data: "Запросы_неудачные", title: "Неуд." },
      { data: "Среднее_CPU", title: "Ср. CPU" },
      { data: "Средняя_память_МБ", title: "Память (МБ)" },
      { data: "Время_запуска_с", title: "Старт (с)" },
    ];

    // Инициализация видимости колонок, если это первый вызов
    if (Object.keys(columnVisibility).length === 0) {
      columns.forEach((col) => {
        columnVisibility[col.data] = true;
      });
    }

    // Создаем CSS для ячеек с разными цветами в зависимости от значений
    const styleElement = document.createElement("style");
    styleElement.innerHTML = `
      .cell-best { background-color: rgba(25, 135, 84, 0.25) !important; font-weight: bold; }
      .cell-good { background-color: rgba(25, 135, 84, 0.15) !important; }
      .cell-worst { background-color: rgba(220, 53, 69, 0.25) !important; font-weight: bold; }
      .cell-bad { background-color: rgba(220, 53, 69, 0.15) !important; }
      .group-separator td { border-top: 2px solid #dee2e6 !important; }
      #resultsTable { font-size: 0.85rem; }
      #resultsTable th, #resultsTable td { padding: 0.4rem 0.5rem; }
    `;
    document.head.appendChild(styleElement);

    // Создаем таблицу
    resultsTable = new DataTable("#resultsTable", {
      data: formattedData,
      columns: columns,
      order: [
        [1, "asc"],
        [2, "asc"],
      ],
      paging: false, // Отключаем пагинацию
      scrollY: "600px", // Добавляем вертикальную прокрутку вместо пагинации
      scrollCollapse: true,
      scrollX: true,
      dom:
        "<'row'<'col-sm-12 col-md-6'l><'col-sm-12 col-md-6'f>>" +
        "<'row'<'col-sm-12'tr>>" +
        "<'row'<'col-sm-12 col-md-5'i><'col-sm-12 col-md-7'p>>",
      language: {
        // Встраиваем русский язык напрямую, избегая CORS-проблему
        search: "Поиск:",
        lengthMenu: "Показать _MENU_ записей",
        info: "Записи с _START_ до _END_ из _TOTAL_",
        infoEmpty: "Записи с 0 до 0 из 0",
        infoFiltered: "(отфильтровано из _MAX_ записей)",
        emptyTable: "Нет данных",
        zeroRecords: "Совпадений не найдено",
      },
      columnDefs: [
        {
          targets: "_all",
          className: "align-middle",
        },
      ],
      createdRow: function (row, data, index) {
        // Находим предыдущую запись, чтобы определить начало новой группы
        if (index > 0) {
          const prevRow = formattedData[index - 1];
          if (
            prevRow.Параллельность !== data.Параллельность ||
            prevRow.Глубина !== data.Глубина
          ) {
            $(row).addClass("group-separator");
          }
        }

        // Добавляем класс по типу приложения для стилизации
        const appType = getAppType(data.Приложение);
        $(row).addClass(`app-type-${appType}`);

        // Подсветка отдельных ячеек на основе данных в группе
        colorizeCell(
          $(row).find("td").eq(3),
          data.RPS,
          data.groupKey,
          "RPS",
          true
        ); // RPS (выше - лучше)
        colorizeCell(
          $(row).find("td").eq(4),
          data.Среднее_время_мс,
          data.groupKey,
          "Среднее_время_мс",
          false
        ); // Среднее время (ниже - лучше)
        colorizeCell(
          $(row).find("td").eq(5),
          data.P99_мс,
          data.groupKey,
          "P99_мс",
          false
        ); // P99 (ниже - лучше)
        colorizeCell(
          $(row).find("td").eq(9),
          data.Среднее_CPU,
          data.groupKey,
          "Среднее_CPU",
          false
        ); // CPU (ниже - лучше)
        colorizeCell(
          $(row).find("td").eq(10),
          data.Средняя_память_МБ,
          data.groupKey,
          "Средняя_память_МБ",
          false
        ); // Память (ниже - лучше)
        colorizeCell(
          $(row).find("td").eq(11),
          data.Время_запуска_с,
          data.groupKey,
          "Время_запуска_с",
          false
        ); // Время запуска (ниже - лучше)

        // Подсветка ошибок
        if (parseInt(data.Запросы_неудачные) > 0) {
          $(row).find("td").eq(8).addClass("cell-worst"); // Неудачные запросы
        }
      },
      drawCallback: function () {
        // Применяем текущую видимость колонок
        columns.forEach((col, index) => {
          if (!columnVisibility[col.data]) {
            resultsTable.column(index).visible(false);
          }
        });
      },
    });

    // Функция для подсветки ячеек в зависимости от значения относительно других в группе
    function colorizeCell($cell, value, groupKey, field, higherIsBetter) {
      const { min, max } = getMinMaxInGroup(groupKey, field);
      const normalizedValue = parseFloat(value);

      // Если в группе только одно значение или min=max, не подсвечиваем
      if (min === max || groupedData[groupKey].length <= 1) return;

      const range = max - min;
      const threshold = range * 0.2; // 20% от диапазона для определения "лучших" и "худших"

      if (higherIsBetter) {
        // Для метрик, где большее значение лучше (RPS)
        if (normalizedValue >= max - threshold) {
          $cell.addClass("cell-best");
        } else if (normalizedValue >= max - threshold * 2) {
          $cell.addClass("cell-good");
        } else if (normalizedValue <= min + threshold) {
          $cell.addClass("cell-worst");
        } else if (normalizedValue <= min + threshold * 2) {
          $cell.addClass("cell-bad");
        }
      } else {
        // Для метрик, где меньшее значение лучше (время, память и т.д.)
        if (normalizedValue <= min + threshold) {
          $cell.addClass("cell-best");
        } else if (normalizedValue <= min + threshold * 2) {
          $cell.addClass("cell-good");
        } else if (normalizedValue >= max - threshold) {
          $cell.addClass("cell-worst");
        } else if (normalizedValue >= max - threshold * 2) {
          $cell.addClass("cell-bad");
        }
      }
    }

    // Добавляем обработчики фильтров
    document
      .getElementById("table-filter-app")
      .addEventListener("change", function () {
        const value = this.value;
        if (value === "all") {
          resultsTable.columns(0).search("").draw();
        } else {
          resultsTable
            .columns(0)
            .search("^" + value + "$", true, false)
            .draw();
        }
      });

    document
      .getElementById("table-filter-parallel")
      .addEventListener("change", function () {
        const value = this.value;
        if (value === "all") {
          resultsTable.columns(1).search("").draw();
        } else {
          resultsTable
            .columns(1)
            .search("^" + value + "$", true, false)
            .draw();
        }
      });
  } catch (error) {
    console.error("Ошибка при создании таблицы:", error);
    document.getElementById("resultsTable").innerHTML =
      '<div class="alert alert-danger">Ошибка при создании таблицы. Пожалуйста, проверьте консоль для деталей.</div>';
  }
}

// Заполнение фильтров таблицы
function populateTableFilters(data) {
  const appFilter = document.getElementById("table-filter-app");
  const parallelFilter = document.getElementById("table-filter-parallel");

  // Сохраняем текущие значения
  const currentAppValue = appFilter.value;
  const currentParallelValue = parallelFilter.value;

  // Очищаем фильтры
  appFilter.innerHTML = '<option value="all">Все приложения</option>';
  parallelFilter.innerHTML = '<option value="all">Все параллельности</option>';

  // Получаем уникальные значения
  const apps = [...new Set(data.map((row) => row.Приложение))].sort();
  const parallels = [...new Set(data.map((row) => row.Параллельность))].sort(
    (a, b) => a - b
  );

  // Заполняем фильтр приложений
  apps.forEach((app) => {
    const option = document.createElement("option");
    option.value = app;
    option.textContent = app;
    appFilter.appendChild(option);
  });

  // Заполняем фильтр параллельности
  parallels.forEach((parallel) => {
    const option = document.createElement("option");
    option.value = parallel;
    option.textContent = `Параллельность: ${parallel}`;
    parallelFilter.appendChild(option);
  });

  // Восстанавливаем значения
  if (currentAppValue && apps.includes(currentAppValue)) {
    appFilter.value = currentAppValue;
  }

  if (
    currentParallelValue &&
    parallels.map(String).includes(currentParallelValue)
  ) {
    parallelFilter.value = currentParallelValue;
  }
}

// Заполнение переключателей колонок в модальном окне
function populateColumnToggles() {
  const container = document.getElementById("column-toggles");
  container.innerHTML = "";

  // Получаем все колонки таблицы
  if (!resultsTable) return;

  const columns = resultsTable.settings().init().columns;

  columns.forEach((col, index) => {
    // Создаем чекбокс для каждой колонки
    const toggleDiv = document.createElement("div");
    toggleDiv.className = "column-toggle";

    const checkbox = document.createElement("input");
    checkbox.type = "checkbox";
    checkbox.id = `toggle-${col.data}`;
    checkbox.className = "form-check-input";
    checkbox.checked = columnVisibility[col.data];
    checkbox.dataset.column = col.data;
    checkbox.dataset.index = index;

    const label = document.createElement("label");
    label.htmlFor = `toggle-${col.data}`;
    label.textContent = col.title;

    toggleDiv.appendChild(checkbox);
    toggleDiv.appendChild(label);
    container.appendChild(toggleDiv);
  });
}

// Применение настроек видимости колонок
function applyColumnVisibility() {
  if (!resultsTable) return;

  // Получаем все переключатели
  const toggles = document.querySelectorAll(
    '#column-toggles input[type="checkbox"]'
  );

  toggles.forEach((toggle) => {
    const columnName = toggle.dataset.column;
    const columnIndex = parseInt(toggle.dataset.index);
    const isVisible = toggle.checked;

    // Обновляем объект с настройками видимости
    columnVisibility[columnName] = isVisible;

    // Устанавливаем видимость колонки
    resultsTable.column(columnIndex).visible(isVisible, false);
  });

  // Перерисовываем таблицу после всех изменений
  resultsTable.columns.adjust().draw();
}

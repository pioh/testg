#!/usr/bin/env bun

import { spawn } from "child_process";
import { parse } from "csv-parse/sync";
import { readdirSync, readFileSync, statSync } from "fs";
import { join, resolve } from "path";

const PORT = 3000;
const STAT_DIR = resolve(process.cwd(), "stat");

// Функция для автоматического открытия URL в браузере
function openBrowser(url) {
  let command;
  let args;

  switch (process.platform) {
    case "darwin": // macOS
      command = "open";
      args = [url];
      break;
    case "win32": // Windows
      command = "cmd";
      args = ["/c", "start", url];
      break;
    default: // Linux и другие
      command = "xdg-open";
      args = [url];
      break;
  }

  spawn(command, args, { stdio: "ignore" });
}

// Функция для нахождения последней папки run в директории stat
function findLatestRunDirectory() {
  try {
    const dirs = readdirSync(STAT_DIR)
      .filter(
        (item) =>
          item.startsWith("run") && statSync(join(STAT_DIR, item)).isDirectory()
      )
      .sort((a, b) => {
        // Извлекаем номер из названия директории (например, 'run73' -> 73)
        const numA = parseInt(a.replace("run", ""), 10);
        const numB = parseInt(b.replace("run", ""), 10);
        return numB - numA; // Сортировка по убыванию для получения последней папки
      });

    if (dirs.length === 0) {
      console.error("Не найдены директории с результатами тестов");
      return null;
    }

    return join(STAT_DIR, dirs[0]);
  } catch (error) {
    console.error("Ошибка при поиске последней директории:", error);
    return null;
  }
}

// Функция для чтения данных из CSV файла
function readMatrixResults(runDir) {
  try {
    const filePath = join(runDir, "matrix-results.csv");
    const fileContent = readFileSync(filePath, "utf8");

    // Парсим CSV
    const records = parse(fileContent, {
      columns: true,
      delimiter: ",",
      skip_empty_lines: true,
    });

    return records;
  } catch (error) {
    console.error("Ошибка при чтении файла matrix-results.csv:", error);
    return [];
  }
}

// Анализ данных для выявления выбросов и интересных паттернов
function analyzeData(data) {
  if (!data || data.length === 0) return { data, insights: [] };

  // Группировка по приложениям
  const appGroups = {};
  data.forEach((row) => {
    if (!appGroups[row.Приложение]) {
      appGroups[row.Приложение] = [];
    }
    appGroups[row.Приложение].push(row);
  });

  // Анализ производительности
  const insights = [];

  // Находим приложение с максимальным RPS
  let maxRps = 0;
  let maxRpsApp = "";
  Object.entries(appGroups).forEach(([app, rows]) => {
    const maxAppRps = Math.max(...rows.map((r) => parseFloat(r.RPS)));
    if (maxAppRps > maxRps) {
      maxRps = maxAppRps;
      maxRpsApp = app;
    }
  });

  insights.push({
    type: "highlight",
    message: `Приложение ${maxRpsApp} показало наилучший RPS: ${maxRps}`,
  });

  // Находим аномальные значения (выбросы)
  Object.entries(appGroups).forEach(([app, rows]) => {
    // Проверяем неудачные запросы
    const failedRows = rows.filter((r) => parseInt(r.Запросы_неудачные) > 0);
    if (failedRows.length > 0) {
      insights.push({
        type: "warning",
        message: `Приложение ${app} имеет ${failedRows.length} конфигураций с неудачными запросами`,
      });
    }

    // Проверяем странные значения времени отклика
    const highLatencyRows = rows.filter(
      (r) =>
        parseFloat(r.Среднее_время_мс) > 5000 || parseFloat(r.P99_мс) > 8000
    );

    if (highLatencyRows.length > 0) {
      insights.push({
        type: "alert",
        message: `Приложение ${app} имеет ${highLatencyRows.length} конфигураций с очень высокой задержкой`,
      });
    }
  });

  return { data, insights };
}

// HTTP сервер
const server = Bun.serve({
  port: PORT,
  async fetch(req) {
    const url = new URL(req.url);

    // Обслуживание статических файлов из директории public
    if (url.pathname === "/" || url.pathname === "/index.html") {
      return new Response(
        Bun.file(join(import.meta.dir, "public", "index.html"))
      );
    }

    if (url.pathname.startsWith("/public/")) {
      const filePath = url.pathname.replace("/public/", "");
      return new Response(Bun.file(join(import.meta.dir, "public", filePath)));
    }

    // API эндпоинт для получения данных
    if (url.pathname === "/api/data") {
      const latestRunDir = findLatestRunDirectory();
      if (!latestRunDir) {
        return new Response(
          JSON.stringify({ error: "Не найдена директория с результатами" }),
          {
            headers: { "Content-Type": "application/json" },
          }
        );
      }

      const data = readMatrixResults(latestRunDir);
      const analyzedData = analyzeData(data);

      return new Response(
        JSON.stringify({
          ...analyzedData,
          runDir: latestRunDir,
        }),
        {
          headers: { "Content-Type": "application/json" },
        }
      );
    }

    // Возвращаем 404 для неизвестных маршрутов
    return new Response("Not Found", { status: 404 });
  },
});

console.log(`Сервер запущен на http://localhost:${PORT}`);

// Автоматически открываем браузер после запуска сервера
setTimeout(() => {
  openBrowser(`http://localhost:${PORT}`);
  console.log("Открываю браузер...");
}, 500); // Небольшая задержка для уверенности, что сервер уже запущен

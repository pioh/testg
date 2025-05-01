package com.srg.test;

import java.util.Map;
import java.util.Random;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicLong;

import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.client.RestTemplate;

import lombok.AllArgsConstructor;
import lombok.Data;
import lombok.NoArgsConstructor;
import lombok.extern.slf4j.Slf4j;

@RestController
@Slf4j
public class Api {

    private static final String BASE_URL = "http://localhost:8080";

    @Autowired
    private RestTemplate restTemplate;

    @Autowired
    private ThreadModelConfig threadModelConfig;

    private final Random random = new Random();

    // Статистика
    private final AtomicInteger activeRequests = new AtomicInteger(0);
    private final AtomicInteger totalRequests = new AtomicInteger(0);
    private final AtomicInteger completedChains = new AtomicInteger(0);
    private final AtomicInteger failedRequests = new AtomicInteger(0);
    private final AtomicLong totalProcessingTimeMs = new AtomicLong(0);
    private final AtomicLong maxProcessingTimeMs = new AtomicLong(0);
    private final Map<Integer, AtomicInteger> depthDistribution = new ConcurrentHashMap<>();

    @GetMapping("/check")
    public ResponseEntity<Response> check(
            @RequestParam(value = "left", defaultValue = "0") int left,
            @RequestParam(value = "rnd", required = false) String rnd) {

        long startTime = System.currentTimeMillis();
        int currentActive = activeRequests.incrementAndGet();
        totalRequests.incrementAndGet();

        // Запоминаем глубину для статистики
        depthDistribution.computeIfAbsent(left, k -> new AtomicInteger(0)).incrementAndGet();

        try {
            // Если это первый запрос без rnd, генерируем случайное число
            if (rnd == null) {
                rnd = String.valueOf(random.nextInt(100000));
            }

            // Если left > 0, делаем рекурсивный вызов
            if (left > 0) {
                String nextUrl = BASE_URL + "/check?left=" + (left - 1) + "&rnd=" + rnd;

                ResponseEntity<Response> response = restTemplate.getForEntity(nextUrl, Response.class);
                return response;
            } else {
                // Если left=0, завершаем цепочку и возвращаем rnd
                completedChains.incrementAndGet();
                return ResponseEntity.ok(new Response(rnd, getStatistics()));
            }
        } catch (Exception e) {
            log.error("Error processing request", e);
            failedRequests.incrementAndGet();
            return ResponseEntity.status(500).body(new Response(null, "Error: " + e.getMessage()));
        } finally {
            activeRequests.decrementAndGet();
            long processingTime = System.currentTimeMillis() - startTime;
            totalProcessingTimeMs.addAndGet(processingTime);

            // Обновляем максимальное время
            long currentMax;
            do {
                currentMax = maxProcessingTimeMs.get();
                if (processingTime <= currentMax)
                    break;
            } while (!maxProcessingTimeMs.compareAndSet(currentMax, processingTime));
        }
    }

    @GetMapping("/stats")
    public Statistics getStatistics() {
        Statistics stats = new Statistics();
        stats.setActiveRequests(activeRequests.get());
        stats.setTotalRequests(totalRequests.get());
        stats.setCompletedChains(completedChains.get());
        stats.setFailedRequests(failedRequests.get());

        long total = totalRequests.get();
        long totalTime = totalProcessingTimeMs.get();
        stats.setTotalProcessingTimeMs(totalTime);
        stats.setMaxProcessingTimeMs(maxProcessingTimeMs.get());
        stats.setAverageProcessingTimeMs(total > 0 ? (double) totalTime / total : 0);

        stats.setDepthDistribution(new ConcurrentHashMap<>(depthDistribution));
        stats.setThreadModel(threadModelConfig.getThreadModel());
        stats.setThreadCount(threadModelConfig.getThreadCount());
        stats.setAsyncNio(threadModelConfig.isAsyncNio());

        return stats;
    }

    @GetMapping("/stats/reset")
    public String resetStatistics() {
        activeRequests.set(0);
        totalRequests.set(0);
        completedChains.set(0);
        failedRequests.set(0);
        totalProcessingTimeMs.set(0);
        maxProcessingTimeMs.set(0);
        depthDistribution.clear();
        return "Statistics reset successful";
    }

    @GetMapping("/")
    public String root() {
        return "API is running with thread model: " + threadModelConfig.getThreadModel() +
                ", thread count: " + threadModelConfig.getThreadCount() +
                ", asyncNio: " + threadModelConfig.isAsyncNio() +
                ". Use /check?left=N to start a test or /stats to view statistics.";
    }

    @Data
    @NoArgsConstructor
    @AllArgsConstructor
    public static class Response {
        private String rnd;
        private Object statistics;
    }

    @Data
    public static class Statistics {
        private int activeRequests;
        private int totalRequests;
        private int completedChains;
        private int failedRequests;
        private long totalProcessingTimeMs;
        private long maxProcessingTimeMs;
        private double averageProcessingTimeMs;
        private Map<Integer, AtomicInteger> depthDistribution;
        private String threadModel;
        private int threadCount;
        private boolean asyncNio;
    }
}

package com.srg.test;

import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.SynchronousQueue;
import java.util.concurrent.ThreadPoolExecutor;
import java.util.concurrent.TimeUnit;

import org.springframework.boot.CommandLineRunner;
import org.springframework.boot.context.properties.ConfigurationProperties;
import org.springframework.stereotype.Component;

import lombok.Data;
import lombok.extern.slf4j.Slf4j;

@Component
@ConfigurationProperties(prefix = "app")
@Data
@Slf4j
public class ThreadModelConfig implements CommandLineRunner {

    private String threadModel = "virtual"; // default to virtual threads
    private int threadCount = 24; // default thread count when using fixed pool
    private boolean asyncNio = false; // use async NIO

    // Конструктор по умолчанию для Spring
    public ThreadModelConfig() {
        log.info("ThreadModelConfig initialized with defaults: model={}, count={}, asyncNio={}",
                threadModel, threadCount, asyncNio);
    }

    @Override
    public void run(String... args) throws Exception {
        for (String arg : args) {
            if (arg.startsWith("--thread-model=")) {
                threadModel = arg.substring("--thread-model=".length());
            } else if (arg.startsWith("--thread-count=")) {
                try {
                    threadCount = Integer.parseInt(arg.substring("--thread-count=".length()));
                } catch (NumberFormatException e) {
                    log.warn("Invalid thread-count value, using default: {}", threadCount);
                }
            }
        }

        if (threadModel.equals("asyncNio")) {
            asyncNio = true;
            threadModel = "fixed"; // asyncNio использует фиксированный пул потоков
        }

        log.info("Thread model configured: {}, Thread count: {}, Async NIO: {}",
                threadModel, threadCount, asyncNio);
    }

    public ExecutorService createExecutor() {
        switch (threadModel.toLowerCase()) {
            case "virtual":
                return Executors.newVirtualThreadPerTaskExecutor();
            case "fixed":
                if (threadCount <= 0) {
                    threadCount = Runtime.getRuntime().availableProcessors();
                }
                return Executors.newFixedThreadPool(threadCount);
            default:
                log.warn("Unknown thread model: {}, using virtual threads", threadModel);
                return Executors.newVirtualThreadPerTaskExecutor();
        }
    }

    /**
     * Создает пул потоков для обработки HTTP запросов Tomcat
     */
    public ExecutorService createTomcatExecutor() {
        if (threadModel.equalsIgnoreCase("virtual")) {
            return Executors.newVirtualThreadPerTaskExecutor();
        } else if (threadModel.equalsIgnoreCase("fixed")) {
            // Для фиксированного пула создаем ThreadPoolExecutor с неограниченной очередью
            return new ThreadPoolExecutor(
                    threadCount, threadCount,
                    60L, TimeUnit.SECONDS,
                    new SynchronousQueue<>(),
                    Executors.defaultThreadFactory(),
                    new ThreadPoolExecutor.CallerRunsPolicy());
        } else {
            return Executors.newVirtualThreadPerTaskExecutor();
        }
    }
}
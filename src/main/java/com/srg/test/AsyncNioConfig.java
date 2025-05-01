package com.srg.test;

import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Conditional;
import org.springframework.context.annotation.Configuration;
import org.springframework.context.annotation.Primary;
import org.springframework.scheduling.concurrent.ThreadPoolTaskExecutor;
import org.springframework.web.client.RestTemplate;

import lombok.extern.slf4j.Slf4j;

/**
 * Конфигурация для асинхронного NIO
 * Активируется только если в ThreadModelConfig установлен флаг asyncNio = true
 */
@Configuration
@Slf4j
public class AsyncNioConfig {

    @Autowired
    private ThreadModelConfig threadModelConfig;

    /**
     * Условие для активации бинов - только если выбран режим AsyncNIO
     */
    public static class AsyncNioCondition implements org.springframework.context.annotation.Condition {
        @Override
        public boolean matches(org.springframework.context.annotation.ConditionContext context,
                org.springframework.core.type.AnnotatedTypeMetadata metadata) {
            try {
                // Проверяем наличие свойства в контексте вместо получения бина напрямую
                String asyncNioProperty = context.getEnvironment().getProperty("app.async-nio");
                return "true".equalsIgnoreCase(asyncNioProperty);
            } catch (Exception e) {
                // В случае проблем не активируем бин
                return false;
            }
        }
    }

    /**
     * Создает RestTemplate для выполнения запросов в фиксированном пуле потоков
     * В режиме AsyncNIO мы просто используем обычный RestTemplate,
     * но параллельность обеспечивается через пул потоков в Tomcat
     */
    @Bean
    @Primary
    @Conditional(AsyncNioCondition.class)
    public RestTemplate asyncRestTemplate() {
        log.info("Configuring RestTemplate for AsyncNIO mode with thread count: {}",
                threadModelConfig.getThreadCount());
        return new RestTemplate();
    }

    /**
     * Пул потоков для асинхронных задач
     */
    @Bean
    @Conditional(AsyncNioCondition.class)
    public ThreadPoolTaskExecutor threadPoolTaskExecutor() {
        ThreadPoolTaskExecutor executor = new ThreadPoolTaskExecutor();
        executor.setCorePoolSize(threadModelConfig.getThreadCount());
        executor.setMaxPoolSize(threadModelConfig.getThreadCount());
        executor.setQueueCapacity(500);
        executor.setThreadNamePrefix("AsyncNioExecutor-");
        executor.initialize();
        return executor;
    }
}
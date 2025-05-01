package com.srg.test;

import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Conditional;
import org.springframework.context.annotation.Configuration;
import org.springframework.context.annotation.Primary;
import org.springframework.web.client.RestTemplate;

/**
 * Конфигурация RestTemplate по умолчанию
 * Будет использоваться, если не активирован AsyncNioConfig.asyncRestTemplate
 */
@Configuration
public class RestTemplateConfig {

    /**
     * Условие для активации бинов - только если НЕ выбран режим AsyncNIO
     */
    public static class NotAsyncNioCondition implements org.springframework.context.annotation.Condition {
        @Override
        public boolean matches(org.springframework.context.annotation.ConditionContext context,
                org.springframework.core.type.AnnotatedTypeMetadata metadata) {
            try {
                // Проверяем отсутствие свойства в контексте
                String asyncNioProperty = context.getEnvironment().getProperty("app.async-nio");
                return !"true".equalsIgnoreCase(asyncNioProperty);
            } catch (Exception e) {
                // В случае проблем активируем бин по умолчанию
                return true;
            }
        }
    }

    @Bean
    @Primary
    @Conditional(NotAsyncNioCondition.class)
    public RestTemplate restTemplate() {
        return new RestTemplate();
    }
}
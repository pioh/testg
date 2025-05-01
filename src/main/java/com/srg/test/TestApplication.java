package com.srg.test;

import org.apache.coyote.AbstractProtocol;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.boot.autoconfigure.task.TaskExecutionAutoConfiguration;
import org.springframework.boot.web.embedded.tomcat.TomcatProtocolHandlerCustomizer;
import org.springframework.context.annotation.Bean;
import org.springframework.core.task.AsyncTaskExecutor;
import org.springframework.core.task.support.TaskExecutorAdapter;
import org.springframework.web.servlet.config.annotation.EnableWebMvc;

@SpringBootApplication
@EnableWebMvc
public class TestApplication {

	@Autowired
	private ThreadModelConfig threadModelConfig;

	@Bean(TaskExecutionAutoConfiguration.APPLICATION_TASK_EXECUTOR_BEAN_NAME)
	public AsyncTaskExecutor asyncTaskExecutor() {
		return new TaskExecutorAdapter(threadModelConfig.createExecutor());
	}

	@Bean
	public TomcatProtocolHandlerCustomizer<?> protocolHandlerVirtualThreadExecutorCustomizer() {
		return protocolHandler -> {
			protocolHandler.setExecutor(threadModelConfig.createTomcatExecutor());

			// Для асинхронного NIO установим настройки, если необходимо
			if (threadModelConfig.isAsyncNio() && protocolHandler instanceof AbstractProtocol) {
				AbstractProtocol<?> protocol = (AbstractProtocol<?>) protocolHandler;
				protocol.setConnectionTimeout(30000);
			}
		};
	}

	public static void main(String[] args) {
		SpringApplication.run(TestApplication.class, args);
	}
}

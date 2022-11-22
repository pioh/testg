package com.srg.test;

import lombok.extern.java.Log;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

import java.util.concurrent.atomic.AtomicInteger;


@RestController
@Log
public class Api {
    AtomicInteger counter = new AtomicInteger(0);
    @GetMapping("/check")
    public String check() throws InterruptedException {
        int c = counter.incrementAndGet();
        log.info("enter check " + c);
//        Thread.sleep(1000 * 10);
        log.info("exit check " + c);
        return "OK";
    }
}

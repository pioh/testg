package main

import (
	"math/rand"
	"runtime"
	"time"
)

// Response представляет JSON-ответ сервера
type Response struct {
	Rnd string `json:"rnd"`
}

func init() {
	// Инициализация генератора случайных чисел
	// Используем более современный подход начиная с Go 1.20
	rand.New(rand.NewSource(time.Now().UnixNano()))

	// Установка максимального числа процессоров по умолчанию
	// Эта настройка может быть переопределена при запуске с GOMAXPROCS=N
	// Например, GOMAXPROCS=1 для тестирования с одним процессором
	_ = runtime.GOMAXPROCS(runtime.NumCPU())
}

package main

import (
	"sync"
	"time"

	"github.com/valyala/fasthttp"
)

// AppManagerResponse представляет ответ от app-manager
type AppManagerResponse struct {
	PID         int     `json:"pid"`
	StartupTime float64 `json:"startup_time"`
	AppName     string  `json:"app_name"`
}

// ResourceMetric представляет метрику использования ресурсов
type ResourceMetric struct {
	Timestamp time.Time
	CPU       float64
	Memory    float64 // МБ
}

// ResourceStats представляет статистику использования ресурсов
type ResourceStats struct {
	Metrics      []ResourceMetric
	AvgCPU       float64
	MaxCPU       float64
	AvgMemory    float64
	MaxMemory    float64
	StartupTime  float64
	CollectMutex sync.Mutex
}

// ResourceStatsInterface определяет методы для работы со статистикой ресурсов
type ResourceStatsInterface interface {
	// AddMetric добавляет метрику использования ресурсов
	AddMetric(cpu, memory float64)
	// Calculate вычисляет статистические данные
	Calculate()
}

// Проверка, что ResourceStats реализует ResourceStatsInterface
var _ ResourceStatsInterface = (*ResourceStats)(nil)

// LatencyMetric представляет метрику задержки
type LatencyMetric struct {
	Min            time.Duration
	Max            time.Duration
	Mean           time.Duration
	Median         time.Duration
	P90            time.Duration
	P95            time.Duration
	P99            time.Duration
	StdDev         time.Duration
	Samples        []time.Duration
	FirstResp      time.Duration
	FirstBatchTime time.Duration
	StartTime      time.Time
	FinishTime     time.Time
}

// LatencyMetricInterface определяет методы для работы с метриками задержки
type LatencyMetricInterface interface {
	// AddSample добавляет образец времени
	AddSample(d time.Duration)
	// Calculate вычисляет статистические данные
	Calculate()
}

// Проверка, что LatencyMetric реализует LatencyMetricInterface
var _ LatencyMetricInterface = (*LatencyMetric)(nil)

// SecondStat хранит статистику за одну секунду
type SecondStat struct {
	Second   int
	Requests int
	RPS      float64
}

// TestResult представляет результаты теста
type TestResult struct {
	Parallelism    int
	Depth          int
	Latency        *LatencyMetric
	Resources      *ResourceStats
	AppName        string
	AppPID         int
	Requests       int64
	Success        int64
	Failures       int64
	Mismatches     int64
	RPS            float64
	TestDuration   time.Duration
	SecondBySecond []SecondStat
}

// TesterInterface определяет методы для тестера производительности
type TesterInterface interface {
	// RunMatrixTest запускает тест с конкретной комбинацией parallelism и depth
	RunMatrixTest(parallelism, depth int) *TestResult
	// RunMatrixTests запускает полное матричное тестирование
	RunMatrixTests(showTableAfterEachTest bool)
	// StartApplication запускает приложение через app-manager
	StartApplication() error
	// StopApplication останавливает приложение через app-manager
	StopApplication() error
	// StartResourceCollection запускает сбор метрик использования ресурсов
	StartResourceCollection()
	// InitResultFiles инициализирует файлы результатов
	InitResultFiles() error
	// SaveResult сохраняет результат теста в файлы
	SaveResult(result *TestResult) error
	// updateSecondStat обновляет статистику за секунду
	updateSecondStat(second, totalRequests int)
}

// Tester представляет тестер производительности
type Tester struct {
	BaseURL                string
	TestDuration           time.Duration
	MaxParallelism         int
	MaxDepth               int
	ResultsFile            string
	FullStatsFile          string
	AppManagerPath         string
	AppName                string
	CurrentAppPID          int
	ResourceStats          *ResourceStats
	Client                 *fasthttp.Client
	SecondBySecond         []SecondStat
	SecondStatMutex        sync.Mutex
	StopResourceCollection chan struct{}
}

// Проверка, что Tester реализует TesterInterface
var _ TesterInterface = (*Tester)(nil)

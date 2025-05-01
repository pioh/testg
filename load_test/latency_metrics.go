package main

import (
	"math"
	"sort"
	"time"
)

// NewLatencyMetric создает новую метрику задержки
func NewLatencyMetric() *LatencyMetric {
	return &LatencyMetric{
		Min:            time.Duration(math.MaxInt64),
		Max:            0,
		Mean:           0,
		Median:         0,
		P90:            0,
		P95:            0,
		P99:            0,
		StdDev:         0,
		Samples:        make([]time.Duration, 0, 10000),
		StartTime:      time.Now(),
		FirstResp:      0,
		FirstBatchTime: 0,
	}
}

// AddSample добавляет образец времени
func (lm *LatencyMetric) AddSample(d time.Duration) {
	lm.Samples = append(lm.Samples, d)
	if d < lm.Min {
		lm.Min = d
	}
	if d > lm.Max {
		lm.Max = d
	}
}

// Calculate вычисляет статистические данные
func (lm *LatencyMetric) Calculate() {
	lm.FinishTime = time.Now()
	if len(lm.Samples) == 0 {
		return
	}

	// Сортируем выборки для вычисления перцентилей
	sort.Slice(lm.Samples, func(i, j int) bool {
		return lm.Samples[i] < lm.Samples[j]
	})

	// Вычисляем среднее
	var sum time.Duration
	for _, s := range lm.Samples {
		sum += s
	}
	lm.Mean = sum / time.Duration(len(lm.Samples))

	// Вычисляем медиану и перцентили
	lm.Median = lm.Samples[len(lm.Samples)/2]
	lm.P90 = lm.Samples[int(float64(len(lm.Samples))*0.9)]
	lm.P95 = lm.Samples[int(float64(len(lm.Samples))*0.95)]
	lm.P99 = lm.Samples[int(float64(len(lm.Samples))*0.99)]

	// Вычисляем среднеквадратическое отклонение
	var sumSquares float64
	for _, s := range lm.Samples {
		diff := float64(s - lm.Mean)
		sumSquares += diff * diff
	}
	variance := sumSquares / float64(len(lm.Samples))
	lm.StdDev = time.Duration(math.Sqrt(variance))
}

package main

import "time"

// NewResourceStats создает новую статистику ресурсов
func NewResourceStats() *ResourceStats {
	return &ResourceStats{
		Metrics:   make([]ResourceMetric, 0, 100),
		AvgCPU:    0,
		MaxCPU:    0,
		AvgMemory: 0,
		MaxMemory: 0,
	}
}

// AddMetric добавляет метрику использования ресурсов
func (rs *ResourceStats) AddMetric(cpu, memory float64) {
	rs.CollectMutex.Lock()
	defer rs.CollectMutex.Unlock()

	rs.Metrics = append(rs.Metrics, ResourceMetric{
		Timestamp: time.Now(),
		CPU:       cpu,
		Memory:    memory,
	})

	if cpu > rs.MaxCPU {
		rs.MaxCPU = cpu
	}

	if memory > rs.MaxMemory {
		rs.MaxMemory = memory
	}
}

// Calculate вычисляет статистические данные
func (rs *ResourceStats) Calculate() {
	rs.CollectMutex.Lock()
	defer rs.CollectMutex.Unlock()

	if len(rs.Metrics) == 0 {
		return
	}

	var totalCPU, totalMemory float64
	for _, m := range rs.Metrics {
		totalCPU += m.CPU
		totalMemory += m.Memory
	}

	rs.AvgCPU = totalCPU / float64(len(rs.Metrics))
	rs.AvgMemory = totalMemory / float64(len(rs.Metrics))
}

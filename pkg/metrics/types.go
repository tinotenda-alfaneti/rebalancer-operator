package metrics

import "time"

// NodeUsage represents utilization data for a node over a sampling window.
type NodeUsage struct {
	CPUCores    float64            `json:"cpuCores"`
	MemoryBytes int64              `json:"memoryBytes"`
	Window      time.Duration      `json:"window"`
	Timestamp   time.Time          `json:"timestamp"`
	Custom      map[string]float64 `json:"custom,omitempty"`
}

// PodUsage captures per-pod utilization figures.
type PodUsage struct {
	CPUCores    float64            `json:"cpuCores"`
	MemoryBytes int64              `json:"memoryBytes"`
	Window      time.Duration      `json:"window"`
	Timestamp   time.Time          `json:"timestamp"`
	Custom      map[string]float64 `json:"custom,omitempty"`
	Node        string             `json:"node"`
}

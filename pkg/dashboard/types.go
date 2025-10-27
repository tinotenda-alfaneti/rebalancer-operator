package dashboard

import "time"

// NodeSummary represents a node with computed utilization and classification.
type NodeSummary struct {
	Name           string            `json:"name"`
	Labels         map[string]string `json:"labels,omitempty"`
	CPUCapacity    float64           `json:"cpuCapacity"`
	CPUUsage       float64           `json:"cpuUsage"`
	CPUUtilization float64           `json:"cpuUtilization"`
	MemoryCapacity int64             `json:"memoryCapacity"`
	MemoryUsage    int64             `json:"memoryUsage"`
	MemoryUtil     float64           `json:"memoryUtilization"`
	Classification string            `json:"classification"`
}

// PlanSummary mirrors the status block of a RebalancePolicy.
type PlanSummary struct {
	HotNodes          int        `json:"hotNodes"`
	ColdNodes         int        `json:"coldNodes"`
	Variance          float64    `json:"variance"`
	EvictionsPlanned  int        `json:"evictionsPlanned"`
	EvictionsExecuted int        `json:"evictionsExecuted"`
	LastExecutionTime *time.Time `json:"lastExecutionTime,omitempty"`
	DryRun            bool       `json:"dryRun"`
}

// PolicySummary summarises a policy and its latest status.
type PolicySummary struct {
	Name                    string       `json:"name"`
	TargetVariance          float64      `json:"targetVariance"`
	HotThreshold            float64      `json:"hotThreshold"`
	ColdThreshold           float64      `json:"coldThreshold"`
	MaxEvictionsPerMinute   int          `json:"maxEvictionsPerMinute"`
	MaxEvictionsPerNamespace int         `json:"maxEvictionsPerNamespace"`
	DryRun                  bool         `json:"dryRun"`
	Plan                    PlanSummary  `json:"plan"`
	NamespacesAllowed       []string     `json:"namespacesAllowed,omitempty"`
	NamespacesExcluded      []string     `json:"namespacesExcluded,omitempty"`
}

// DashboardResponse contains aggregated data for the UI.
type DashboardResponse struct {
	GeneratedAt time.Time      `json:"generatedAt"`
	Nodes       []NodeSummary  `json:"nodes"`
	Policies    []PolicySummary `json:"policies"`
}

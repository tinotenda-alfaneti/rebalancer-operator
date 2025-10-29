package metrics

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/api"
	promapi "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

// Provider exposes cluster utilization insights.
type Provider interface {
	CollectNodeMetrics(ctx context.Context) (map[string]NodeUsage, error)
	CollectPodMetrics(ctx context.Context) (map[types.NamespacedName]PodUsage, error)
}

// ProviderOptions configures the metrics provider.
type ProviderOptions struct {
	EnableMetricsServer bool
	EnablePrometheus    bool
	Prometheus          PrometheusOptions
}

// PrometheusOptions configure optional Prometheus integration.
type PrometheusOptions struct {
	Address            string
	BearerTokenFile    string
	InsecureSkipVerify bool
	Timeout            time.Duration
	CustomNodeQueries  map[string]string
	CustomPodQueries   map[string]string
}

type provider struct {
	metricsClient metricsclient.Interface
	prom          promapi.API

	customNodeQueries map[string]string
	customPodQueries  map[string]string
	httpTimeout       time.Duration
}

// NewProvider creates a metrics provider backed by metrics-server and Prometheus.
func NewProvider(cfg *rest.Config, opts ProviderOptions) (Provider, error) {
	p := &provider{
		httpTimeout: 15 * time.Second,
	}

	if opts.EnableMetricsServer {
		client, err := metricsclient.NewForConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("create metrics client: %w", err)
		}
		p.metricsClient = client
	}

	if opts.EnablePrometheus && opts.Prometheus.Address != "" {
		rt := api.DefaultRoundTripper
		if opts.Prometheus.Timeout > 0 {
			p.httpTimeout = opts.Prometheus.Timeout
		}

		transportConfig := api.Config{
			Address: opts.Prometheus.Address,
			RoundTripper: &bearerAuthRoundTripper{
				parent: rt,
				token:  readTokenFile(opts.Prometheus.BearerTokenFile),
			},
		}

		client, err := api.NewClient(transportConfig)
		if err != nil {
			return nil, fmt.Errorf("create prometheus client: %w", err)
		}
		p.prom = promapi.NewAPI(client)
		p.customNodeQueries = opts.Prometheus.CustomNodeQueries
		p.customPodQueries = opts.Prometheus.CustomPodQueries
	}

	if p.metricsClient == nil && p.prom == nil {
		return nil, errors.New("no metrics backend enabled")
	}

	return p, nil
}

// CollectNodeMetrics retrieves utilization data per node.
func (p *provider) CollectNodeMetrics(ctx context.Context) (map[string]NodeUsage, error) {
	result := make(map[string]NodeUsage)

	if p.metricsClient != nil {
		resp, err := p.metricsClient.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
		if err != nil {
			if isMetricsAPIUnavailable(err) {
				goto custom
			}
			return nil, fmt.Errorf("list node metrics: %w", err)
		}
		for _, item := range resp.Items {
			cpuCores := float64(item.Usage.Cpu().MilliValue()) / 1000.0
			memBytes := item.Usage.Memory().Value()
			result[item.Name] = NodeUsage{
				CPUCores:    cpuCores,
				MemoryBytes: memBytes,
				Window:      item.Window.Duration,
				Timestamp:   item.Timestamp.Time,
				Custom:      make(map[string]float64),
			}
		}
	}

custom:
	if p.prom != nil && len(p.customNodeQueries) > 0 {
		if err := p.populateCustomNodeMetrics(ctx, result); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// CollectPodMetrics returns pod level usage data.
func (p *provider) CollectPodMetrics(ctx context.Context) (map[types.NamespacedName]PodUsage, error) {
	result := make(map[types.NamespacedName]PodUsage)

	if p.metricsClient != nil {
		resp, err := p.metricsClient.MetricsV1beta1().PodMetricses(corev1.NamespaceAll).List(ctx, metav1.ListOptions{})
		if err != nil {
			if isMetricsAPIUnavailable(err) {
				goto custom
			}
			return nil, fmt.Errorf("list pod metrics: %w", err)
		}
		for _, item := range resp.Items {
			var totalCPU float64
			var totalMem int64
			for _, c := range item.Containers {
				totalCPU += float64(c.Usage.Cpu().MilliValue()) / 1000.0
				totalMem += c.Usage.Memory().Value()
			}
			key := types.NamespacedName{Namespace: item.Namespace, Name: item.Name}
			result[key] = PodUsage{
				CPUCores:    totalCPU,
				MemoryBytes: totalMem,
				Window:      item.Window.Duration,
				Timestamp:   item.Timestamp.Time,
				Custom:      make(map[string]float64),
			}
		}
	}

custom:
	if p.prom != nil && len(p.customPodQueries) > 0 {
		if err := p.populateCustomPodMetrics(ctx, result); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func (p *provider) populateCustomNodeMetrics(ctx context.Context, usages map[string]NodeUsage) error {
	for name, query := range p.customNodeQueries {
		vector, err := p.runPrometheusQuery(ctx, query)
		if err != nil {
			return fmt.Errorf("prometheus node query %q failed: %w", name, err)
		}
		for _, sample := range vector {
			node := string(sample.Metric["node"])
			if node == "" {
				continue
			}
			usage := usages[node]
			if usage.Custom == nil {
				usage.Custom = make(map[string]float64)
			}
			usage.Custom[name] = float64(sample.Value)
			if usage.Timestamp.IsZero() {
				usage.Timestamp = time.Now()
			}
			usages[node] = usage
		}
	}
	return nil
}

func (p *provider) populateCustomPodMetrics(ctx context.Context, usages map[types.NamespacedName]PodUsage) error {
	for name, query := range p.customPodQueries {
		vector, err := p.runPrometheusQuery(ctx, query)
		if err != nil {
			return fmt.Errorf("prometheus pod query %q failed: %w", name, err)
		}
		for _, sample := range vector {
			ns := string(sample.Metric["namespace"])
			pod := string(sample.Metric["pod"])
			if ns == "" || pod == "" {
				continue
			}
			key := types.NamespacedName{Namespace: ns, Name: pod}
			usage := usages[key]
			if usage.Custom == nil {
				usage.Custom = make(map[string]float64)
			}
			usage.Custom[name] = float64(sample.Value)
			if usage.Timestamp.IsZero() {
				usage.Timestamp = time.Now()
			}
			usages[key] = usage
		}
	}
	return nil
}

func (p *provider) runPrometheusQuery(ctx context.Context, query string) (model.Vector, error) {
	if p.prom == nil {
		return nil, errors.New("prometheus not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, p.httpTimeout)
	defer cancel()
	value, _, err := p.prom.Query(ctx, query, time.Now())
	if err != nil {
		return nil, err
	}
	vector, ok := value.(model.Vector)
	if !ok {
		return nil, fmt.Errorf("unexpected result type %T", value)
	}
	return vector, nil
}

type bearerAuthRoundTripper struct {
	parent http.RoundTripper
	token  string
}

func (rt *bearerAuthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if rt.token != "" {
		req.Header.Set("Authorization", "Bearer "+rt.token)
	}
	parent := rt.parent
	if parent == nil {
		parent = http.DefaultTransport
	}
	return parent.RoundTrip(req)
}

func readTokenFile(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func isMetricsAPIUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if apierrors.IsNotFound(err) || apierrors.IsServiceUnavailable(err) {
		return true
	}
	msg := err.Error()
	if strings.Contains(msg, "the server could not find the requested resource") && strings.Contains(msg, "metrics.k8s.io") {
		return true
	}
	return false
}

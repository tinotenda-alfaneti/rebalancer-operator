package main

import (
	"flag"
	"net/http"
	"os"
	"time"

	rbv1 "github.com/you/rebalancer/api/v1"
	"github.com/you/rebalancer/controllers"
	"github.com/you/rebalancer/controllers/executors"
	"github.com/you/rebalancer/controllers/planner"
	"github.com/you/rebalancer/pkg/dashboard"
	"github.com/you/rebalancer/pkg/kube"
	"github.com/you/rebalancer/pkg/metrics"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(rbv1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var probeAddr string
	var enableLeaderElection bool
	var syncPeriod time.Duration
	var prometheusAddress string
	var emptyDirThresholdGi int

	opts := zap.Options{
		Development: false,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}
	opts.BindFlags(flag.CommandLine)
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", true, "Enable leader election for controller manager.")
	flag.DurationVar(&syncPeriod, "sync-period", 30*time.Second, "Reconciliation sync period.")
	flag.StringVar(&prometheusAddress, "prometheus-address", "", "Optional Prometheus base URL for extended metrics scraping.")
	flag.IntVar(&emptyDirThresholdGi, "emptydir-threshold-gi", planner.DefaultEmptyDirThresholdGi, "Skip pods with emptyDir volumes larger than this many GiB (set to 0 to keep the default).")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	dashboardHandler, err := dashboard.NewHandler()
	if err != nil {
		setupLog.Error(err, "unable to create dashboard handler")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
			ExtraHandlers: map[string]http.Handler{
				"/":               http.HandlerFunc(dashboardHandler.Static),
				"/dashboard":      http.HandlerFunc(dashboardHandler.Static),
				"/dashboard/":     http.HandlerFunc(dashboardHandler.Static),
				"/dashboard/data": http.HandlerFunc(dashboardHandler.Data),
			},
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "rebalancer-operator",
		Cache: cache.Options{
			SyncPeriod: &syncPeriod,
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	restCfg := mgr.GetConfig()

	kubeCaches, err := kube.NewSharedCacheSet(restCfg)
	if err != nil {
		setupLog.Error(err, "unable to construct shared cache helpers")
		os.Exit(1)
	}

	providerOpts := metrics.ProviderOptions{EnableMetricsServer: true}
	if prometheusAddress != "" {
		providerOpts.EnablePrometheus = true
		providerOpts.Prometheus.Address = prometheusAddress
	}
	metricsProvider, err := metrics.NewProvider(restCfg, providerOpts)
	if err != nil {
		setupLog.Error(err, "unable to create metrics provider")
		os.Exit(1)
	}

	emptyDirLimit := int64(emptyDirThresholdGi)
	if emptyDirLimit <= 0 {
		emptyDirLimit = planner.DefaultEmptyDirThresholdGi
	}
	rebalancerPlanner := planner.New(planner.Options{
		Client:          mgr.GetClient(),
		MetricsProvider: metricsProvider,
		CacheSet:        kubeCaches,
		EmptyDirLimitGi: emptyDirLimit,
	})

	evictor := executors.NewEvictor(mgr.GetClient())

	dashboardHandler.Attach(mgr.GetClient(), metricsProvider, kubeCaches)

	if err := controllers.SetupWithManager(mgr, rebalancerPlanner, evictor); err != nil {
		setupLog.Error(err, "unable to register controllers")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

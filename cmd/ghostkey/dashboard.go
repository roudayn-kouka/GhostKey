package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/roudayna/ghostkey/pkg/dashboard"
	"github.com/roudayna/ghostkey/pkg/logging"
)

var dashboardScheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(dashboardScheme))
}

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Run the GhostKey read-only web dashboard",
	Long: `Serves a read-only web dashboard of GhostKey's state in the cluster: controller
pods per node, webhook status, data-plane modes, protected secrets, tracked pods
and binding / SNI violations. It never returns a secret value.

Reach it with: kubectl port-forward -n ghostkey-system svc/ghostkey-dashboard 8090:8090`,
	Run: runDashboard,
}

var (
	dashboardBindAddr     string
	dashboardProbeAddr    string
	dashboardNamespace    string
	dashboardAllowedHosts string
	dashboardDemoData     bool
)

func init() {
	dashboardCmd.Flags().StringVar(&dashboardBindAddr, "bind-address", ":8090", "The address the dashboard binds to.")
	dashboardCmd.Flags().StringVar(&dashboardProbeAddr, "health-probe-bind-address", ":8091", "The address the probe endpoint binds to.")
	dashboardCmd.Flags().StringVar(&dashboardNamespace, "controller-namespace", "",
		"Namespace of the GhostKey controller and webhook (default: $POD_NAMESPACE, else ghostkey-system).")
	dashboardCmd.Flags().StringVar(&dashboardAllowedHosts, "allowed-hosts", "",
		"Comma-separated extra Host header values the dashboard answers, besides localhost, 127.0.0.1, ::1 "+
			"and the ghostkey-dashboard Service names (DNS-rebinding protection; \"*\" allows any host).")
	dashboardCmd.Flags().BoolVar(&dashboardDemoData, "demo-data", false,
		"Serve fictitious demonstration data instead of reading a cluster (no kubeconfig needed).")
}

// dashboardServiceName is the chart's dashboard Service, allowed as a Host.
const dashboardServiceName = "ghostkey-dashboard"

func runDashboard(cmd *cobra.Command, args []string) {
	setupLog := logging.Setup().Named("setup")
	log := setupLog.Named("dashboard")

	namespace := dashboardNamespace
	if namespace == "" {
		namespace = os.Getenv("POD_NAMESPACE")
	}
	if namespace == "" {
		namespace = "ghostkey-system"
	}
	hosts := dashboard.DefaultAllowedHosts(namespace, dashboardServiceName)
	if dashboardAllowedHosts != "" {
		hosts = append(hosts, strings.Split(dashboardAllowedHosts, ",")...)
	}

	setupLog.Infow("Starting GhostKey dashboard", "namespace", namespace, "demoData", dashboardDemoData, "allowedHosts", hosts)

	if dashboardDemoData {
		exitOnError(setupLog, "dashboard failed", runDemoDashboard(hosts, log))
		setupLog.Infow("dashboard exited cleanly")
		_ = setupLog.Sync()
		return
	}

	gracefulShutdown := 30 * time.Second
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                  dashboardScheme,
		HealthProbeBindAddress:  dashboardProbeAddr,
		GracefulShutdownTimeout: &gracefulShutdown,
		// The dashboard exposes no metrics, and must not collide with
		// another component's default :8080.
		Metrics: metricsserver.Options{BindAddress: "0"},
	})
	exitOnError(setupLog, "unable to start manager", err)

	// The uncached API reader: a cached client would keep every Secret of
	// the cluster, values included, in memory. Real Secrets are only read
	// as metadata.
	store := dashboard.NewKubeStore(mgr.GetAPIReader(), dashboard.DefaultKubeOptions(namespace))
	srv, err := dashboard.NewServer(store, dashboard.Options{AllowedHosts: hosts, Log: log})
	exitOnError(setupLog, "unable to create dashboard server", err)

	exitOnError(setupLog, "unable to add dashboard server", mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		return dashboard.Serve(ctx, dashboardBindAddr, srv, log)
	})))
	exitOnError(setupLog, "unable to set up health check", mgr.AddHealthzCheck("healthz", healthz.Ping))
	exitOnError(setupLog, "unable to set up ready check", mgr.AddReadyzCheck("readyz", healthz.Ping))

	signalCtx := ctrl.SetupSignalHandler()
	startErr := mgr.Start(signalCtx)
	if signalCtx.Err() != nil {
		setupLog.Infow("shutdown signal received, draining")
	}
	exitOnError(setupLog, "problem running manager", startErr)
	flushCoverage()
	setupLog.Infow("dashboard exited cleanly")
	_ = setupLog.Sync()
}

// runDemoDashboard serves the demonstration data and the health probes,
// without a cluster, until a shutdown signal.
func runDemoDashboard(hosts []string, log *zap.SugaredLogger) error {
	srv, err := dashboard.NewServer(dashboard.NewDemoStore(), dashboard.Options{Demo: true, AllowedHosts: hosts, Log: log})
	if err != nil {
		return err
	}
	probes := http.NewServeMux()
	ok := func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }
	probes.HandleFunc("/healthz", ok)
	probes.HandleFunc("/readyz", ok)

	ctx, cancel := context.WithCancel(ctrl.SetupSignalHandler())
	defer cancel()
	errCh := make(chan error, 2)
	go func() { errCh <- dashboard.Serve(ctx, dashboardBindAddr, srv, log) }()
	go func() { errCh <- dashboard.Serve(ctx, dashboardProbeAddr, probes, log.Named("probes")) }()

	// The first server to stop (a listen error, or the signal) stops both.
	first := <-errCh
	cancel()
	second := <-errCh
	if ctx.Err() != nil && first == nil {
		log.Infow("shutdown signal received")
	}
	return errors.Join(first, second)
}

func exitOnError(log *zap.SugaredLogger, msg string, err error) {
	if err == nil {
		return
	}
	log.Errorw(msg, "error", err)
	_ = log.Sync()
	os.Exit(1)
}

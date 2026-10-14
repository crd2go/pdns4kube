package operator

import (
	"fmt"

	"github.com/josvazg/pdns4kube/internal/controller/record"
	"github.com/josvazg/pdns4kube/internal/controller/zone"
	"github.com/josvazg/pdns4kube/internal/pdns"
	v1 "github.com/josvazg/pdns4kube/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// createManager builds the controller manager. getConfig is the seam used
// to obtain the Kubernetes rest config.
func createManager(opConfig *operatorCfg, getConfig func() (*rest.Config, error)) (manager.Manager, error) {
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opConfig.zopts)))

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1.AddToScheme(scheme))

	cfg, err := getConfig()
	if err != nil {
		return nil, fmt.Errorf("get config: %w", err)
	}
	return ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: opConfig.metricsAddr},
		HealthProbeBindAddress: opConfig.proveAddr,
		LeaderElection:         opConfig.enableLeaderElection,
		LeaderElectionID:       "pdns4kube.pdns.example.io",
	})
}

func setupChecks(mgr manager.Manager) error {
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("setup health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("setup ready check: %w", err)
	}
	return nil
}

func registerReconcilers(mgr manager.Manager, opConfig *operatorCfg) error {
	if opConfig.pdnsAPIURL == "" {
		return fmt.Errorf("PDNS_API_URL is required")
	}
	if opConfig.pdnsAPIKey == "" {
		return fmt.Errorf("PDNS_API_KEY is required")
	}
	pdnsClient := pdns.NewClient(opConfig.pdnsAPIURL, opConfig.pdnsAPIKey, nil)
	reconciler, err := zone.NewPDNSZoneReconciler(DefaultPredicates(), pdnsClient)
	if err != nil {
		return fmt.Errorf("error creating DNSZone controller: %w", err)
	}
	if err := reconciler.SetupWithManager(mgr, controller.Options{}); err != nil {
		return fmt.Errorf("failed setup DNSZone controller: %w", err)
	}
	recordReconciler, err := record.NewPDNSRecordReconciler(DefaultPredicates(), pdnsClient)
	if err != nil {
		return fmt.Errorf("error creating DNSRecord controller: %w", err)
	}
	if err := recordReconciler.SetupWithManager(mgr, controller.Options{}); err != nil {
		return fmt.Errorf("failed setup DNSRecord controller: %w", err)
	}
	return nil
}

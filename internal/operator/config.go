package operator

import (
	"flag"
	"fmt"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

type operatorCfg struct {
	proveAddr            string
	metricsAddr          string
	enableLeaderElection bool
	pdnsAPIURL           string
	pdnsAPIKey           string
	zopts                zap.Options
}

type getCfgFunc func() (*rest.Config, error)

func parseFlagsConfig(args []string, getenv func(string) string) (*operatorCfg, error) {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	fs := flag.NewFlagSet("pdns4kube", flag.ContinueOnError)
	fs.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	fs.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	fs.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(fs)
	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("parse flags: %w", err)
	}
	return &operatorCfg{
		proveAddr:            probeAddr,
		metricsAddr:          metricsAddr,
		enableLeaderElection: enableLeaderElection,
		pdnsAPIURL:           getenv("PDNS_API_URL"),
		pdnsAPIKey:           getenv("PDNS_API_KEY"),
		zopts:                opts,
	}, nil
}

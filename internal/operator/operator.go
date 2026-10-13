// Copyright 2026 Jose Vazquez
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package operator contains the pdns4kube operator entrypoint logic,
// factored out of cmd/main.go so it can be unit tested.
package operator

import (
	"context"
	"flag"
	"fmt"
	"os"

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

	"github.com/josvazg/pdns4kube/internal/controller/record"
	"github.com/josvazg/pdns4kube/internal/controller/zone"
	"github.com/josvazg/pdns4kube/internal/pdns"
	v1 "github.com/josvazg/pdns4kube/v1"
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

// Run executes the operator with the given arguments and environment
// lookup. getenv is used for environment driven configuration; when nil
// it falls back to os.Getenv.
func Run(ctx context.Context, args []string, getenv func(string) string, getConfig getCfgFunc) error {
	return run(ctx, args, getenv, ctrl.GetConfig)
}

// run is the testable core of Run; getConfig is the seam used to obtain
// the Kubernetes rest config (production uses ctrl.GetConfig).
func run(ctx context.Context, args []string, getenv func(string) string, getConfig getCfgFunc) error {
	if getenv == nil {
		getenv = os.Getenv
	}
	opConfig, err := parseFlagsConfig(args, getenv)
	if err != nil {
		return fmt.Errorf("failed to parse config args: %w", err)
	}

	mgr, err := createManager(opConfig, getConfig)
	if err != nil {
		return fmt.Errorf("failed to create manager: %w", err)
	}

	if err := registerReconcilers(mgr, opConfig); err != nil {
		return fmt.Errorf("failed to register reconcilers: %w", err)
	}

	if err := setupChecks(mgr); err != nil {
		return fmt.Errorf("failed to setup operator checks: %w", err)
	}

	setupLog := ctrl.Log.WithName("setup")
	setupLog.Info("starting manager")
	return mgr.Start(ctx)
}

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
	reconciler, err := zone.NewPDNSZoneReconciler(zone.Predicates(), pdnsClient)
	if err != nil {
		return fmt.Errorf("error creating DNSZone controller: %w", err)
	}
	if err := reconciler.SetupWithManager(mgr, controller.Options{}); err != nil {
		return fmt.Errorf("failed setup DNSZone controller: %w", err)
	}
	recordReconciler, err := record.NewPDNSRecordReconciler(record.Predicates(), pdnsClient)
	if err != nil {
		return fmt.Errorf("error creating DNSRecord controller: %w", err)
	}
	if err := recordReconciler.SetupWithManager(mgr, controller.Options{}); err != nil {
		return fmt.Errorf("failed setup DNSRecord controller: %w", err)
	}
	return nil
}

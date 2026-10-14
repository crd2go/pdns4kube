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
	"fmt"
	"os"

	ctrl "sigs.k8s.io/controller-runtime"
)

// Run executes the operator with the given arguments and environment
// lookup. getenv is used for environment driven configuration; when nil
// it falls back to os.Getenv.
func Run(ctx context.Context, args []string, getenv func(string) string, getConfig getCfgFunc) error {
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

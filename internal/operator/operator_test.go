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

package operator_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/crd2go/pdns4kube/internal/operator"
	"k8s.io/client-go/rest"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		wantErr string
	}{
		{
			name:    "unknown flag",
			args:    []string{"-unknown"},
			wantErr: "parse flags",
		},
		{
			name:    "bad flag value",
			args:    []string{"-leader-elect=maybe"},
			wantErr: "parse flags",
		},
		{
			name:    "get config failure",
			wantErr: "get config",
		},
		{
			name:    "unknown flag in otherwise valid args",
			args:    []string{"-metrics-bind-address", ":9090", "-nope"},
			wantErr: "parse flags",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string { return tt.env[key] }
			// All test cases exit before the manager starts, so inject a
			// getConfig that always fails; flag parse errors take precedence
			// over it.
			getConfig := func() (*rest.Config, error) {
				return nil, errors.New("injected config failure")
			}
			err := operator.Run(context.Background(), tt.args, getenv, getConfig)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Run() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Run() error = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Run() error = %q, want error containing %q", err.Error(), tt.wantErr)
			}
		})
	}
}

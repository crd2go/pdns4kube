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

package record

import (
	"context"
	"fmt"

	constate "github.com/crd2go/constate"
	"github.com/josvazg/pdns4kube/internal/pdns"
	v1 "github.com/josvazg/pdns4kube/v1"
	crt "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type RecordHandler struct {
	constate.VersionDispatcher[v1.DNSRecord]
	// PDNSRecordClient performs rrset mutations against the PowerDNS API.
	PDNSRecordClient PDNSRecordClient
	client           client.Client
	predicates       []predicate.Predicate
	v0000015Handler  *V000015Handler
}

// PDNSRecordClient is the subset of the PowerDNS client used by the record
// handlers.
type PDNSRecordClient interface {
	ReplaceRRset(ctx context.Context, zoneID string, rrset pdns.RRset) error
	DeleteRRset(ctx context.Context, zoneID, name, rtype string) error
}

func NewRecordHandler(predicates []predicate.Predicate, pdnsClient PDNSRecordClient) (*RecordHandler, error) {
	v0000015Handler, err := NewV000015Handler(pdnsClient)
	if err != nil {
		return nil, fmt.Errorf("v0_0_15 handler: %w", err)
	}
	rh := &RecordHandler{
		predicates:       predicates,
		PDNSRecordClient: pdnsClient,
		v0000015Handler:  v0000015Handler,
	}
	rh.VersionDispatcher = *constate.NewVersionDispatcher(rh.HandlerSelector)
	return rh, nil
}

func (rh *RecordHandler) HandlerSelector(ctx context.Context, obj *v1.DNSRecord) (constate.StateHandler[v1.DNSRecord], error) {
	if obj.Spec.V0_0_15 != nil {
		return rh.v0000015Handler, nil
	}
	return nil, fmt.Errorf("no valid resource spec version specified")
}

func (rh *RecordHandler) For() (client.Object, builder.Predicates) {
	obj := &v1.DNSRecord{}
	return obj, builder.WithPredicates(rh.predicates...)
}

func (rh *RecordHandler) SetupWithManager(
	mgr crt.Manager,
	rec reconcile.Reconciler,
	defaultOptions controller.Options) error {
	rh.client = mgr.GetClient()
	return crt.NewControllerManagedBy(mgr).Named(
		"DNSRecord").For(rh.For()).WithOptions(defaultOptions).Complete(rec)
}

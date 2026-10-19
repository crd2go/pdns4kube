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

package zone

import (
	"context"
	"fmt"
	"log"

	"k8s.io/apimachinery/pkg/runtime"

	constate "github.com/crd2go/constate"
	crapi "github.com/crd2go/crapi"
	bases "github.com/crd2go/pdns4kube/config/crd/bases"
	"github.com/crd2go/pdns4kube/internal/pdns"
	v1 "github.com/crd2go/pdns4kube/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// crdVersion is the CRD version used for the DNSZone resource.
	crdVersion = "v1"
	// majorVersion is the pinned SDK major version handled by this handler.
	majorVersion = "v0_0_15"
)

// NewV000015Handler builds the crapi translator from the embedded CRD base
// and the generated v1 types registered in a runtime scheme, then returns a
// handler using it.
func NewV000015Handler(pdnsClient PDNSZoneClient) (*V000015Handler, error) {
	sch := runtime.NewScheme()
	if err := v1.AddToScheme(sch); err != nil {
		return nil, fmt.Errorf("register v1 types in scheme: %w", err)
	}
	crd, err := bases.CustomResourceDefinition()
	if err != nil {
		return nil, fmt.Errorf("parse embedded CRD: %w", err)
	}
	tr, err := crapi.NewTranslator(sch, crd, crdVersion, majorVersion)
	if err != nil {
		return nil, fmt.Errorf("create translator: %w", err)
	}
	return &V000015Handler{pdns: pdnsClient, translator: tr}, nil
}

type V000015Handler struct {
	constate.FallbackHandler[v1.DNSZone]
	// pdns performs zone mutations against the PowerDNS API.
	pdns PDNSZoneClient
	// translator converts CRs into pdns API objects.
	translator crapi.Translator
}

func (h *V000015Handler) For() (client.Object, builder.Predicates) {
	return nil, builder.Predicates{}
}

func (h *V000015Handler) HandleInitial(ctx context.Context, obj *v1.DNSZone) (constate.Result, error) {
	z, err := h.zoneFromEntry(obj)
	if err != nil {
		return constate.ErrorState(constate.StateInitial, err)
	}
	if err := h.pdns.CreateZone(ctx, z); err != nil {
		return constate.ErrorState(constate.StateInitial, fmt.Errorf("create zone: %w", err))
	}
	return constate.NextState(constate.StateCreated, "zone created in pdns")
}

func (h *V000015Handler) HandleCreated(ctx context.Context, obj *v1.DNSZone) (constate.Result, error) {
	return h.upsert(ctx, obj)
}

func (h *V000015Handler) HandleUpdated(ctx context.Context, obj *v1.DNSZone) (constate.Result, error) {
	return h.upsert(ctx, obj)
}

func (h *V000015Handler) HandleDeletionRequested(ctx context.Context, obj *v1.DNSZone) (constate.Result, error) {
	if obj.Spec.V0_0_15 == nil || obj.Spec.V0_0_15.Entry == nil {
		err := fmt.Errorf("v0_0_15 spec entry is required")
		return constate.ErrorState(constate.StateDeletionRequested, err)
	}
	if err := h.pdns.DeleteZone(ctx, obj.Spec.V0_0_15.Entry.Name); err != nil {
		return constate.ErrorState(constate.StateDeletionRequested, fmt.Errorf("delete zone: %w", err))
	}
	return constate.NextState(constate.StateDeleted, "zone deleted from pdns")
}

func (h *V000015Handler) upsert(ctx context.Context, obj *v1.DNSZone) (constate.Result, error) {
	z, err := h.zoneFromEntry(obj)
	if err != nil {
		return constate.ErrorState(constate.StateCreated, err)
	}
	log.Printf("updatign with %v", z)
	if err := h.pdns.UpdateZone(ctx, z.Name, z); err != nil {
		return constate.ErrorState(constate.StateCreated, fmt.Errorf("update zone: %w", err))
	}
	return constate.NextState(constate.StateUpdated, "zone updated in pdns")
}

// zoneFromEntry converts the v0_0_15 spec entry into a pdns.Zone request
// using the handler's stored crapi translator.
func (h *V000015Handler) zoneFromEntry(obj *v1.DNSZone) (pdns.Zone, error) {
	if obj.Spec.V0_0_15 == nil || obj.Spec.V0_0_15.Entry == nil {
		return pdns.Zone{}, fmt.Errorf("v0_0_15 spec entry is required")
	}
	var z pdns.Zone
	if err := h.translator.ToAPI(&z, obj); err != nil {
		return pdns.Zone{}, fmt.Errorf("translate v0_0_15 entry: %w", err)
	}
	return z, nil
}

// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
)

// redesignMuxAddrs — backend address map with compute/storage public + internal
// keys so both the public (Image/MachineType read+CRUD) and the internal
// (InternalImage / InternalMachineType) handlers register.
func redesignMuxAddrs() map[string]string {
	return map[string]string{
		"vpc":             "127.0.0.1:1",
		"vpcInternal":     "127.0.0.1:1",
		"compute":         "127.0.0.1:1",
		"computeInternal": "127.0.0.1:1",
		"iam":             "127.0.0.1:1",
		"iamInternal":     "127.0.0.1:1",
		"storage":         "127.0.0.1:1",
		"storageInternal": "127.0.0.1:1",
	}
}

// TestRedesign_PublicRoutesRegistered — new public REST routes (storage Image
// CRUD+ListOperations, compute MachineType read, vpc Network CIDR :verbs, iam
// AccessBinding unified List + soft-revoke) must be served on the EXTERNAL
// listener (route found — NOT a route-level 404). Unreachable backend 127.0.0.1:1
// yields a downstream gRPC error, never a 404.
func TestRedesign_PublicRoutesRegistered(t *testing.T) {
	h, err := NewMux(context.Background(), redesignMuxAddrs(), nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}

	publicRoutes := []struct{ method, path string }{
		// storage ImageService
		{"GET", "/storage/v1/images"},
		{"POST", "/storage/v1/images"},
		{"GET", "/storage/v1/images/img-1"},
		{"PATCH", "/storage/v1/images/img-1"},
		{"DELETE", "/storage/v1/images/img-1"},
		{"GET", "/storage/v1/images/img-1/operations"},
		// compute MachineTypeService (read-only sizing catalog)
		{"GET", "/compute/v1/machineTypes"},
		{"GET", "/compute/v1/machineTypes/mt-1"},
		// vpc NetworkService :verb supernet growth/shrink
		{"POST", "/vpc/v1/networks/net-1:add-cidr-blocks"},
		{"POST", "/vpc/v1/networks/net-1:remove-cidr-blocks"},
		// iam AccessBindingService unified List + soft-revoke
		{"GET", "/iam/v1/accessBindings"},
		{"POST", "/iam/v1/accessBindings/acb-1:revoke"},
	}
	for _, tc := range publicRoutes {

		t.Run("EXT "+tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code == http.StatusNotFound {
				t.Errorf("redesign public %s %s on EXTERNAL listener: got 404 — public route not registered",
					tc.method, tc.path)
			}
		})
	}
}

// TestRedesign_InternalRoutes_InternalListenerServes — the admin/internal routes
// (compute InternalMachineType CRUD on /compute/v1/internal/machineTypes; storage
// InternalImageService.GetInternal default unbound-route) must be served on the
// INTERNAL listener (route found — NOT a route-level 404).
func TestRedesign_InternalRoutes_InternalListenerServes(t *testing.T) {
	h, err := NewMux(context.Background(), redesignMuxAddrs(), nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}

	internalRoutes := []struct{ method, path string }{
		{"POST", "/compute/v1/internal/machineTypes"},
		{"PATCH", "/compute/v1/internal/machineTypes/mt-1"},
		{"DELETE", "/compute/v1/internal/machineTypes/mt-1"},
		{"POST", "/kacho.cloud.storage.v1.InternalImageService/GetInternal"},
	}
	for _, tc := range internalRoutes {

		t.Run("INT "+tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req = req.WithContext(listenerorigin.WithInternal(req.Context()))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code == http.StatusNotFound {
				t.Errorf("redesign Internal %s %s on INTERNAL listener: got 404 — Internal handler not registered (admin broken)",
					tc.method, tc.path)
			}
		})
	}
}

// TestRedesign_InternalRoutes_ExternalListenerRejected — the same admin/internal
// routes on the EXTERNAL TLS listener MUST return 404 (existence-hiding): admin
// MachineType CRUD and the Image infra-projection never surface on the external
// endpoint (ban #6). isInternalPath catches both the `/internal/` path segment and
// the Internal*Service default unbound-route (mirrors the gRPC HasInternalSuffix block).
func TestRedesign_InternalRoutes_ExternalListenerRejected(t *testing.T) {
	h, err := NewMux(context.Background(), redesignMuxAddrs(), nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}

	internalRoutes := []struct{ method, path string }{
		{"POST", "/compute/v1/internal/machineTypes"},
		{"PATCH", "/compute/v1/internal/machineTypes/mt-1"},
		{"DELETE", "/compute/v1/internal/machineTypes/mt-1"},
		{"POST", "/kacho.cloud.storage.v1.InternalImageService/GetInternal"},
	}
	for _, tc := range internalRoutes {

		t.Run("EXT "+tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Errorf("redesign Internal %s %s on EXTERNAL listener: got %d, want 404 (CRITICAL: internal path exposed on external endpoint)",
					tc.method, tc.path, rec.Code)
			}
		})
	}
}

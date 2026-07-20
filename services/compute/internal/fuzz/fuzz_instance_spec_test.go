// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Continuous fuzzing: Compute Instance create-spec validation (COMP-1 redesign).
//
// Instance create accepts a nested spec (instance_kind, machine_type_id,
// boot_source, network_interface_specs, secondary_volume_specs, vm_spec/
// container_spec). This target drives the SAME production path the RPC runs on
// hostile input:
//
//	protojson → computev1.CreateInstanceRequest → handler.CreateReqFromProto →
//	service.ValidateCreateInstanceReq
//
// Invariants asserted:
//   - malformed input must NOT panic anywhere on that path (proto decode,
//     conversion, synchronous field validation);
//   - a validation rejection must be a stable, client-caused gRPC status — either
//     InvalidArgument (format/grammar) or FailedPrecondition (net/unreachable
//     runbook guards) — never a bare error / Unknown / Internal (which would
//     surface an internal fault to clients as a client-caused one).
package fuzz_test

import (
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"

	"github.com/PRO-Robotech/kacho/services/compute/internal/handler"
	"github.com/PRO-Robotech/kacho/services/compute/internal/service"
)

var instSpecTestSink error

func FuzzInstanceSpecValidate(f *testing.F) {
	seeds := []string{
		`{"name":"vm1","zoneId":"ru-central1-a"}`,
		`{}`,
		``,
		`{"name":""}`,
		`{"name":"` + strings.Repeat("a", 100) + `"}`,
		`{"name":"vm1","zoneId":"empty"}`,
		// kind set but sizing/boot missing (F1/F2 rejections).
		`{"instanceKind":"VM","name":"vm1","zoneId":"ru-central1-a"}`,
		// bare bootSource id without a tag/digest (F3 grammar rejection).
		`{"instanceKind":"VM","machineTypeId":"mt-std2","bootSource":{"type":"storage.image","id":"img-x"}}`,
		// kind/spec mismatch (VM + containerSpec, F1 XOR).
		`{"instanceKind":"VM","machineTypeId":"mt-std2","bootSource":{"type":"storage.image","id":"img-x:22.04"},"containerSpec":{}}`,
		// many secondary-volume specs (structural loop).
		`{"secondaryVolumeSpecs":[` + strings.Repeat(`{"sizeGib":10},`, 100) + `{}]}`,
		// SQL-injection in name.
		`{"name":"'; DROP TABLE instances; --"}`,
		`{"metadata":{"key":"value"}}`,
		// well-formed happy VM spec — exercises the accept path (err == nil).
		`{"projectId":"prj1","name":"vm1","zoneId":"ru-central1-a","instanceKind":"VM",` +
			`"machineTypeId":"mt-std2","bootSource":{"type":"storage.image","id":"img-x:22.04"},` +
			`"sshPublicKeys":["ssh-ed25519 AAAA user@h"],` +
			`"networkInterfaceSpecs":[{"subnetId":"sub-a","securityGroupIds":["scg-a"]}],"vmSpec":{}}`,
		// well-formed happy CONTAINER spec.
		`{"projectId":"prj1","name":"job1","zoneId":"ru-central1-a","instanceKind":"CONTAINER",` +
			`"machineTypeId":"mt-std2","bootSource":{"type":"registry.image","id":"repo/app:1.0"},` +
			`"networkInterfaceSpecs":[{"subnetId":"sub-a"}],"containerSpec":{}}`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// Real wire decode: malformed JSON is an expected outcome (skip), a PANIC
		// is the bug we hunt. Discard decode errors rather than fail — the fuzzer
		// explores the decoder itself for crashes.
		req := &computev1.CreateInstanceRequest{}
		if err := protojson.Unmarshal([]byte(input), req); err != nil {
			return
		}

		// Same conversion + synchronous validation the RPC runs.
		cr := handler.CreateReqFromProto(req)
		err := service.ValidateCreateInstanceReq(cr)
		instSpecTestSink = err
		if err == nil {
			return
		}
		// A rejection must be a stable, client-caused gRPC status — not Unknown/
		// Internal or a non-status error (which would surface to clients as INTERNAL).
		switch code := status.Code(err); code {
		case codes.InvalidArgument, codes.FailedPrecondition:
			// ok — stable, client-caused rejection.
		default:
			t.Fatalf("validation rejection must be InvalidArgument or FailedPrecondition, got %s: %v (input=%q)", code, err, input)
		}
	})
}

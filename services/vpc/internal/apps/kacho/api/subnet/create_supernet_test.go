// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subnet

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestValidateSubnetWithinSupernet — behaviour-level lock редизайна VPC-1 F7:
// Subnet CIDR обязан быть подмножеством объявленного супернета сети; иначе —
// INVALID_ARGUMENT с точным текстом (VPC-1-29 happy / VPC-1-30 reject / VPC-1-34
// add-block / v6). Пустой супернет (legacy) → skip (back-compat).
func TestValidateSubnetWithinSupernet(t *testing.T) {
	tests := []struct {
		name              string
		netV4, netV6      []string
		subV4, subV6      []string
		wantErr           bool
		wantMsgSubstr     string // точный контракт-текст, часть которого проверяем
		wantInvalidArgErr bool
	}{
		{
			name:  "VPC-1-29 primary within supernet → ok",
			netV4: []string{"10.20.0.0/16"}, subV4: []string{"10.20.0.0/24"},
			wantErr: false,
		},
		{
			name:  "VPC-1-30 primary outside supernet → InvalidArgument exact text",
			netV4: []string{"10.20.0.0/16"}, subV4: []string{"192.168.0.0/24"},
			wantErr: true, wantInvalidArgErr: true,
			wantMsgSubstr: "subnet CIDR 192.168.0.0/24 is not within any network CIDR block",
		},
		{
			name:  "exact-match block (subnet == supernet block) → ok",
			netV4: []string{"10.20.0.0/24"}, subV4: []string{"10.20.0.0/24"},
			wantErr: false,
		},
		{
			name:  "multiple supernet blocks, matches second → ok",
			netV4: []string{"10.10.0.0/16", "10.20.0.0/16"}, subV4: []string{"10.20.5.0/24"},
			wantErr: false,
		},
		{
			name:  "VPC-1-34 additional block outside supernet → reject",
			netV4: []string{"10.20.0.0/16"}, subV4: []string{"10.20.0.0/24", "10.99.0.0/24"},
			wantErr: true, wantInvalidArgErr: true,
			wantMsgSubstr: "subnet CIDR 10.99.0.0/24 is not within any network CIDR block",
		},
		{
			name:  "empty supernet (legacy network) → skip, no error",
			netV4: nil, subV4: []string{"192.168.0.0/24"},
			wantErr: false,
		},
		{
			name:  "v6 within supernet → ok",
			netV6: []string{"fd00:20::/48"}, subV6: []string{"fd00:20::/64"},
			wantErr: false,
		},
		{
			name:  "v6 outside supernet → reject",
			netV6: []string{"fd00:20::/48"}, subV6: []string{"fd00:99::/64"},
			wantErr: true, wantInvalidArgErr: true,
			wantMsgSubstr: "subnet CIDR fd00:99::/64 is not within any network CIDR block",
		},
		{
			name:  "supernet-broader-prefix must NOT accept a shorter subnet prefix (subnet /8 vs supernet /16)",
			netV4: []string{"10.20.0.0/16"}, subV4: []string{"10.0.0.0/8"},
			wantErr: true, wantInvalidArgErr: true,
			wantMsgSubstr: "subnet CIDR 10.0.0.0/8 is not within any network CIDR block",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSubnetWithinSupernet(tc.netV4, tc.netV6, tc.subV4, tc.subV6)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.wantInvalidArgErr && status.Code(err) != codes.InvalidArgument {
					t.Fatalf("code = %v; want InvalidArgument", status.Code(err))
				}
				if tc.wantMsgSubstr != "" && status.Convert(err).Message() != tc.wantMsgSubstr {
					t.Fatalf("msg = %q; want exactly %q", status.Convert(err).Message(), tc.wantMsgSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected nil, got %v", err)
			}
		})
	}
}

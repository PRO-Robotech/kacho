// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestValidateNetworkSupernet — VPC-1-09/F2: format-валидация объявленного
// супернета Network. Валидный canonical CIDR → ok; невалидная маска / host-bits /
// неверное семейство → InvalidArgument "invalid CIDR block '<X>'".
func TestValidateNetworkSupernet(t *testing.T) {
	tests := []struct {
		name    string
		v4, v6  []string
		wantErr bool
		wantMsg string
	}{
		{name: "empty → ok", wantErr: false},
		{name: "valid v4 supernet → ok", v4: []string{"10.20.0.0/16"}, wantErr: false},
		{name: "valid v6 supernet → ok", v6: []string{"fd00:20::/48"}, wantErr: false},
		{name: "valid v4+v6 → ok", v4: []string{"10.20.0.0/16"}, v6: []string{"fd00:20::/48"}, wantErr: false},
		{
			name: "VPC-1-09 invalid mask /33 → reject exact text",
			v4:   []string{"10.20.0.0/33"}, wantErr: true,
			wantMsg: "invalid CIDR block '10.20.0.0/33'",
		},
		{
			name: "non-zero host bits → reject",
			v4:   []string{"10.20.0.5/16"}, wantErr: true,
			wantMsg: "invalid CIDR block '10.20.0.5/16'",
		},
		{
			name: "garbage → reject",
			v4:   []string{"not-a-cidr"}, wantErr: true,
			wantMsg: "invalid CIDR block 'not-a-cidr'",
		},
		{
			name: "v6 value in v4 field → reject (family mismatch)",
			v4:   []string{"fd00:20::/48"}, wantErr: true,
			wantMsg: "invalid CIDR block 'fd00:20::/48'",
		},
		{
			name: "v4 value in v6 field → reject (family mismatch)",
			v6:   []string{"10.20.0.0/16"}, wantErr: true,
			wantMsg: "invalid CIDR block '10.20.0.0/16'",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateNetworkSupernet(tc.v4, tc.v6)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if status.Code(err) != codes.InvalidArgument {
					t.Fatalf("code = %v; want InvalidArgument", status.Code(err))
				}
				if status.Convert(err).Message() != tc.wantMsg {
					t.Fatalf("msg = %q; want %q", status.Convert(err).Message(), tc.wantMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected nil, got %v", err)
			}
		})
	}
}

// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

import (
	"fmt"
	"net/netip"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"

	// Blank-import регистрирует трансферы Network/time через init().
	_ "github.com/PRO-Robotech/kacho/services/vpc/internal/dto/toproto"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// validateNetworkSupernet — sync-валидация объявленного супернета (VPC-1-09/F2):
// каждый блок ipv4CidrBlocks/ipv6CidrBlocks обязан быть валидным CIDR с нулевыми
// host-битами (canonical network form). Нарушение → InvalidArgument c
// редизайн-тоном "invalid CIDR block '<X>'" ДО создания Operation (format-класс).
// Семейство блока (v4/v6) обязано совпадать с полем, в котором он объявлен.
func validateNetworkSupernet(v4, v6 []string) error {
	for _, b := range v4 {
		if err := validateSupernetBlock(b, true); err != nil {
			return err
		}
	}
	for _, b := range v6 {
		if err := validateSupernetBlock(b, false); err != nil {
			return err
		}
	}
	return nil
}

func validateSupernetBlock(block string, wantV4 bool) error {
	p, err := netip.ParsePrefix(block)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "invalid CIDR block '%s'", block)
	}
	if p.Masked() != p {
		return status.Errorf(codes.InvalidArgument, "invalid CIDR block '%s'", block)
	}
	if p.Addr().Is4() != wantV4 || p.Addr().Is4In6() {
		return status.Errorf(codes.InvalidArgument, "invalid CIDR block '%s'", block)
	}
	return nil
}

// mergeCidrBlocks добавляет new-блоки к existing, дедуплицируя по canonical-строке
// (повторное добавление уже-объявленного блока идемпотентно, не дубль). Порядок
// сохраняется: existing, затем впервые встреченные new.
func mergeCidrBlocks(existing, add []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(add))
	out := make([]string, 0, len(existing)+len(add))
	for _, b := range existing {
		if _, ok := seen[b]; ok {
			continue
		}
		seen[b] = struct{}{}
		out = append(out, b)
	}
	for _, b := range add {
		if _, ok := seen[b]; ok {
			continue
		}
		seen[b] = struct{}{}
		out = append(out, b)
	}
	return out
}

// subtractCidrBlocks возвращает existing без блоков из remove (set-difference по
// canonical-строке). Блоки remove, которых нет в existing, игнорируются (no-op).
func subtractCidrBlocks(existing, remove []string) []string {
	drop := make(map[string]struct{}, len(remove))
	for _, b := range remove {
		drop[b] = struct{}{}
	}
	out := make([]string, 0, len(existing))
	for _, b := range existing {
		if _, ok := drop[b]; ok {
			continue
		}
		out = append(out, b)
	}
	return out
}

// marshalNetworkRecord конвертирует repo-entity Network в *anypb.Any через
// DTO-реестр. Используется worker'ами Create/Update/Move для упаковки результата
// в Operation.response.
func marshalNetworkRecord(rec *kachorepo.NetworkRecord) (*anypb.Any, error) {
	var dst *vpcv1.Network
	if err := dto.Transfer(dto.FromTo(*rec, &dst)); err != nil {
		return nil, fmt.Errorf("dto.Transfer Network: %w", err)
	}
	return anypb.New(dst)
}

// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package cidrgroup

import (
	"fmt"
	"net/netip"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"

	// Blank-import регистрирует трансферы CidrGroup/time через init().
	_ "github.com/PRO-Robotech/kacho/services/vpc/internal/dto/toproto"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// normalizeCidrBlocks — проверка формата членов набора И приведение их к
// канонической записи, одной функцией.
//
// Приведение здесь не украшение: дедупликация состава идёт по значению префикса
// (первичный ключ дочерней таблицы объявлен на типе `cidr`), а вызывающий
// вправе прислать то же значение в другом написании (`2001:0db8::/32` против
// `2001:db8::/32`). Без приведения повторное добавление одного и того же
// префикса читалось бы как добавление нового ровно до попадания в базу, и
// потолок считался бы по числу, которого в наборе нет.
//
// Семейство блока обязано совпадать с полем, в котором он объявлен: смешанный
// набор отвергается НА ВХОДЕ, а не разбирается позже.
func normalizeCidrBlocks(field string, blocks []string, wantV4 bool) ([]string, error) {
	if len(blocks) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(blocks))
	out := make([]string, 0, len(blocks))
	for i, b := range blocks {
		p, err := netip.ParsePrefix(b)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid CIDR block '%s'", b)
		}
		if p.Masked() != p {
			return nil, status.Errorf(codes.InvalidArgument, "invalid CIDR block '%s'", b)
		}
		if p.Addr().Is4() != wantV4 || p.Addr().Is4In6() {
			// Имя поля с индексом — вызывающий правит СВОЙ ввод, а не гадает,
			// какой из присланных блоков не того семейства.
			return nil, status.Errorf(codes.InvalidArgument,
				"invalid CIDR block '%s' in %s[%d]", b, field, i)
		}
		canonical := p.String()
		if _, dup := seen[canonical]; dup {
			continue
		}
		seen[canonical] = struct{}{}
		out = append(out, canonical)
	}
	return out, nil
}

// validateCidrGroupCardinality — потолок состава НА СЕМЕЙСТВО, синхронно.
//
// Применяется ТОЛЬКО на путях РОСТА (Create / :add-cidr-blocks): это граница
// стоимости ОДНОГО запроса. Накопленный между вызовами состав ограничивает
// конструкция базы — условный инкремент счётчика под блокировкой строки плюс
// CHECK, — потому что синхронная проверка ограничивает один запрос и ничего не
// знает о том, что было прислано до него.
//
// :remove-cidr-blocks потолком НЕ гейтится: сужение обязано проходить всегда.
func validateCidrGroupCardinality(v4, v6 []string) error {
	if len(v4) > domain.MaxCidrGroupBlocks || len(v6) > domain.MaxCidrGroupBlocks {
		return status.Errorf(codes.InvalidArgument,
			"too many CIDR blocks (max %d per family)", domain.MaxCidrGroupBlocks)
	}
	return nil
}

// marshalCidrGroupRecord — repo-entity → *anypb.Any через DTO-реестр (тело
// операции).
func marshalCidrGroupRecord(rec *kachorepo.CidrGroupRecord) (*anypb.Any, error) {
	var dst *vpcv1.CidrGroup
	if err := dto.Transfer(dto.FromTo(*rec, &dst)); err != nil {
		return nil, fmt.Errorf("dto.Transfer CidrGroup: %w", err)
	}
	return anypb.New(dst)
}

// cidrGroupToPb — repo-entity → proto через тот же DTO-реестр (ответ чтения).
func cidrGroupToPb(rec *kachorepo.CidrGroupRecord) (*vpcv1.CidrGroup, error) {
	var dst *vpcv1.CidrGroup
	if err := dto.Transfer(dto.FromTo(*rec, &dst)); err != nil {
		return nil, status.Error(codes.Internal, "dto.Transfer CidrGroup failed")
	}
	return dst, nil
}

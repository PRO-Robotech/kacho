// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// quotashapehome_injection_test.go — доказательство того, что проверка СПОСОБНА
// упасть и способна смолчать.
//
// Формы подаются НАСТОЯЩИЕ — те, что лежат в дереве, — и каждая ось несёт
// законного близнеца: без него «молчит» неотличимо от «не различает ничего».
//
// Ось места однофактна by construction: тело контракта одно и то же побайтово,
// различается ровно путь. Иначе «граница проходит по МЕСТУ объявления» осталась
// бы заявлением документа, а не свойством кода.

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Форма, РАДИ КОТОРОЙ проверка заведена: объявление службы чтения, как оно
// стояло в пакете общей формы ответа до переезда (kacho#2362, решение `Д9`).
// Координата историческая и в дереве более не резолвится — это и есть тот мир,
// который проверка обязана называть находкой.
const injIdentityQuotaService = `syntax = "proto3";

package kacho.cloud.quota.v1;

// IdentityQuotaService — the ceilings carried by the CALLER THEMSELVES.
service IdentityQuotaService {
  rpc List (ListIdentityQuotasRequest) returns (ListIdentityQuotasResponse) {
    option (google.api.http) = { get: "/iam/v1/quotas" };
  }
}
`

// Настоящая форма пакета ФОРМЫ без службы — из
// `proto/kacho/cloud/quota/v1/quota.proto`.
const injQuotaShapeOnly = `syntax = "proto3";

package kacho.cloud.quota.v1;

// WHY THIS MESSAGE IS SHARED AND THE SERVICE IS NOT. The value lives in iam and
// the counting lives with the owner of the resource type.
message Quota {
  string kind = 1;
  int64 limit = 2;
}
`

// --- ось 1: место объявления. Один изменённый факт — путь ------------------

func TestQSH_ServiceInsideTheShapePackageIsAFinding(t *testing.T) {
	t.Parallel()

	const rel = "proto/kacho/cloud/quota/v1/identity_quota_service.proto"
	require.True(t, InQuotaShapePackage(rel),
		"путь пакета формы обязан узнаваться: иначе проверка осмотрит ноль файлов и смолчит")
	require.Contains(t, ServicesDeclaredIn(injIdentityQuotaService), "IdentityQuotaService",
		"объявление службы обязано узнаваться НАСТОЯЩЕЙ формой из дерева")
}

func TestQSH_TheSameServiceInItsOwnPackageIsSilent(t *testing.T) {
	t.Parallel()

	// ЗАКОННЫЙ БЛИЗНЕЦ: тело побайтово то же, изменён ровно один факт — каталог.
	// Так объявляют свою службу пять владельцев платформы, и находкой это не является.
	const rel = "proto/kaname/cloud/iam/v1/identity_quota_service.proto"
	require.False(t, InQuotaShapePackage(rel),
		"служба, объявленная в СВОЁМ контракте, находкой не является — иначе проверка "+
			"запрещала бы ровно то, ради чего она заведена")
}

func TestQSH_FiveOwnersDeclareOutsideTheShapePackage(t *testing.T) {
	t.Parallel()

	// Положительный близнец из дерева: у владельцев платформы служба лежит
	// в их собственном каталоге.
	for _, rel := range []string{
		"proto/kacho/cloud/vpc/v1/quota_service.proto",
		"proto/kacho/cloud/compute/v1/quota_service.proto",
		"proto/kacho/cloud/storage/v1/quota_service.proto",
		"proto/kacho/cloud/registry/v1/quota_service.proto",
		"proto/kacho/cloud/loadbalancer/v1/quota_service.proto",
	} {
		require.Falsef(t, InQuotaShapePackage(rel),
			"%s — свой каталог владельца, а не пакет формы", rel)
	}
}

// --- ось 2: разбор судит ОБЪЯВЛЕНИЕ, а не слово ----------------------------

func TestQSH_ShapePackageWithoutAServiceIsSilent(t *testing.T) {
	t.Parallel()

	// Пакет формы, объявляющий только сообщение, — законное состояние и цель
	// этой проверки. Слово `service` в его прозе стоит дважды.
	require.Empty(t, ServicesDeclaredIn(injQuotaShapeOnly),
		"проза о службе объявлением службы не является: предикат по подстроке краснел бы "+
			"на собственном объяснении проверки")
}

func TestQSH_ProseMentioningAServiceIsNotADeclaration(t *testing.T) {
	t.Parallel()

	const prose = `// Обслуживает её служба доступа: service IdentityQuotaService объявлен рядом.
//   service NotADeclaration {
message Quota { string kind = 1; }
`
	require.Empty(t, ServicesDeclaredIn(prose),
		"объявление судится по НАЧАЛУ строки: имя службы встречается в комментариях "+
			"этих же файлов десятками раз")
}

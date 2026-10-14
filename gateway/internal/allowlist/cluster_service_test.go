// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package allowlist_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/allowlist"
)

// clusterServicePrefix — полный путь публичного близнеца службы администраторов
// кластера (kaname#661, kacho#3093).
const clusterServicePrefix = "/kaname.cloud.iam.v1.ClusterService/"

// internalClusterServiceMethods — внутренний близнец той же службы. Пути
// выписаны поимённо намеренно: проба обязана краснеть, если Internal-глагол
// окажется в перечне, даже когда дескрипторы его не несут.
var internalClusterServiceMethods = []string{
	"/kaname.cloud.iam.v1.InternalClusterService/Get",
	"/kaname.cloud.iam.v1.InternalClusterService/ListAdmins",
	"/kaname.cloud.iam.v1.InternalClusterService/GrantAdmin",
	"/kaname.cloud.iam.v1.InternalClusterService/RevokeAdmin",
}

// TestAllowlist_ClusterServiceIsPublic — четыре глагола публичного близнеца
// ClusterService проходят директора края, внутренний близнец — нет.
//
// Положительная сторона берёт перечень глаголов из слинкованных дескрипторов:
// их ровно четыре (Get, ListAdmins, GrantAdmin, RevokeAdmin) — меньше значит,
// что пин модуля службы не несёт kaname#661 и проба ничего не прочла; больше —
// контракт вырос, и новый глагол обязан получить своё решение, а не проехать
// молча под этим заголовком. Отрицательная сторона — запрет #6: ни один глагол
// InternalClusterService не открыт наружу и каждый ловится HasInternalSuffix.
func TestAllowlist_ClusterServiceIsPublic(t *testing.T) {
	surface := readDescriptorSurface(t)
	var declared []string
	for m := range surface.public {
		if strings.HasPrefix(m, clusterServicePrefix) {
			declared = append(declared, m)
		}
	}
	for m := range surface.internal {
		if strings.HasPrefix(m, clusterServicePrefix) {
			t.Errorf("%s опознан как Internal — публичный близнец ClusterService публичный", m)
		}
	}
	sort.Strings(declared)
	t.Logf("перепись: глаголов ClusterService в дескрипторах %d", len(declared))
	if len(declared) != 4 {
		t.Fatalf("глаголов ClusterService в дескрипторах %d, ожидалось 4 "+
			"(Get, ListAdmins, GrantAdmin, RevokeAdmin): %v", len(declared), declared)
	}
	for _, m := range declared {
		if !allowlist.IsAllowed(m) {
			t.Errorf("%s не в AllowedMethods: директор края ответит NotFound, "+
				"неотличимым от скрытой admin-поверхности", m)
		}
		if allowlist.HasInternalSuffix(m) {
			t.Errorf("%s ловится HasInternalSuffix — публичный глагол был бы срезан запретом #6", m)
		}
	}
	for _, m := range internalClusterServiceMethods {
		if allowlist.IsAllowed(m) {
			t.Errorf("%s в AllowedMethods — внутренний близнец открыт наружу (запрет #6)", m)
		}
		if !allowlist.HasInternalSuffix(m) {
			t.Errorf("%s не ловится HasInternalSuffix — внутренний близнец не срезан запретом #6", m)
		}
	}
}

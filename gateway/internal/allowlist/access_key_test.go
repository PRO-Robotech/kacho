// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package allowlist_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/allowlist"
)

// accessKeyServicePrefix — полный путь службы ключей доступа (Ф7, kacho#2718).
const accessKeyServicePrefix = "/kaname.cloud.iam.v1.AccessKeyService/"

// TestAllowlist_AccessKeyServiceIsPublic — шесть глаголов службы ключей доступа
// проходят директора края, и перечень глаголов берётся из дескрипторов, а не
// выписывается здесь руками.
//
// Две стороны одного утверждения. Положительная: каждый глагол службы, который
// объявлен в слинкованных дескрипторах, стоит в AllowedMethods и не ловится
// HasInternalSuffix — служба публичная, её пути `/iam/v1/users/{user_id}/accessKeys…`
// и `/iam/v1/accessKeys:…`. Предпосылка: дескрипторов службы ровно шесть — меньше
// значит, что пин модуля службы не несёт Ф7 и проба ничего не прочла; больше —
// контракт вырос, и новый глагол обязан получить своё решение, а не проехать
// молча под этим заголовком.
func TestAllowlist_AccessKeyServiceIsPublic(t *testing.T) {
	surface := readDescriptorSurface(t)
	var declared []string
	for m := range surface.public {
		if strings.HasPrefix(m, accessKeyServicePrefix) {
			declared = append(declared, m)
		}
	}
	for m := range surface.internal {
		if strings.HasPrefix(m, accessKeyServicePrefix) {
			t.Errorf("%s опознан как Internal — служба ключей доступа публичная", m)
		}
	}
	sort.Strings(declared)
	t.Logf("перепись: глаголов AccessKeyService в дескрипторах %d", len(declared))
	if len(declared) != 6 {
		t.Fatalf("глаголов AccessKeyService в дескрипторах %d, ожидалось 6 "+
			"(BeginRegistration, FinishRegistration, List, Revoke, BeginAssertion, "+
			"FinishAssertion): %v", len(declared), declared)
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
}

// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/fgaregister"
)

// Test_RegisterApplier_ForwardsTombstoneOnUnregister — снятие регистрации обязано
// нести source_version как TOMBSTONE.
//
// Обе стороны ребра уже версионированы на register-пути: emit штампует
// `jsonb_set(payload,'{source_version}', to_jsonb(now()))` внутри writer-TX, а
// applier прокидывает его в RegisterResource. iam удаляет строку зеркала под
// `WHERE source_version <= $tombstone`, поэтому unregister БЕЗ версии ('-infinity')
// не матчит ни одну версионированную строку — зеркало переживает удаление ресурса, а
// level-triggered реконсайлер iam продолжает ре-материализовать его tuple'ы
// (over-grant на удалённый объект). Инвариант: снятие регистрации удаляет строку
// зеркала целиком и никогда не гейтится. Паритет с compute/nlb, которые версию на
// unregister шлют.
func Test_RegisterApplier_ForwardsTombstoneOnUnregister(t *testing.T) {
	f := &fakeIAMRegisterClient{}
	apply := NewIAMRegisterApplier(f)

	tombstone := time.Now().UTC().Truncate(time.Microsecond)
	p := fgaregister.Payload{
		Tuple:         fgaregister.ProjectHierarchy("prj-P", "vpc_network", "net-1"),
		SourceVersion: tombstone,
	}
	require.NoError(t, apply(context.Background(), fgaregister.EventUnregister, p))

	require.Len(t, f.unregCalls, 1)
	req := f.unregCalls[0]
	require.NotNil(t, req.GetSourceVersion(), "unregister must carry the tombstone version")
	assert.True(t, req.GetSourceVersion().AsTime().Equal(tombstone),
		"tombstone must be the DB stamp of the unregister row, got %s", req.GetSourceVersion().AsTime())
}

// Test_RegisterApplier_UnregisterZeroSourceVersion_ForwardsNil — legacy-строка без
// stamp'а по-прежнему шлёт nil (iam: '-infinity'), т.е. прежнее безусловное поведение
// для пары legacy-register/legacy-unregister сохраняется.
func Test_RegisterApplier_UnregisterZeroSourceVersion_ForwardsNil(t *testing.T) {
	f := &fakeIAMRegisterClient{}
	apply := NewIAMRegisterApplier(f)

	p := fgaregister.Payload{Tuple: fgaregister.ProjectHierarchy("prj-P", "vpc_network", "net-1")}
	require.NoError(t, apply(context.Background(), fgaregister.EventUnregister, p))

	require.Len(t, f.unregCalls, 1)
	assert.Nil(t, f.unregCalls[0].GetSourceVersion(), "zero source_version → nil")
}

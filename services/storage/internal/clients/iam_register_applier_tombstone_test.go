// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/storage/internal/fgaregister"
)

// Test_RegisterApplier_ForwardsTombstoneOnUnregister — снятие регистрации обязано
// нести source_version как TOMBSTONE.
//
// register-путь уже версионирован на обеих сторонах ребра: emitFGARegister штампует
// `jsonb_set(payload,'{source_version}', to_jsonb(now()))` внутри writer-TX, applier
// прокидывает его в RegisterResource. iam удаляет строку зеркала под
// `WHERE source_version <= $tombstone`, поэтому unregister БЕЗ версии ('-infinity')
// не матчит версионированную строку — зеркало переживает удаление Volume/Snapshot, а
// level-triggered реконсайлер iam продолжает ре-материализовать его tuple'ы
// (over-grant на удалённый объект). Инвариант: снятие регистрации удаляет строку
// зеркала целиком и никогда не гейтится. Паритет с compute/nlb.
func Test_RegisterApplier_ForwardsTombstoneOnUnregister(t *testing.T) {
	f := &fakeIAMRegister{}
	apply := NewIAMRegisterApplier(f)

	tombstone := time.Now().UTC().Truncate(time.Microsecond)
	p := fgaregister.Payload{
		Tuple:         fgaregister.StorageVolume("prj-1", "vol-abc"),
		SourceVersion: tombstone,
	}
	require.NoError(t, apply(context.Background(), fgaregister.EventUnregister, p))

	require.Len(t, f.unregisterCalls, 1)
	req := f.unregisterCalls[0]
	require.NotNil(t, req.GetSourceVersion(), "unregister must carry the tombstone version")
	assert.True(t, req.GetSourceVersion().AsTime().Equal(tombstone),
		"tombstone must be the DB stamp of the unregister row, got %s", req.GetSourceVersion().AsTime())
}

// Test_RegisterApplier_UnregisterZeroSourceVersion_ForwardsNil — legacy-строка без
// stamp'а по-прежнему шлёт nil (iam: '-infinity') — прежнее безусловное поведение для
// пары legacy-register/legacy-unregister сохраняется.
func Test_RegisterApplier_UnregisterZeroSourceVersion_ForwardsNil(t *testing.T) {
	f := &fakeIAMRegister{}
	apply := NewIAMRegisterApplier(f)

	p := fgaregister.Payload{Tuple: fgaregister.StorageVolume("prj-1", "vol-abc")}
	require.NoError(t, apply(context.Background(), fgaregister.EventUnregister, p))

	require.Len(t, f.unregisterCalls, 1)
	assert.Nil(t, f.unregisterCalls[0].GetSourceVersion(), "zero source_version → nil")
}

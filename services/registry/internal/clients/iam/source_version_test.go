// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// source_version_test.go — registry must carry the monotonic source_version on BOTH
// delivery paths so kacho-iam's redelivery gate can recognise the second delivery.
//
// Every registration reaches iam TWICE: the synchronous registrar right after the
// writer-tx commits, and the register-drainer replaying the same durable outbox row.
// iam's gate skips the enqueue + event + fan-out when a delivery changes no mirror row,
// but ONLY when that delivery carries a version to compare — an unversioned producer
// fails OPEN into doing the work (a deliberate guard: without a version the mirror's
// monotonic UPSERT reports "unchanged" for reasons that have nothing to do with
// redelivery). registry sent no version on either path, so it was never gated and paid
// for both deliveries.
package iam

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
)

// outboxPayload renders the JSONB payload of one registry_outbox row: the intent as
// Go marshals it, with the source_version the BEFORE-INSERT trigger stamps spliced in
// (migration 0011 stamps clock_timestamp()). Building it from raw JSON — rather than
// from a Go struct literal — keeps the test honest about what the drainer actually
// decodes off the wire.
func outboxPayload(t *testing.T, intent domain.RegisterIntent, stamped string) []byte {
	t.Helper()
	raw, err := intent.Marshal()
	require.NoError(t, err)
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &m))
	m["source_version"] = json.RawMessage(stamped)
	out, err := json.Marshal(m)
	require.NoError(t, err)
	return out
}

// Test_RegisterApplier_ForwardsSourceVersion — the drainer path must forward the
// version the DB stamped inside the writer-tx. Without it iam sees '-infinity', the
// gate has nothing to compare, and the redelivery re-runs the whole materialisation.
func Test_RegisterApplier_ForwardsSourceVersion(t *testing.T) {
	stamped := time.Date(2026, 7, 26, 10, 0, 0, 123456000, time.UTC)
	intent := domain.RegisterIntentForCreate(
		&domain.Registry{ID: "reg-1", ProjectID: "prj-1"}, "user", "usr-abc")
	payload := outboxPayload(t, intent, fmt.Sprintf("%q", stamped.Format(time.RFC3339Nano)))

	decoded, err := DecodeRegisterIntent(payload)
	require.NoError(t, err)

	fake := &scriptedRegisterClient{}
	require.NoError(t, NewRegisterApplier(fake)(context.Background(), domain.FGAEventRegister, decoded))

	require.Len(t, fake.registerReqs, 2, "project-tuple + owner-tuple")
	// Первый tuple несёт стамп строки как есть; последующие — тот же стамп со сдвигом
	// на микросекунду за tuple (см. Test_RegisterApplier_VersionStrictlyIncreasesWithinRow:
	// gate ключуется на строке зеркала, то есть по ОБЪЕКТУ, и без сдвига проглотил бы
	// второй tuple того же объекта как редоставку).
	for i, req := range fake.registerReqs {
		require.NotNil(t, req.GetSourceVersion(), "tuple[%d]: source_version must be forwarded", i)
		want := stamped.Add(time.Duration(i) * time.Microsecond)
		assert.True(t, req.GetSourceVersion().AsTime().Equal(want),
			"tuple[%d]: want %s, got %s", i, want, req.GetSourceVersion().AsTime())
	}
}

// Test_RegisterApplier_ForwardsTombstoneOnUnregister — the unregister path must carry
// the version too, as a TOMBSTONE. iam removes the mirror row under
// `source_version <= $tombstone`, so once registers are versioned an unversioned
// unregister ('-infinity') can never match and the mirror row — with the tuples the
// level-triggered reconciler keeps re-materialising off it — survives the delete.
// "Unregistration removes the mirror row outright" is the invariant this pins.
func Test_RegisterApplier_ForwardsTombstoneOnUnregister(t *testing.T) {
	tombstone := time.Date(2026, 7, 26, 11, 0, 0, 0, time.UTC)
	intent := domain.UnregisterIntentForDelete("reg-1", "prj-1")
	payload := outboxPayload(t, intent, fmt.Sprintf("%q", tombstone.Format(time.RFC3339Nano)))

	decoded, err := DecodeRegisterIntent(payload)
	require.NoError(t, err)

	fake := &scriptedRegisterClient{}
	require.NoError(t, NewRegisterApplier(fake)(context.Background(), domain.FGAEventUnregister, decoded))

	require.Len(t, fake.unregisterReqs, 1)
	req := fake.unregisterReqs[0]
	require.NotNil(t, req.GetSourceVersion(), "unregister must carry the tombstone version")
	assert.True(t, req.GetSourceVersion().AsTime().Equal(tombstone))
}

// Test_DecodeRegisterIntent_LegacyNumericSourceVersion — rows enqueued BEFORE migration
// 0011 carry the BIGSERIAL row id under `source_version` (a JSON number, not a time).
// Those rows must still decode and apply unversioned, exactly as they do today: a
// decode error would be classified ErrPermanent and POISON the row, losing an
// owner-tuple that is already durable. Rollout safety, not cosmetics.
func Test_DecodeRegisterIntent_LegacyNumericSourceVersion(t *testing.T) {
	intent := domain.RegisterIntentForCreate(
		&domain.Registry{ID: "reg-1", ProjectID: "prj-1"}, "user", "usr-abc")
	payload := outboxPayload(t, intent, "12345")

	decoded, err := DecodeRegisterIntent(payload)
	require.NoError(t, err, "a legacy numeric source_version must never poison the row")
	require.Len(t, decoded.Tuples, 2)

	fake := &scriptedRegisterClient{}
	require.NoError(t, NewRegisterApplier(fake)(context.Background(), domain.FGAEventRegister, decoded))
	require.Len(t, fake.registerReqs, 2)
	// No version → nil on the wire → iam '-infinity' → applies unconditionally and the
	// gate fails OPEN. Same behaviour as before this change.
	assert.Nil(t, fake.registerReqs[0].GetSourceVersion(), "legacy row stays unversioned")
}

// Test_SyncRegistrar_StampsSourceVersion — the synchronous path must stamp a version
// too, and it must be at least as new as the one the queue carries for the same
// registration: the sync call happens AFTER the writer-tx commits, the queue stamp was
// taken INSIDE it. That ordering is what makes the drainer's replay the redelivery that
// loses the comparison and gets gated, rather than the sync call.
func Test_SyncRegistrar_StampsSourceVersion(t *testing.T) {
	queueStamp := time.Now().UTC() // stands in for the DB stamp taken inside the writer-tx
	intent := domain.RegisterIntentForCreate(
		&domain.Registry{ID: "reg-1", ProjectID: "prj-1"}, "user", "usr-abc")

	fake := &scriptedRegisterClient{}
	require.NoError(t, NewSyncRegistrar(fake).Register(context.Background(), []domain.RegisterIntent{intent}))
	after := time.Now().UTC()

	require.Len(t, fake.registerReqs, 2)
	// Знак несёт вызов, закрывающий набор объекта (см.
	// Test_SyncRegistrar_VersionOnlyOnLastTupleOfObject).
	mark := fake.registerReqs[1].GetSourceVersion()
	require.NotNil(t, mark, "sync registrar must stamp source_version")
	assert.False(t, mark.AsTime().Before(queueStamp),
		"sync stamp %s must not precede the queue stamp %s taken before the commit", mark.AsTime(), queueStamp)
	assert.False(t, mark.AsTime().After(after.Add(time.Millisecond)), "sync stamp must be wall-clock now, not a future value")
}

// Test_SyncRegistrar_VersionOnlyOnLastTupleOfObject — синхронный путь поднимает
// «водяной знак» объекта РОВНО ОДИН раз, на ПОСЛЕДНЕМ его tuple'е; предыдущие идут
// БЕЗ версии.
//
// Gate редоставки ключуется на строке зеркала, а она — ПО ОБЪЕКТУ, не по tuple'у.
// Create реестра шлёт на один объект два tuple'а (project-hierarchy, затем
// creator-owner). Если версию несёт КАЖДЫЙ, то любая её постановка сразу двигает
// watermark — и при обрыве набора посередине (iam моргнул между двумя вызовами;
// registrar обрывается на первой ошибке и НЕ ретраит — он fire-once best-effort)
// зеркало остаётся поднятым, а последующая редоставка drainer'ом гейтится ЦЕЛИКОМ:
// owner-tuple теряется навсегда и молча. Отдавая версию только на последнем tuple'е
// объекта, мы поднимаем watermark лишь когда весь его набор доставлен: оборванный
// набор оставляет зеркало низким, и at-least-once backstop нормально доводит дело.
// Неверсионированные вызовы gate не глотает — он открывается в сторону работы.
func Test_SyncRegistrar_VersionOnlyOnLastTupleOfObject(t *testing.T) {
	// Create-intent: project-tuple + owner-tuple — ОБА на registry_registry:reg-1.
	intent := domain.RegisterIntentForCreate(
		&domain.Registry{ID: "reg-1", ProjectID: "prj-1"}, "user", "usr-abc")
	require.Len(t, intent.Tuples, 2)
	require.Equal(t, intent.Tuples[0].Object, intent.Tuples[1].Object, "both tuples target one object")

	fake := &scriptedRegisterClient{}
	require.NoError(t, NewSyncRegistrar(fake).Register(context.Background(), []domain.RegisterIntent{intent}))

	require.Len(t, fake.registerReqs, 2)
	assert.Nil(t, fake.registerReqs[0].GetSourceVersion(),
		"tuple[0] must stay unversioned: it is not the last of its object, so it must not raise the watermark")
	require.NotNil(t, fake.registerReqs[1].GetSourceVersion(),
		"tuple[1] closes the object's set and carries the watermark")
}

// Test_SyncRegistrar_AbortedSetLeavesWatermarkDown — прямая проверка того, ради чего
// нужно правило выше: набор, оборвавшийся НЕ дойдя до своего последнего tuple'а, не
// поднимает watermark вообще — поэтому редоставка из очереди НЕ будет загейчена и
// довезёт весь набор.
//
// Знак и «набор доставлен» совпадают by construction: знак несёт РОВНО тот вызов,
// который закрывает объект, поэтому зеркало поднимается тогда и только тогда, когда
// этот вызов реально применён. Даже потерянный ответ на него безопасен: если сервер
// успел применить — знак поднят и терять нечего; если не успел — знак не поднят и
// повтор из очереди доводит набор.
func Test_SyncRegistrar_AbortedSetLeavesWatermarkDown(t *testing.T) {
	intent := domain.RegisterIntentForCreate(
		&domain.Registry{ID: "reg-1", ProjectID: "prj-1"}, "user", "usr-abc")
	require.Len(t, intent.Tuples, 2)

	// Первый же tuple падает transient'ом — registrar обрывает набор, закрывающий
	// вызов не отправляется вовсе.
	fake := &scriptedRegisterClient{registerErrs: []error{status.Error(codes.Unavailable, "iam blip")}}
	err := NewSyncRegistrar(fake).Register(context.Background(), []domain.RegisterIntent{intent})
	require.Error(t, err)

	require.Len(t, fake.registerReqs, 1, "aborted on the first tuple; the closing call was never sent")
	assert.Nil(t, fake.registerReqs[0].GetSourceVersion(),
		"an aborted set must raise no watermark, so the drainer replay is NOT gated")
}

// Test_SyncRegistrar_WatermarkPerObject — водяной знак ставится ПО ОБЪЕКТУ, а не один
// на всю доставку: доставка из нескольких intent'ов (repo-push + public-grant) несёт
// РАЗНЫЕ объекты, и каждый обязан получить свой watermark на своём последнем tuple'е —
// иначе объекты, кроме последнего, никогда не гейтятся и экономии на них нет.
func Test_SyncRegistrar_WatermarkPerObject(t *testing.T) {
	push := domain.RegisterIntentForRepoPush("reg-1", "team/app", "prj-1", "service_account:sva-x")
	grant := domain.RegisterIntentForRepoPublicGrant("reg-2", "team/other")
	require.Len(t, push.Tuples, 2, "parent-tuple + owner-tuple on one object")
	require.Len(t, grant.Tuples, 1)

	fake := &scriptedRegisterClient{}
	require.NoError(t, NewSyncRegistrar(fake).Register(context.Background(),
		[]domain.RegisterIntent{push, grant}))

	require.Len(t, fake.registerReqs, 3)
	assert.Nil(t, fake.registerReqs[0].GetSourceVersion(), "push tuple[0]: not the last of its object")
	require.NotNil(t, fake.registerReqs[1].GetSourceVersion(), "push tuple[1]: closes its object")
	require.NotNil(t, fake.registerReqs[2].GetSourceVersion(), "grant: sole tuple of its own object")
}

// Test_RegisterApplier_VersionStrictlyIncreasesWithinRow — то же для пути очереди:
// одна outbox-строка несёт ВЕСЬ набор tuple'ов с ОДНИМ маркером, поэтому applier обязан
// шагать так же. Иначе редоставка, пришедшая ПЕРВОЙ (sync-путь упал / iam моргнул),
// поставит только первый tuple объекта, а at-least-once backstop потеряет owner-tuple
// навсегда.
func Test_RegisterApplier_VersionStrictlyIncreasesWithinRow(t *testing.T) {
	stamped := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	intent := domain.RegisterIntentForCreate(
		&domain.Registry{ID: "reg-1", ProjectID: "prj-1"}, "user", "usr-abc")
	payload := outboxPayload(t, intent, fmt.Sprintf("%q", stamped.Format(time.RFC3339Nano)))

	decoded, err := DecodeRegisterIntent(payload)
	require.NoError(t, err)

	fake := &scriptedRegisterClient{}
	require.NoError(t, NewRegisterApplier(fake)(context.Background(), domain.FGAEventRegister, decoded))

	require.Len(t, fake.registerReqs, 2)
	v0 := fake.registerReqs[0].GetSourceVersion().AsTime()
	v1 := fake.registerReqs[1].GetSourceVersion().AsTime()
	assert.True(t, v0.Equal(stamped), "tuple[0] carries the row's own stamp")
	assert.True(t, v1.After(v0), "tuple[1] must strictly exceed tuple[0]")
	assert.GreaterOrEqual(t, v1.Sub(v0), time.Microsecond, "step must survive microsecond truncation")
}

// Test_SyncRegistrar_VersionAdvancesPerRegistration — grant → revoke → grant must not
// collapse: each registration carries a strictly newer version than the last, so the
// gate can never mistake a genuine re-registration for a redelivery.
func Test_SyncRegistrar_VersionAdvancesPerRegistration(t *testing.T) {
	intent := domain.RegisterIntentForCreate(
		&domain.Registry{ID: "reg-1", ProjectID: "prj-1"}, "user", "usr-abc")

	fake := &scriptedRegisterClient{}
	sr := NewSyncRegistrar(fake)
	require.NoError(t, sr.Register(context.Background(), []domain.RegisterIntent{intent}))
	time.Sleep(2 * time.Millisecond)
	require.NoError(t, sr.Register(context.Background(), []domain.RegisterIntent{intent}))

	require.Len(t, fake.registerReqs, 4)
	// Знак каждой доставки — на её закрывающем вызове (индексы 1 и 3).
	firstGrant := fake.registerReqs[1].GetSourceVersion().AsTime()
	reGrant := fake.registerReqs[3].GetSourceVersion().AsTime()
	assert.True(t, reGrant.After(firstGrant),
		"a re-registration must carry a strictly newer version (%s must be after %s)", reGrant, firstGrant)
}

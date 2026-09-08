// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// source_version_test.go — registry must carry the monotonic source_version on BOTH
// delivery paths so kaname's redelivery gate can recognise the second delivery.
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
// ── синхронный путь: версия ИЗ WRITER-ТРАНЗАКЦИИ, а не водяной знак ────────
//
// Шесть проб этого места закрепляли ВОДЯНОЙ ЗНАК: часы момента доставки,
// проставляемые один раз на объект, на последнем его tuple'е. Знак был обходом
// того, что настоящей версии на синхронном пути не было вовсе. Версия появилась
// (эмиттер возвращает штамп триггера из writer-транзакции), обход снят, и пробы
// переписаны под новый контракт — а не удалены вместе с ним.

// Test_SyncRegistrar_CarriesWriterTxVersionOfEachTuple — синхронная доставка
// несёт версию writer-транзакции, шагнутую по tuple'ам.
//
// Утверждается РАВЕНСТВО конкретным значениям, а не «версия не пуста»:
// «не пуста» зеленело бы и на часах момента доставки — ровно на том, что здесь
// стояло.
func Test_SyncRegistrar_CarriesWriterTxVersionOfEachTuple(t *testing.T) {
	fake := &deadlineCapturingRegisterClient{}
	reg, err := NewSyncRegistrar(fake)
	require.NoError(t, err)

	base := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	intent := domain.RegisterIntent{
		Kind:            domain.RegisterIntentKindRegistry,
		ResourceID:      "reg-1",
		Tuples:          []domain.FGATuple{{SubjectID: "project:prj-1", Relation: "project", Object: "registry_registry:reg-1"}, {SubjectID: "user:usr-1", Relation: "owner", Object: "registry_registry:reg-1"}},
		ParentProjectID: "prj-1",
		SourceVersion:   domain.SourceVersion{Time: base},
	}
	require.NoError(t, reg.Register(context.Background(), []domain.RegisterIntent{intent}))

	require.Len(t, fake.reqs, 2)
	assert.True(t, fake.reqs[0].GetSourceVersion().AsTime().Equal(base),
		"первый tuple обязан нести штамп writer-транзакции как есть")
	assert.True(t, fake.reqs[1].GetSourceVersion().AsTime().Equal(base.Add(sourceVersionStep)),
		"второй tuple ТОГО ЖЕ объекта обязан быть строго новее первого, иначе он неотличим от редоставки и будет проглочен")
}

// Test_SyncRegistrar_AgreesWithDrainPathTupleByTuple — ГЛАВНАЯ проба: версии,
// которые синхронный путь ставит каждому tuple'у, СОВПАДАЮТ с теми, что тому же
// намерению поставит дренаж.
//
// Ради этого совпадения всё и делалось. Пока версии расходились, гашение
// повторной доставки зависело от того, кто выиграл гонку: приди дренаж первым —
// синхронный вызов выглядел новее состоянием и заставлял пересчитывать
// материализацию заново, на самом горячем пути создания.
//
// Проба сверяет ДВА ПРОИЗВОДИТЕЛЯ между собой, а не каждый со своей ожидаемой
// константой: константа зафиксировала бы сегодняшнее значение и промолчала бы,
// разойдись пути завтра.
func Test_SyncRegistrar_AgreesWithDrainPathTupleByTuple(t *testing.T) {
	base := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	intent := domain.RegisterIntent{
		Kind:            domain.RegisterIntentKindRegistry,
		ResourceID:      "reg-1",
		Tuples:          []domain.FGATuple{{SubjectID: "project:prj-1", Relation: "project", Object: "registry_registry:reg-1"}, {SubjectID: "user:usr-1", Relation: "owner", Object: "registry_registry:reg-1"}},
		ParentProjectID: "prj-1",
		SourceVersion:   domain.SourceVersion{Time: base},
	}

	fake := &deadlineCapturingRegisterClient{}
	reg, err := NewSyncRegistrar(fake)
	require.NoError(t, err)
	require.NoError(t, reg.Register(context.Background(), []domain.RegisterIntent{intent}))
	require.Len(t, fake.reqs, len(intent.Tuples))

	for seq := range intent.Tuples {
		fromDrain := stepSourceVersion(base, seq)
		fromSync := fake.reqs[seq].GetSourceVersion()
		require.NotNil(t, fromDrain)
		assert.True(t, fromDrain.AsTime().Equal(fromSync.AsTime()),
			"tuple %d: дренаж поставит %s, синхронная доставка — %s; расхождение возвращает "+
				"зависимость гашения от гонки", seq, fromDrain.AsTime(), fromSync.AsTime())
	}
}

// Test_SyncRegistrar_UnversionedIntentIsNotDelivered — намерение без версии
// (строка, поставленная в очередь до появления маркера) синхронно НЕ
// доставляется: общая форма доставки регистрацию без маркера отвергает, а
// дренаж такую строку доведёт сам.
func Test_SyncRegistrar_UnversionedIntentIsNotDelivered(t *testing.T) {
	fake := &deadlineCapturingRegisterClient{}
	reg, err := NewSyncRegistrar(fake)
	require.NoError(t, err)
	require.NoError(t, reg.Register(context.Background(), []domain.RegisterIntent{{
		Kind:       domain.RegisterIntentKindRegistry,
		ResourceID: "reg-legacy",
		Tuples:     []domain.FGATuple{{SubjectID: "project:prj-1", Relation: "project", Object: "registry_registry:reg-legacy"}},
	}}))
	assert.Empty(t, fake.reqs, "намерение без версии ушло на провод")
}

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

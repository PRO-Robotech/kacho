// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// СМЕНА МЕТКИ ОБЯЗАНА МАТЕРИАЛИЗОВАТЬСЯ НА ПУТИ ЗАПРОСА, КАК И СОЗДАНИЕ.
//
// Create после коммита СИНХРОННО доставляет тот же register-intent, который уже
// лежит в durable-outbox'е, поэтому владелец видит свой свежий ресурс сразу.
// Update, меняющий метки, делал только durable-эмиссию — доставку целиком
// отдавал дренажу. Разница наблюдаема и измерена на стенде 2026-08-05: строки
// intent'ов, порождённые Update'ами, лежали в `kacho_vpc.fga_register_outbox`
// от 188 до 365 секунд, пока дренаж разбирал очередь, накопленную конкурентной
// волной; клиентский бюджет чтения-своих-записей — 15 с.
//
// Практический смысл — не «медленно», а АСИММЕТРИЯ ДВУХ ПОЛОВИН ОДНОГО
// КОНТРАКТА: выдача по метке применяется на пути запроса, а ОТЗЫВ по снятию
// метки — когда до него дойдёт очередь. Отзыв прав, отложенный на неограниченное
// очередью время, — это стоящий доступ, которого никто не выдавал.
//
// Проба утверждает ИСХОД: синхронный регистратор ПОЛУЧИЛ обновлённый набор
// меток и версию, проштампованную эмиттером внутри writer-TX (ту же, что несёт
// durable-intent) — иначе монотонное гашение повторной доставки на приёмной
// стороне зависело бы от того, кто выиграл гонку.
func TestUpdateUseCase_SyncRegister_OnLabelChange(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	reg := &recordingRegistrar{}

	created := seedNetworkForUpdate(t, kr, or)

	uc := NewUpdateNetworkUseCase(kr, or).WithRegistrar(reg)
	op, err := uc.Execute(context.Background(), UpdateInput{
		NetworkID:  created,
		Network:    domain.Network{Labels: domain.RcLabels{"network": "treska"}},
		UpdateMask: []string{"labels"},
	})
	require.NoError(t, err)
	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.True(t, saved.Done)
	require.Nil(t, saved.Error)

	calls := reg.snapshot()
	require.Len(t, calls, 1, "смена меток обязана быть доставлена синхронно ровно один раз")
	require.Len(t, calls[0], 1)
	item := calls[0][0]
	assert.Equal(t, "vpc_network:"+created, item.Tuple.Object)
	assert.Equal(t, map[string]string{"network": "treska"}, item.Labels,
		"синхронная доставка обязана нести ОБНОВЛЁННЫЕ метки, иначе селектор у владельца прав останется на старых")

	// Версия — из штампа эмиттера (mock штампует 2026-01-01+N мс), а не из часов
	// вызывающего: различающее утверждение, ровно как в create_sync_register_test.
	sv := reg.lastVersion()
	require.False(t, sv.IsZero(), "версия обязана быть проставлена")
	assert.True(t, sv.Before(time.Now().Add(-24*time.Hour)),
		"версия пришла из часов вызывающего (%s), а не из штампа intent'а", sv)

	// Durable-intent остаётся backstop'ом: синхронная доставка его не заменяет.
	require.GreaterOrEqual(t, len(kr.FGARegisterEvents()), 2,
		"outbox-intent остаётся at-least-once backstop'ом (create + update)")
}

// ОТРИЦАТЕЛЬНЫЙ КОНТРОЛЬ: Update, не трогающий метки, синхронной доставки НЕ
// делает. Без него положительное утверждение зеленело бы и на реализации,
// которая шлёт регистрацию на каждый Update подряд — то есть на лишнем вызове к
// владельцу прав на каждое переименование.
func TestUpdateUseCase_NoSyncRegister_WhenLabelsUntouched(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	reg := &recordingRegistrar{}

	created := seedNetworkForUpdate(t, kr, or)

	uc := NewUpdateNetworkUseCase(kr, or).WithRegistrar(reg)
	op, err := uc.Execute(context.Background(), UpdateInput{
		NetworkID:  created,
		Network:    domain.Network{Description: domain.RcDescription("переименование без меток")},
		UpdateMask: []string{"description"},
	})
	require.NoError(t, err)
	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.True(t, saved.Done)
	require.Nil(t, saved.Error)

	assert.Empty(t, reg.snapshot(),
		"Update без меток в маске не меняет проекцию селектора — звать владельца прав незачем")
}

// Отказ синхронной доставки НЕ проваливает мутацию: строка уже закоммичена, а
// durable-intent доедет дренажом. Симметрично create-пути.
func TestUpdateUseCase_SyncRegisterFailure_DoesNotFailTheMutation(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	reg := &recordingRegistrar{err: errors.New("iam недоступен")}

	created := seedNetworkForUpdate(t, kr, or)

	uc := NewUpdateNetworkUseCase(kr, or).WithRegistrar(reg)
	op, err := uc.Execute(context.Background(), UpdateInput{
		NetworkID:  created,
		Network:    domain.Network{Labels: domain.RcLabels{}},
		UpdateMask: []string{"labels"},
	})
	require.NoError(t, err)
	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.True(t, saved.Done, "отказ ускорителя не может провалить уже закоммиченную мутацию")
	require.Nil(t, saved.Error)
	require.Len(t, reg.snapshot(), 1)
}

// seedNetworkForUpdate создаёт сеть через create-use-case (без registrar'а) и
// возвращает её id — общая преамбула трёх проб выше.
func seedNetworkForUpdate(t *testing.T, kr *kachomock.Repository, or *repomock.OpsRepo) string {
	t.Helper()
	cuc := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or)
	op, err := cuc.Execute(context.Background(), domain.Network{
		ProjectID: "f1",
		Name:      domain.RcNameVPC("net-upd-sync"),
	})
	require.NoError(t, err)
	repomock.AwaitOpDone(t, or, op.ID)
	nets := kr.Networks()
	require.Len(t, nets, 1)
	return nets[0].ID
}

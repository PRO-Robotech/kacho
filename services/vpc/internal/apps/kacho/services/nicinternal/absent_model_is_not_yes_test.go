// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// absent_model_is_not_yes_test.go — «модели здесь нет» не есть «да».
//
// Эталон уже стоит в дереве, у соседа: storage AllowedOnObject на условии «порт
// есть, спросить негде» ОТКАЗЫВАЕТ и говорит почему — «Это состояние посадки, а не
// ответ модели». Здесь то же условие обслуживает ListByInstance, за которым per-RPC
// Check не задаётся вовсе (ScopeFiltered).
//
// Соседний файл уже закрыл ПОЛОВИНУ этого: вызывающий без личности отсекается
// безусловно, и его godoc прямо называет причину — «привяжи мы fail-closed к наличию
// фильтра, конфигурация без фильтра отдавала бы вообще всё и вообще всем». Вторая
// половина — НАЗВАННЫЙ вызывающий при отсутствующем фильтре — осталась
// непроведённой: закрыли шумный подслучай, тихий выжил.
package nicinternal

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
)

// TestListByInstance_AbsentModelRefusesANamedCaller — фильтра нет, вызывающий назван:
// привязки не отдаются. Инстансы называет ВЫЗЫВАЮЩИЙ, поэтому проход означал бы
// выдачу привязок любых названных инстансов из чужих проектов и аккаунтов.
func TestListByInstance_AbsentModelRefusesANamedCaller(t *testing.T) {
	kr := kachomock.NewRepository()
	seedAttachedNIC(t, kr, "prj_theirs", "e9b_sub2", "nic_theirs", "ins_theirs")

	svc := NewService(kr) // фильтр не подключён

	att, err := svc.ListByInstance(context.Background(), "user:usr_alice", []string{"ins_theirs"})
	require.Error(t, err, "спросить негде — значит отказ, а не «да»")
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Empty(t, att)
}

// TestListByInstance_PresentModelStillAnswers — ПАРНЫЙ ПОЛОЖИТЕЛЬНЫЙ. Без него отказ
// выше неотличим от «отказывает всегда» и зеленел бы на полностью сломанном пути.
func TestListByInstance_PresentModelStillAnswers(t *testing.T) {
	kr := kachomock.NewRepository()
	seedAttachedNIC(t, kr, "prj_mine", "e9b_sub1", "nic_mine", "ins_mine")
	seedAttachedNIC(t, kr, "prj_theirs", "e9b_sub2", "nic_theirs", "ins_theirs")

	svc := NewService(kr).WithListFilter(&fakeNICFilter{allowed: []string{"nic_mine"}})

	att, err := svc.ListByInstance(context.Background(), "user:usr_alice",
		[]string{"ins_mine", "ins_theirs"})
	require.NoError(t, err, "модель на месте — ответ обязан быть получен")
	assert.Equal(t, []string{"nic_mine"}, nicIDsOf(att), "и он обязан быть СУЖЕНИЕМ")
}

// TestListByInstance_AbsentModelDoesNotSwallowTheAnonymityCut — два класса не должны
// схлопываться: безымянный вызывающий и при отсутствующем фильтре получает СВОЙ
// исход (пусто), а не отказ «модели нет». Иначе один фикс спрятал бы регрессию
// другого.
func TestListByInstance_AbsentModelDoesNotSwallowTheAnonymityCut(t *testing.T) {
	kr := kachomock.NewRepository()
	seedAttachedNIC(t, kr, "prj_theirs", "e9b_sub2", "nic_theirs", "ins_theirs")

	svc := NewService(kr) // фильтр не подключён

	att, err := svc.ListByInstance(context.Background(), "", []string{"ins_theirs"})
	require.NoError(t, err, "безымянный вызывающий отсекается своим путём, до вопроса о модели")
	assert.Empty(t, att)
}

// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// absent_model_is_not_yes_test.go — «модели здесь нет» не есть «да», а «никого не
// назвали» не есть «пусто».
//
// Оба исхода теперь принимает ОДНА функция общего фундамента, поэтому проба
// утверждает их вместе: они обязаны оставаться РАЗЛИЧИМЫМИ. Схлопни их — и фикс
// одного спрятал бы регрессию другого, что и произошло однажды: закрыли шумный
// подслучай, тихий выжил.
//
// Инстансы называет ВЫЗЫВАЮЩИЙ, а per-RPC Check за этим RPC не задаётся вовсе
// (ScopeFiltered), поэтому проход при отсутствующей модели означал бы выдачу
// привязок любых названных инстансов — из чужих проектов и аккаунтов.
package nicinternal

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
)

// TestListByInstance_AbsentModelRefusesANamedCaller — фильтра нет, вызывающий назван:
// привязки не отдаются.
func TestListByInstance_AbsentModelRefusesANamedCaller(t *testing.T) {
	kr := kachomock.NewRepository()
	seedAttachedNIC(t, kr, "prj_theirs", "e9b_sub2", "nic_theirs", "ins_theirs")

	svc := NewService(kr) // сужатель не подключён

	att, err := svc.ListByInstance(narrowtest.Caller(), []string{"ins_theirs"})
	require.Error(t, err, "спросить негде — значит отказ, а не «да»")
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Empty(t, att)
}

// TestListByInstance_UnnamedCallerIsRefusedByIdentityNotByWiring — второй исход, и он
// обязан отличаться от первого КОДОМ: вызывающего никто не назвал, и это не про
// посадку. Сужатель здесь подключён и разрешает всё — значит отказ приходит именно с
// линии личности, а не «потому что всё сломано».
func TestListByInstance_UnnamedCallerIsRefusedByIdentityNotByWiring(t *testing.T) {
	kr := kachomock.NewRepository()
	seedAttachedNIC(t, kr, "prj_theirs", "e9b_sub2", "nic_theirs", "ins_theirs")

	svc := NewService(kr).WithListFilter(narrowtest.AllowingAll())

	att, err := svc.ListByInstance(context.Background(), []string{"ins_theirs"})
	require.Error(t, err, "запрос никого не назвал — привязки не отдаются")
	assert.Equal(t, codes.Unauthenticated, status.Code(err),
		"ответ обязан быть про личность, а не про посадку: схлопнутые исходы прячут регрессию друг друга")
	assert.Empty(t, att)
}

// TestListByInstance_PresentModelStillAnswers — ПАРНЫЙ ПОЛОЖИТЕЛЬНЫЙ к обоим отказам.
// Без него они неотличимы от «отказывает всегда» и зеленели бы на полностью сломанном
// пути.
func TestListByInstance_PresentModelStillAnswers(t *testing.T) {
	kr := kachomock.NewRepository()
	seedAttachedNIC(t, kr, "prj_mine", "e9b_sub1", "nic_mine", "ins_mine")
	seedAttachedNIC(t, kr, "prj_theirs", "e9b_sub2", "nic_theirs", "ins_theirs")

	svc := NewService(kr).WithListFilter(narrowtest.Allowing("nic_mine"))

	att, err := svc.ListByInstance(narrowtest.Caller(), []string{"ins_mine", "ins_theirs"})
	require.NoError(t, err, "модель на месте — ответ обязан быть получен")
	assert.Equal(t, []string{"nic_mine"}, nicIDsOf(att), "и он обязан быть СУЖЕНИЕМ")
}

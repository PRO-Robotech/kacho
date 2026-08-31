// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package registry_test

// rename_ordering_test.go — переименование не уносит адресуемое содержимое.
//
// Что было. Перенос в движке двухфазный: скопировать все теги под новое имя, затем
// снять их под старым. Снятие шло ДО того, как закреплялись метаданные (строка
// наложения и права). Сбой посередине снятия — а он достижим не только отказом
// движка, но и обычным истечением дедлайна операции на репозитории с большим
// числом тегов — оставлял часть тегов НЕадресуемой под тем именем, которое знает
// тенант и на которое выданы права, тогда как под новым именем лежал полный набор,
// но без строки наложения и без регистрации: туда не доходил НИКТО, включая
// администратора аккаунта и облака. Операция при этом докладывала ОТКАЗ, то есть
// «ничего не произошло».
//
// Хуже того, повтор был закрыт терминально и БЕЗ всякой второй фазы: достаточно
// сбоя после первого же скопированного тега — целевое имя начинало считаться
// занятым, и любая следующая попытка получала «репозиторий уже существует» при
// полностью целом источнике.
//
// Что теперь. Необратимый шаг стоит ПОСЛЕ закрепления метаданных, а прерванная
// копия узнаётся как своя и не блокирует повтор.

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// TestRename_MetadataCommittedBeforeDestructiveStep — порядок шагов: снятие тегов
// под старым именем происходит ПОСЛЕ того, как новое имя закреплено в наложении.
// Это и есть предмет находки: обратный порядок делает сбой посередине потерей
// адресуемости, а не безобидным повтором.
func TestRename_MetadataCommittedBeforeDestructiveStep(t *testing.T) {
	cfg, ops := newMockCfg(), newMemOps()
	cfg.byName["ord/src"] = &domain.RepositoryConfig{RegistryID: regID, Name: "ord/src", Visibility: domain.VisibilityPrivate}
	zot := &mockZot{projByName: map[string]*domain.Repository{"ord/src": {RegistryID: regID, Name: "ord/src", TagCount: 3}}}
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	// Наблюдение в момент разрушающего шага: закреплено ли уже новое имя.
	var newNameCommittedAtPurge bool
	zot.onPurge = func() { newNameCommittedAtPurge = cfg.has("ord/dst") }

	op, err := uc.RenameRepository(aliceCtx(), regID, "ord/src", "ord/dst", "")
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, ops, op.ID).Error)

	require.Equal(t, []string{"copy", "purge"}, zot.renameSteps,
		"копирование обязано предшествовать снятию")
	require.True(t, newNameCommittedAtPurge,
		"снятие тегов под СТАРЫМ именем обязано идти ПОСЛЕ закрепления наложения: "+
			"иначе сбой посередине уносит теги из адресуемого имени, а операция докладывает отказ")
}

// TestRename_PurgeFailureLeavesEverythingAddressable — если снятие под старым
// именем не удалось, содержимое остаётся адресуемым (под новым именем), права и
// имя закреплены, а операция НЕ докладывает отказ: предмет переименования —
// перенос имени, и он состоялся.
func TestRename_PurgeFailureLeavesEverythingAddressable(t *testing.T) {
	cfg, ops := newMockCfg(), newMemOps()
	cfg.byName["res/src"] = &domain.RepositoryConfig{RegistryID: regID, Name: "res/src", Visibility: domain.VisibilityPrivate}
	zot := &mockZot{
		projByName: map[string]*domain.Repository{"res/src": {RegistryID: regID, Name: "res/src", TagCount: 2}},
		purgeErr:   regerrors.ErrUnavailable,
	}
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	op, err := uc.RenameRepository(aliceCtx(), regID, "res/src", "res/dst", "")
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, ops, op.ID).Error,
		"снятие остатка под старым именем не отменяет состоявшийся перенос")
	require.Contains(t, cfg.byName, "res/dst", "новое имя закреплено")
	require.NotContains(t, cfg.byName, "res/src")
}

// TestRename_CopyFailureDoesNotPoisonRetry — прерванная копия узнаётся как СВОЯ:
// повтор сходится. Раньше единственный скопированный тег делал целевое имя
// «занятым» навсегда, и переименование становилось невозможным при целом источнике.
func TestRename_CopyFailureDoesNotPoisonRetry(t *testing.T) {
	cfg, ops := newMockCfg(), newMemOps()
	cfg.byName["rty/src"] = &domain.RepositoryConfig{RegistryID: regID, Name: "rty/src", Visibility: domain.VisibilityPrivate}
	zot := &mockZot{
		projByName: map[string]*domain.Repository{
			"rty/src": {RegistryID: regID, Name: "rty/src", TagCount: 2},
			// Остаток прерванной копии: часть тегов источника уже под целевым именем.
			"rty/dst": {RegistryID: regID, Name: "rty/dst", TagCount: 1},
		},
		tagsByRepo: map[string][]string{
			"rty/src": {"v1", "v2"},
			"rty/dst": {"v1"},
		},
	}
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	op, err := uc.RenameRepository(aliceCtx(), regID, "rty/src", "rty/dst", "")
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, ops, op.ID).Error,
		"повтор после прерванной копии обязан сходиться, а не отвечать «уже существует»")
	require.Contains(t, cfg.byName, "rty/dst")
}

// TestRename_IndependentTargetStillCollides — обратная сторона инъекции: целевое
// имя, занятое ЧУЖИМ содержимым (теги, которых у источника нет), по-прежнему
// отвергается. Иначе послабление выше проглатывало бы чужой репозиторий.
func TestRename_IndependentTargetStillCollides(t *testing.T) {
	cfg, ops := newMockCfg(), newMemOps()
	cfg.byName["ind/src"] = &domain.RepositoryConfig{RegistryID: regID, Name: "ind/src", Visibility: domain.VisibilityPrivate}
	zot := &mockZot{
		projByName: map[string]*domain.Repository{
			"ind/src": {RegistryID: regID, Name: "ind/src", TagCount: 1},
			"ind/dst": {RegistryID: regID, Name: "ind/dst", TagCount: 1},
		},
		tagsByRepo: map[string][]string{
			"ind/src": {"v1"},
			"ind/dst": {"someone-elses"},
		},
	}
	uc := ucWithRegistry(cfg, zot, ops, domain.VisibilityPrivate)

	op, err := uc.RenameRepository(aliceCtx(), regID, "ind/src", "ind/dst", "")
	require.NoError(t, err)
	d := awaitOpDone(t, ops, op.ID)
	require.NotNil(t, d.Error)
	require.Equal(t, int32(codes.AlreadyExists), d.Error.Code)
	require.Equal(t, "repository already exists", d.Error.Message)
}

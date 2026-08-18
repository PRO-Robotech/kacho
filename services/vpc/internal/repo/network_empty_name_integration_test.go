// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
)

// TestIntegration_Network_EmptyNameIsNotAnIdentity — #669: пустое имя НЕ занимает
// слот `UNIQUE(project_id, name)`, а непустой дубль по-прежнему отвергается.
//
// # Предмет
//
// Контракт объявляет пустое имя законным: `nameReVPC` в `pkg/validate` допускает
// пустую строку явно («empty allowed» стоит в самом тексте отказа), и то же
// говорит CHECK `networks_name_check` на этой таблице. Но уникальный индекс сети
// был ПОЛНЫМ, тогда как у семи соседних ресурсов того же сервиса — частичным
// (`WHERE name <> ”`). Следствие: вторая сеть без имени в одном проекте ловила
// 23505 → ALREADY_EXISTS, тогда как второй адрес, подсеть, группа или маршрутная
// таблица — нет. Один контракт исполнялся по-разному в зависимости от ресурса.
//
// # Почему две ветки, а не одна
//
// Отрицание («пустые сосуществуют») в одиночку зеленеет и на снятом индексе — то
// есть на потере уникальности имён целиком. Поэтому рядом стоит положительный
// контроль: непустой дубль обязан по-прежнему получать ErrAlreadyExists. Первая
// ветка доказывает, что индекс стал частичным; вторая — что он остался
// уникальным. Врозь ни одна из них этого не утверждает.
func TestIntegration_Network_EmptyNameIsNotAnIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)
	defer r.Close()

	// insert открывает свою транзакцию, вставляет сеть и коммитит. Возвращает
	// ошибку вставки, чтобы ветка ниже утверждала о НЕЙ, а не о коммите.
	insert := func(projectID, name string) error {
		w, werr := r.Writer(ctx)
		require.NoError(t, werr)
		defer w.Abort()
		if _, ierr := w.Networks().Insert(ctx, &domain.Network{
			ID:        ids.NewID(ids.PrefixNetwork),
			ProjectID: projectID,
			Name:      domain.RcNameVPC(name),
		}); ierr != nil {
			return ierr
		}
		return w.Commit()
	}

	t.Run("пустое имя не занимает слот — две безымянные сети сосуществуют", func(t *testing.T) {
		const projectID = "prj-emptyname-coexist"

		require.NoError(t, insert(projectID, ""),
			"первая сеть с пустым именем обязана создаваться")
		require.NoError(t, insert(projectID, ""),
			"вторая сеть с пустым именем обязана создаваться: контракт объявляет "+
				"пустое имя законным, и шесть соседних ресурсов vpc так и ведут себя")
	})

	t.Run("положительный контроль: непустой дубль по-прежнему отвергается", func(t *testing.T) {
		const projectID, name = "prj-emptyname-control", "core-prod"

		require.NoError(t, insert(projectID, name),
			"первая сеть с непустым именем обязана создаваться")
		err := insert(projectID, name)
		require.Error(t, err,
			"дубль непустого имени обязан отвергаться — иначе снятие индекса целиком "+
				"прошло бы как успех этой пробы")
		require.ErrorIs(t, err, repo.ErrAlreadyExists,
			"дубль обязан маппиться в ErrAlreadyExists (23505), а не в общую ошибку")
	})
}

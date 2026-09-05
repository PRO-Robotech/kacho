// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// Baseline 0001_initial.sql несет DB-уровневые CHECK на name regex и длину
// description. Эти тесты идут в обход domain.Network.Validate()
// (прямой INSERT через writer.Networks().Insert, который Validate не зовет) и
// убеждаются, что DB-CHECK срабатывает: 23514 → helpers.WrapPgErr →
// helpers.ErrInvalidArg.

func TestIntegration_NetworkRepo_CheckConstraints(t *testing.T) {
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

	insertNet := func(t *testing.T, n *domain.Network) error {
		t.Helper()
		w, err := r.Writer(ctx)
		require.NoError(t, err)
		_, err = w.Networks().Insert(ctx, n)
		if err != nil {
			w.Abort()
			return err
		}
		return w.Commit()
	}

	// 1. Корректное имя проходит.
	good := &domain.Network{
		ID:        ids.NewID(ids.PrefixNetwork),
		ProjectID: "project-check",
		Name:      domain.RcNameVPC("good-name"),
	}
	require.NoError(t, insertNet(t, good))

	// 2. Имя не по форме — отклоняется DB-CHECK regex.
	//
	// Здесь стояло "1bad" с пояснением «начинающееся с цифры». Прежняя форма
	// требовала букву первым символом, единственная форма дерева (#715) —
	// не требует: "1bad" ей ОТВЕЧАЕТ, и проба на нём зеленела бы вхолостую.
	// Отвергается теперь подчёркивание — его в форме нет.
	bad := &domain.Network{
		ID:        ids.NewID(ids.PrefixNetwork),
		ProjectID: "project-check",
		Name:      domain.RcNameVPC("bad_name"),
	}
	err = insertNet(t, bad)
	require.Error(t, err, "имя не по форме должно быть отклонено CHECK")
	// Полоса — ErrInternal, а не ErrInvalidArg, и это решение задачи #718:
	// проба вставляет МИМО use-case, то есть воспроизводит «сервис пропустил
	// негодное имя». Настоящий вызывающий сюда не доходит — его имя судит
	// `domain.RcNameVPC` до вставки, — поэтому обвинение его ввода было бы
	// неправдой, а исправить ему было бы нечего.
	require.Truef(t, errors.Is(err, helpers.ErrInternal),
		"форма имени — дефект сервиса: expected helpers.ErrInternal, got: %v", err)
	require.Falsef(t, errors.Is(err, helpers.ErrInvalidArg),
		"дефект сервиса не обвиняет вызывающего: %v", err)

	// 3. Description длиннее 256 chars — отклоняется DB-CHECK length.
	longDesc := make([]byte, 257)
	for i := range longDesc {
		longDesc[i] = 'a'
	}
	tooLong := &domain.Network{
		ID:          ids.NewID(ids.PrefixNetwork),
		ProjectID:   "project-check",
		Name:        domain.RcNameVPC("long-desc"),
		Description: domain.RcDescription(longDesc),
	}
	err = insertNet(t, tooLong)
	require.Error(t, err, "description >256 chars должно быть отклонено CHECK")
	require.Truef(t, errors.Is(err, helpers.ErrInvalidArg),
		"expected helpers.ErrInvalidArg from CHECK violation, got: %v", err)

	// 4. Пустое имя — ОТКЛОНЯЕТСЯ.
	//
	// Прежде проба утверждала обратное («разрешительная политика допускает
	// empty»). Это перестало быть правдой вместе с формой (#715): пустая строка
	// ей не отвечает, а миграция 715001 сняла и частичный уникальный индекс,
	// существовавший ради пустых имён. Утверждение перевёрнуто, а не снято, —
	// иначе пропал бы контроль на то, что пустое имя до записи не доживает.
	empty := &domain.Network{
		ID:        ids.NewID(ids.PrefixNetwork),
		ProjectID: "project-check",
		Name:      domain.RcNameVPC(""),
	}
	err = insertNet(t, empty)
	require.Error(t, err, "пустое имя больше не является допустимым значением")
	// Та же полоса, что у пункта 2: пустая строка форме не отвечает, значит это
	// то же ограничение формы имени и тот же смысл — сервис пропустил.
	require.Truef(t, errors.Is(err, helpers.ErrInternal),
		"форма имени — дефект сервиса: expected helpers.ErrInternal, got: %v", err)
}

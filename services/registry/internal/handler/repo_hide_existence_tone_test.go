// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// repositoryMissText — тон, которым ВЛАДЕЛЕЦ называет отсутствующий репозиторий.
//
// Источник — слой хранения: services/registry/internal/repo/kacho/pg/repository_config.go
// (`fmt.Errorf("%w: repository not found", regerrors.ErrNotFound)`); маппер снимает
// только приставку-сигнал, поэтому на провод уходит эта строка дословно.
//
// Здесь она выписана литералом осознанно: проба обязана краснеть, если ЛЮБАЯ из двух
// сторон уедет. Сверка «текст равен сам себе» не заметила бы согласованного дрейфа.
const repositoryMissText = "repository not found"

// TestRepoDeny_SpeaksTheOwnersMissTone — отказ по репозиторию и ОТСУТСТВИЕ репозитория
// называются ОДНИМ текстом; отказ по реестру — своим.
//
// Зачем проба на сообщение, а не на код. Прежние REG-24/REG-25 утверждали только
// `codes.NotFound` и остались бы зелёными при любом тексте — а различимый текст и есть
// existence-oracle: по нему «есть, но не твой» отличается от «нет вовсе», то есть
// скрытие существования перестаёт скрывать (security.md, hardening-инвариант #6 —
// утверждать надо СООБЩЕНИЕ, а не только код). Дрейф и случился: checkRepo и
// checkRepository гейтили ОДИН объект и отвечали разными текстами.
//
// Положительный контроль обязателен: без него проба зеленела бы на реализации, где ВСЕ
// отказы отвечают одной строкой, — то есть перестала бы отличать верное от сломанного.
func TestRepoDeny_SpeaksTheOwnersMissTone(t *testing.T) {
	denyAll := func() *recordingAuthorizer { return &recordingAuthorizer{allow: map[string]bool{}} }

	msg := func(t *testing.T, err error) string {
		t.Helper()
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok, "ожидался gRPC status, получено %v", err)
		require.Equal(t, codes.NotFound, st.Code())
		return st.Message()
	}

	// Обе полосы, гейтящие РЕПОЗИТОРИЙ, говорят тоном владельца.
	t.Run("ListTags deny — тон владельца", func(t *testing.T) {
		err := newRepoAuthz(denyAll()).checkRepo(carolCtx(), "reg-A", "web", relationVList)
		require.Equal(t, repositoryMissText, msg(t, err),
			"отказ по репозиторию обязан быть НЕОТЛИЧИМ от его отсутствия")
	})
	t.Run("DeleteTag deny — тон владельца", func(t *testing.T) {
		err := newRepoAuthz(denyAll()).checkRepo(carolCtx(), "reg-A", "app", relationVDelete)
		require.Equal(t, repositoryMissText, msg(t, err))
	})
	t.Run("GetRepository deny — тот же тон", func(t *testing.T) {
		err := newRepoAuthz(denyAll()).checkRepository(carolCtx(), "reg-A", "web", relationVGet)
		require.Equal(t, repositoryMissText, msg(t, err))
	})

	// Положительный контроль: у РЕЕСТРА предмет другой, и тон у него свой.
	// Если эта половина покраснеет вместе с верхними — значит проба перестала
	// различать предметы, а не поймала дефект.
	t.Run("контроль: отказ по РЕЕСТРУ сохраняет свой тон", func(t *testing.T) {
		err := newRepoAuthz(denyAll()).namespaceGate(carolCtx(), "reg-A")
		require.NotEqual(t, repositoryMissText, msg(t, err),
			"реестр и репозиторий — разные предметы; один тон на оба означал бы, "+
				"что проба ничего не различает")
	})
}

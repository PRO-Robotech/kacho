// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal_test

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/ids"
	"github.com/PRO-Robotech/corelib/journaltx"
	"github.com/PRO-Robotech/corelib/subscription"
	"github.com/PRO-Robotech/kacho/services/storage/internal/subscriptionjournal"
)

// TestStorageJournal_NTF360_EveryKindDeclaresNameFormAndScope — NTF3-60 (половина
// storage), З2: каждый вид журнала storage объявляет форму имени и якорь. Вид без
// объявления функция фундамента писать отказывается, а сервер потока не отдаёт
// у него имени снятия. Все виды storage — проектные.
func TestStorageJournal_NTF360_EveryKindDeclaresNameFormAndScope(t *testing.T) {
	kinds := subscriptionjournal.Journal(false).Mapping.Kinds
	if len(kinds) == 0 {
		t.Fatal("словарь видов storage пуст — судить нечего")
	}
	words := make([]string, 0, len(kinds))
	for word, k := range kinds {
		words = append(words, word)
		if k.NameForm == subscription.NameFormUnset {
			t.Errorf("вид %s не объявил NameForm", word)
		}
		if k.Scope != subscription.ScopeProject {
			t.Errorf("вид %s: Scope = %d, ожидался ScopeProject (%d)", word, k.Scope, subscription.ScopeProject)
		}
	}
	sort.Strings(words)
	t.Logf("осмотрено видов %d: %v", len(kinds), words)
}

// TestStorageJournal_UK313a_EmptyAnchorOfAProjectKindIsRefusedByName — УК3-13 (а),
// CX3B-31: функция фундамента с журналом storage отказывает проектному виду с
// пустым якорем по ИМЕНИ вида; близнец с якорем ложится. Отличие одно — якорь.
func TestStorageJournal_UK313a_EmptyAnchorOfAProjectKindIsRefusedByName(t *testing.T) {
	s := newStand(t)
	j := subscriptionjournal.Journal(false)
	if len(j.Mapping.Kinds) == 0 {
		t.Fatal("словарь видов пуст — проба судила бы пустоту")
	}
	for word := range j.Mapping.Kinds {
		t.Run(word, func(t *testing.T) {
			id := ids.NewHyphenID("uk3")
			bare := emitViaFoundation(t, s, j, subscription.Entry{Kind: word, ID: id, Change: "CREATED",
				Payload: map[string]any{"id": id}})
			wantText := "пустой якорь у проектного вида " + word
			if !errors.Is(bare, subscription.ErrEntryRefused) || !strings.Contains(bare.Error(), wantText) {
				t.Errorf("пустой якорь: ожидался отказ %q, получено %v", wantText, bare)
			}
			if err := emitViaFoundation(t, s, j, subscription.Entry{Kind: word, ID: id, ProjectID: probeProject,
				Change: "CREATED", Payload: map[string]any{"id": id}}); err != nil {
				t.Errorf("близнец с якорем отвергнут: %v", err)
			}
		})
	}
}

func emitViaFoundation(t *testing.T, s *stand, j subscription.Journal, e subscription.Entry) error {
	t.Helper()
	ctx := journalPrincipalCtx(context.Background())
	tx, err := journaltx.Begin(ctx, s.pool, journaltx.NewOptions(false))
	if err != nil {
		t.Fatalf("фикстура: транзакция помощника не открылась: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := j.Emit(ctx, tx, e); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("фикстура: транзакция не зафиксировалась: %v", err)
	}
	return nil
}

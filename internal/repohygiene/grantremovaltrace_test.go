// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// grantremovaltrace_test.go — гейт: миграция, снимающая выдачи, оставляет след.
//
// Предмет, единица счёта, довод в пользу храповика и граница разобраны в шапке
// grantremovaltrace.go — здесь они не пересказываются, чтобы не завести двух
// мест об одном предмете.
//
// Доказательство способности упасть и смолчать — в
// grantremovaltrace_injection_test.go.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// TestMigrationRemovingGrantsLeavesATrace — IAM-RM-1-11.
func TestMigrationRemovingGrantsLeavesATrace(t *testing.T) {
	root := repoRoot(t)

	paths, err := treecorpus.UnderWithSuffix(
		filepath.Join(root, filepath.FromSlash(grantRemovalMigrationsDir)), ".sql")
	if err != nil {
		t.Fatalf("ПРЕДПОСЫЛКА ЛОЖНА: корпус миграций %s не перечислен: %v — "+
			"«ноль находок» ниже означало бы «ноль прочитанного»",
			grantRemovalMigrationsDir, err)
	}

	corpus, err := readMigrationCorpus(paths, os.ReadFile)
	if err != nil {
		t.Fatalf("ПРЕДПОСЫЛКА ЛОЖНА: файл корпуса не читается: %v", err)
	}

	silent, c := auditGrantRemovalTrace(corpus)

	// Перепись печатается ДО вердикта: она обязана быть видна и на зелёном
	// прогоне, иначе «ноль находок» неотличимо от «ноль прочитанного».
	t.Logf("перепись: файлов корпуса прочитано %d; с удалением выдач где угодно %d; "+
		"из них в накатной половине %d; из них без следа %d (храповик %d)",
		c.FilesRead, c.WithDelete, c.InUpHalf, len(silent), grantRemovalRatchet)
	if len(silent) > 0 {
		t.Logf("прощённые сегодня: %s", strings.Join(silent, ", "))
	}

	// ПРЕДПОСЫЛКИ. Каждая — факт, который может измениться и сделать вердикт
	// беспредметным, поэтому гейт проверяет их сам, а не подразумевает.
	if c.FilesRead == 0 {
		t.Fatalf("ПРЕДПОСЫЛКА ЛОЖНА: в %s не прочитано ни одного файла", grantRemovalMigrationsDir)
	}
	if c.WithDelete == 0 {
		t.Fatal("ПРЕДПОСЫЛКА ЛОЖНА: во всём корпусе не найдено НИ ОДНОГО удаления строк " +
			"kacho_iam.access_bindings — ни в накатной половине, ни в откатной. Так " +
			"выглядит переставший узнавать оператор распознаватель, а не корпус без " +
			"снятий: удаление стоит в откатной половине у каждой миграции, которая " +
			"выдачи вставляет")
	}

	if len(silent) != grantRemovalRatchet {
		t.Error(grantRemovalFinding(len(silent), silent))
	}
}

// TestNoMigrationMovesGrantsBetweenRoles — IAM-RM-1-13, характеризующий замок.
//
// Дерево уже даёт это поведение; проба обязана его ПЕРЕЖИТЬ, а не покраснеть.
// Требовать от неё красноты запрещено: она утверждает, что отвергнутый исход
// («перенести выдачи на роли-преемники») не исполнялся ни разу, а не что кто-то
// его исполнил.
func TestNoMigrationMovesGrantsBetweenRoles(t *testing.T) {
	root := repoRoot(t)

	paths, err := treecorpus.UnderWithSuffix(
		filepath.Join(root, filepath.FromSlash(grantRemovalMigrationsDir)), ".sql")
	if err != nil {
		t.Fatalf("ПРЕДПОСЫЛКА ЛОЖНА: корпус миграций не перечислен: %v", err)
	}
	corpus, err := readMigrationCorpus(paths, os.ReadFile)
	if err != nil {
		t.Fatalf("ПРЕДПОСЫЛКА ЛОЖНА: файл корпуса не читается: %v", err)
	}

	moves, statements := auditGrantRoleReassignment(corpus)
	t.Logf("перепись: файлов корпуса %d; операторов «UPDATE access_bindings … SET» "+
		"в накатной половине %d; из них переставляющих role_id %d",
		len(corpus), statements, len(moves))

	if statements == 0 {
		t.Fatal("ПРЕДПОСЫЛКА ЛОЖНА: во всём корпусе не прочитано НИ ОДНОГО оператора " +
			"правки выдачи — так выглядит ослепший распознаватель, а не корпус без " +
			"переносов: мягкий отзыв правит выдачу в каждой миграции, которая её снимает")
	}
	if len(moves) > 0 {
		t.Errorf("миграция переставляет выдачу с одной роли на другую: %s. "+
			"Это ТИХОЕ расширение прав: выдача на снятую роль дала бы доступ к ресурсу, "+
			"которого выдававший НЕ НАЗЫВАЛ, а согласия у него никто не спрашивал. "+
			"Исход отвергнут приёмкой §2.5; законный путь — снять выдачу и оставить след",
			strings.Join(moves, ", "))
	}
}

// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// systemrolerowisneverdeleted_test.go — гейт решения «отзыв роли модуля — это
// ПОМЕТКА, а не удаление строки» (запись решения
// `services/iam/docs/engineering/architecture/role-withdrawal-is-a-mark.md`,
// задача продукта #1913).
//
// Способность гейта упасть и смолчать доказана инъекцией —
// `systemrolerowisneverdeleted_injection_test.go`.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	// iamGoPrefix — дерево сервиса, которому принадлежит таблица ролей.
	iamGoPrefix = "services/iam/"
	// roleDeleteCensusFloor — прод-файлов Go, ниже которого обход беспредметен.
	// Замер на `lane/r1`: 518. Порог взят с запасом вниз — он стережёт обвал
	// обхода, а не рост дерева.
	roleDeleteCensusFloor = 300
)

// roleDeleteFindings — предикат находки. Тот же зовёт инъекция.
func roleDeleteFindings(sites []RoleDeleteSite) []string {
	out := make([]string, 0, len(sites))
	for _, s := range sites {
		out = append(out, fmt.Sprintf("%s:%d  %s", s.File, s.Line, s.What))
	}
	sort.Strings(out)
	return out
}

// TestSystemRoleRowIsNeverDeleted — сам гейт.
func TestSystemRoleRowIsNeverDeleted(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	rels := make([]string, 0, 512)
	for rel := range tt.files {
		if !strings.HasPrefix(rel, iamGoPrefix) || !strings.HasSuffix(rel, ".go") {
			continue
		}
		if strings.HasSuffix(rel, "_test.go") {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var (
		parsed     int
		lits       int
		comments   int
		statements int
		guarded    int
		sites      []RoleDeleteSite
	)
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		s, census, err := ScanRoleDeletes(rel, src)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		parsed++
		lits += census.StringLiterals
		comments += census.Comments
		statements += census.Statements
		guarded += census.Guarded
		sites = append(sites, s...)
	}

	t.Logf("перепись: прод-файлов Go %s разобрано %d, строковых литералов %d, "+
		"комментариев %d, операторов удаления над `roles` прочитано %d, "+
		"из них сужены на пользовательскую роль %d, находок %d",
		iamGoPrefix, parsed, lits, comments, statements, guarded, len(sites))

	if parsed < roleDeleteCensusFloor {
		t.Fatalf("перепись обвалилась: разобрано %d прод-файлов при пороге %d — обход "+
			"читает не то дерево, и «ноль находок» здесь неотличимо от «ноль прочитанного»",
			parsed, roleDeleteCensusFloor)
	}
	if lits == 0 || comments == 0 {
		t.Fatalf("прочитано литералов %d, комментариев %d — различение «код против прозы» "+
			"беспредметно, а вместе с ним и обе половины гейта", lits, comments)
	}

	// ПРЕДПОСЫЛКА ГЕЙТА. Он утверждает свойство операторов удаления над таблицей
	// ролей; ноль таких операторов означает, что судить нечего, и молчание было
	// бы сказано ни о чём. Ноль здесь — находка, а не идеал: удаление роли
	// исчезло из прод-кода целиком, значит предмет гейта переехал, и гейт
	// снимается вместе с ним, а не остаётся зелёным навсегда.
	if statements == 0 {
		t.Fatalf("операторов удаления над `roles` в прод-коде %s прочитано НОЛЬ — предмет "+
			"гейта исчез (прочитано файлов %d, литералов %d). Это отказ, а не чистота: "+
			"проверка, которой нечего искать, неотличима от проверки, ничего не нашедшей",
			iamGoPrefix, parsed, lits)
	}

	if findings := roleDeleteFindings(sites); len(findings) > 0 {
		t.Fatalf("строку СИСТЕМНОЙ роли удаляет прод-код — %d место(а):\n  %s\n\n"+
			"Всякая роль, объявленная манифестом модуля, системная by construction "+
			"(`moduleroles/apply.go` ставит `IsSystem: true`, а `roles.is_system` "+
			"вычисляется из `cluster_id`). Оператор удаления, не сужённый на "+
			"пользовательскую роль, делает отзыв роли модуля выразимым УДАЛЕНИЕМ "+
			"строки — а решение линии обратное: отзыв есть ПОМЕТКА, строка остаётся.\n"+
			"Цена удаления названа замером, а не предположена: выдачи ссылаются на "+
			"роль ключом `access_bindings_role_fk … ON DELETE RESTRICT` (роль в "+
			"работе не удалится вовсе), а три проекции — селекторы, глаголы и "+
			"сегменты правила — уехали бы каскадом МОЛЧА.\n"+
			"Решение и его довод по пяти осям: "+
			"services/iam/docs/engineering/architecture/role-withdrawal-is-a-mark.md",
			len(findings), strings.Join(findings, "\n  "))
	}
}

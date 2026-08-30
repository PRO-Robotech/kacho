// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migratorapplyproducer_test.go — у доказательства наката миграций обязан быть
// производитель.
//
// # Предмет
//
// Отказ тракта миграций означает «сервис не разворачивается». До задачи #1637 его
// не проверял НИ ОДИН процесс конвейера: `grep -rl migrator .github/workflows/`
// давал ноль, а отбор интеграционной джобы сужен четырьмя каталогами внутри
// сервиса (`/internal/(repo|clients|reconciler|subscriptionjournal)`), и ни одна из
// семи точек наката — ни `services/*/cmd/migrator`, ни `services/*/internal/apps/
// migrator` — в него не входит.
//
// Отсюда предмет: не «написать пробу», а дать пробе ПРОИЗВОДИТЕЛЯ. Проба без
// производителя стала бы ровно тем классом, ради которого этот каталог существует, —
// формой без содержания: зелёной всегда, потому что не исполняемой никогда.
//
// # Почему пакет доказательства ОДИН, а не семь
//
// Правило `internal` языка запрещает пакету из корня импортировать
// `services/<svc>/internal/migrations`: такой импорт разрешён только пакетам,
// укоренённым в `services/<svc>`. Значит общий пакет НЕ МОЖЕТ дотянуться до цепочек
// семи сервисов через импорт — это построение, а не неудобство.
//
// Обойти его можно ровно одним способом, и он же оказывается самым полным:
// доказательство СОБИРАЕТ бинарь каждой точки наката и запускает его. Тогда
// проверяется весь тракт — разбор аргументов, резолв DSN, встроенная FS, диалект,
// барьер готовности базы, счёт перед сносом, накат goose, — а не та его половина,
// до которой дотянулся бы импорт. Заодно перечень точек наката ВЫВОДИТСЯ из дерева
// (`go list ./services/*/cmd/migrator`), а не выписывается: новый сервис попадает под
// доказательство сам, и слепой зоны «пробу забыли дописать» не заводится.
//
// # Чего этот гейт НЕ проверяет — и кто проверяет
//
// Провязку «запись shortGatedRunByOwnCIStep ↔ перечень PG_OUTSIDE_SELECTION_PKGS»
// держит pgoutsideselection_test.go, в обе стороны; что строка запуска цели стоит
// в ci.yaml — shortgatedselection_test.go. Здесь это НЕ пересказывается: два места
// об одном предмете расходятся молча.
//
// Незакрытым оставался стык между ними: обе сверки остаются зелёными, если запись
// снять ИЗ ОБОИХ перечней разом — тогда доказательство просто перестаёт исполняться,
// и не краснеет ничего. Этот гейт закрывает именно его: он требует, чтобы
// доказательство наката было названо производителем, а не чтобы два перечня
// сходились между собой.
//
// # Перепись
//
// «Ноль находок» обязано отличаться от «ноль прочитанного»: гейт печатает, сколько
// точек наката осмотрел и сколькие покрыты доказательством, и падает на пустом обходе.
package repohygiene

import (
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// migratorApplyPoints — точки наката, найденные обходом дерева. Перечень
// ВЫВОДИТСЯ, а не выписывается: выписанный разошёлся бы с деревом молча, и
// разошёлся бы именно на новом сервисе — там, где слепая зона дороже всего.
func migratorApplyPoints(t *testing.T, tt *trackedTree) []string {
	t.Helper()
	seen := map[string]bool{}
	for rel := range tt.files {
		// Точка наката — КАТАЛОГ, а индекс git версионирует файлы: каталог берётся
		// у путей, а не спрашивается у диска. Спросить диск значило бы засчитать
		// неотслеживаемый каталог (рабочая копия соседа, распаковка) точкой наката
		// и потребовать доказательства от того, чего в репозитории нет.
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, "services/") {
			continue
		}
		dir := path.Dir(rel)
		if strings.HasSuffix(dir, "/cmd/migrator") {
			seen[dir] = true
		}
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// migratorApplyProofTestFiles — сколько файлов проб несёт пакет доказательства.
// Ноль означает и «пакета нет», и «пакет пуст»: для предиката это один случай —
// доказывать нечем.
func migratorApplyProofTestFiles(t *testing.T, tt *trackedTree) int {
	t.Helper()
	n := 0
	for rel := range tt.files {
		rel = filepath.ToSlash(rel)
		if path.Dir(rel) == migratorApplyProofPkg && strings.HasSuffix(rel, "_test.go") {
			n++
		}
	}
	return n
}

// TestMigratorApplyProofHasAProducer — доказательство наката существует и его
// кто-то гоняет.
func TestMigratorApplyProofHasAProducer(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	migrators := migratorApplyPoints(t, tt)
	proofTests := migratorApplyProofTestFiles(t, tt)
	producer := makefileListedPkgs(t, root, "PG_OUTSIDE_SELECTION_PKGS")

	covered := 0
	if proofTests > 0 {
		// Доказательство выводит перечень точек наката из дерева тем же обходом,
		// поэтому покрытие у него полное by construction: сузить его молча нельзя —
		// сузился бы сам обход, а он печатает своё число в прогоне цели.
		covered = len(migrators)
	}
	t.Logf("перепись: осмотрено отслеживаемых файлов %d; миграторов %d, покрыто пробой %d; "+
		"файлов проб в %s — %d; записей PG_OUTSIDE_SELECTION_PKGS — %d",
		tt.count(), len(migrators), covered, migratorApplyProofPkg, proofTests, len(producer))

	if len(producer) == 0 {
		t.Fatal("PG_OUTSIDE_SELECTION_PKGS корневого Makefile не прочитан или пуст — " +
			"вердикт о производителе выносить не из чего")
	}

	for _, f := range judgeMigratorApplyProducer(migrators, proofTests, producer) {
		t.Errorf("%s", f)
	}
}

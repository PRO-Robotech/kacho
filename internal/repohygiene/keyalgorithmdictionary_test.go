// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// keyalgorithmdictionary_test.go — словарь зарегистрированного алгоритма в
// СХЕМЕ совпадает с закрытым перечнем КОДА (приёмка F2, §9.4).
//
// # Предмет
//
// Одно множество объявлено дважды: ограничением схемы и перечнем проверяющего.
// Приёмка требует их совпадения и говорит прямо, чем оно держится: ГЕЙТОМ, а не
// совпадением формулировок. Разойдясь, они дают одно из двух — строку, которую
// схема примет, а проверяющий не признает (клиент заведён и аутентифицироваться
// не может), либо алгоритм, который проверяющий считает допустимым, а вставить
// его нельзя.
//
// # Пустое значение схемы алгоритмом НЕ является
//
// Ограничение допускает пустую строку, и это законный вход, означающий «ключа
// нет». Гейт обязан это РАЗЛИЧАТЬ, а не считать расхождением: сложив пустое
// значение с алгоритмами, он объявил бы отсутствие ключа одним из них.
//
// Пропажа пустого значения из словаря — НАХОДКА, а не улучшение: на нём стоит
// целый вид клиента (заведённый без ключевого материала), и его исчезновение
// означает, что состояние, которое читатель считает законным, схема больше не
// допускает.
//
// Исключение из этого — ограничения, перечисленные в keyAlgorithmKeyRequired:
// таблицы, где ключевой материал ОБЯЗАТЕЛЕН по решению, и «ключа нет» состоянием
// строки не является вовсе. Запись самоистекает: ограничение, которое перестало
// существовать либо начало допускать пустое, — находка.
//
// # Что здесь считается деревом
//
// Индекс git — то же множество, которое увидит свежий клон и CI.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/tokenpolicy"
)

const (
	// keyAlgorithmMigrations — каталог миграций, где живёт словарь.
	keyAlgorithmMigrations = "services/iam/internal/migrations/"
	// keyAlgorithmColumn — столбец, чей словарь стережётся.
	keyAlgorithmColumn = "key_algorithm"
	// keyAlgorithmConstraintFloor — сколько ограничений обязано быть найдено.
	// Ключевой материал лежит у ДВУХ таблиц из трёх (приёмка §1.1), и словарь
	// объявлен у каждой; ноль означал бы, что разбор перестал видеть предмет.
	keyAlgorithmConstraintFloor = 1
)

// keyAlgorithmKeyRequired — ограничения, у которых пустое значение НЕ законно.
//
// Общее правило («пустое означает ключа нет и обязано допускаться») выведено из
// таблиц клиентов: там ключевой материал есть не у всякой строки. Есть таблицы
// с обратным решением, и у них пустое значение означало бы не «ключа нет», а
// «принимаем без проверки подписи».
//
// Ключ — имя ограничения; значение — причина. Перечень закрыт: ограничение,
// которого здесь нет, судится общим правилом.
var keyAlgorithmKeyRequired = map[string]string{
	"federated_trusted_issuers_alg_ck": "" +
		"перечень доверенных издателей (задача #1124): запись доверия БЕЗ ключа издателя " +
		"не отвергала бы ничего — она принимала бы всё, что называет её пару, то есть " +
		"доверие издателю выродилось бы в доверие строке таблицы. «Ключа нет» здесь не " +
		"состояние строки, а её отсутствие: строки без ключа не существует",
}

// TestKeyAlgorithmDictionaryMatchesTheCode — сам гейт.
func TestKeyAlgorithmDictionaryMatchesTheCode(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	code := append([]string(nil), tokenpolicy.Algorithms()...)
	sort.Strings(code)

	// (1) Предпосылка: перечень кода непуст. Пустой перечень означал бы, что
	// проверяющий не принимает ничего, и «совпадение со схемой» сказано ни о чём.
	if len(code) == 0 {
		t.Fatal("закрытый перечень алгоритмов кода ПУСТ — проверяющий не принимает ни " +
			"одного алгоритма, и сверять со схемой нечего")
	}
	for _, a := range code {
		if strings.TrimSpace(a) == "" {
			t.Fatalf("перечень кода содержит пустое значение (%v). Пустое означает «ключа "+
				"нет», а не «любой алгоритм»: приняв его алгоритмом, проверяющий перестал бы "+
				"сужать вовсе", code)
		}
	}

	var files []string
	for rel := range tt.files {
		if strings.HasPrefix(rel, keyAlgorithmMigrations) && strings.HasSuffix(rel, ".sql") {
			files = append(files, rel)
		}
	}
	// Порядок номера — тот же, в котором миграции применяет накат.
	sort.Slice(files, func(i, j int) bool {
		return migrationOrdinal(files[i]) < migrationOrdinal(files[j])
	})

	var census AlgorithmDictionaryCensus
	live := map[string]AlgorithmConstraint{}
	for _, rel := range files {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		census.Files++
		found, dropped, c := ScanKeyAlgorithmConstraints(rel, string(body), keyAlgorithmColumn)
		census.Statements += c.Statements
		census.Drops += c.Drops
		for _, f := range found {
			live[f.Name] = f
		}
		for _, name := range dropped {
			delete(live, name)
		}
	}

	var names []string
	for n := range live {
		names = append(names, n)
	}
	sort.Strings(names)

	t.Logf("перепись: файлов миграций прочитано %d, объявлений словаря столбца %q найдено %d, "+
		"снятий ограничения %d, действующих ограничений %d (%s); перечень кода: %v",
		census.Files, keyAlgorithmColumn, census.Statements, census.Drops,
		len(live), strings.Join(names, ", "), code)

	if census.Files == 0 {
		t.Fatalf("в %s не прочитано ни одного файла миграции — гейт не может назвать схему, "+
			"о которой он говорит", keyAlgorithmMigrations)
	}
	// (2) Предпосылка: словарь в схеме ВЫРАЖЕН. Ноль ограничений означает, что
	// столбец больше ничем не сужен, — и это само по себе находка: значение,
	// которое схема не сужает, означает «любое».
	if len(live) < keyAlgorithmConstraintFloor {
		t.Fatalf("действующих объявлений словаря столбца %q в схеме %d при пороге %d "+
			"(найдено объявлений %d, снято %d).\n\n"+
			"Столбец, который схема не сужает, принимает ЛЮБОЕ значение: словарь перестал "+
			"быть выражен, и совпадать с перечнем кода нечему. Молчание гейта здесь было бы "+
			"сказано о разборе, а не о схеме.",
			keyAlgorithmColumn, len(live), keyAlgorithmConstraintFloor,
			census.Statements, census.Drops)
	}

	// (3) Находка: словарь схемы разошёлся с перечнем кода.
	var problems []string
	seenKeyRequired := map[string]bool{}
	for _, name := range names {
		c := live[name]
		algorithms, hasEmpty := SplitAlgorithmValues(c.Values)

		reason, keyRequired := keyAlgorithmKeyRequired[name]
		switch {
		case !hasEmpty && !keyRequired:
			problems = append(problems, fmt.Sprintf(
				"%s:%d %s — словарь БОЛЬШЕ НЕ ДОПУСКАЕТ пустое значение (%v). Пустое значение "+
					"означает «ключа нет», и на нём стоит целый вид клиента, заведённый без "+
					"ключевого материала: его исчезновение означает, что состояние, которое "+
					"читатель считает законным, схема больше не допускает",
				c.File, c.Line, name, c.Values))
		case hasEmpty && keyRequired:
			// Запись самоистекает: ограничение начало допускать пустое, значит
			// решение, ради которого запись стояла, отменено — а отменять его
			// молча нельзя, иначе следующая слепая зона унаследует запись.
			problems = append(problems, fmt.Sprintf(
				"%s:%d %s — ограничение объявлено требующим ключа, а словарь ДОПУСКАЕТ пустое "+
					"значение (%v). Причина записи: %s",
				c.File, c.Line, name, c.Values, reason))
		case keyRequired:
			seenKeyRequired[name] = true
		}

		extra := setDifference(algorithms, code)
		missing := setDifference(code, algorithms)
		if len(extra) == 0 && len(missing) == 0 {
			continue
		}
		var parts []string
		if len(extra) > 0 {
			parts = append(parts, fmt.Sprintf("схема допускает сверх кода: %v", extra))
		}
		if len(missing) > 0 {
			parts = append(parts, fmt.Sprintf("код допускает сверх схемы: %v", missing))
		}
		problems = append(problems, fmt.Sprintf("%s:%d %s — %s (схема %v, код %v)",
			c.File, c.Line, name, strings.Join(parts, "; "), algorithms, code))
	}

	// Запись о требуемом ключе живёт, ПОКА живо её ограничение. Оставленная без
	// предмета, она молча накроет следующее ограничение того же имени.
	for name, reason := range keyAlgorithmKeyRequired {
		if !seenKeyRequired[name] {
			problems = append(problems, fmt.Sprintf(
				"запись о требуемом ключе %q больше нечего исключать: действующего ограничения "+
					"с таким именем в схеме нет. Причина записи: %s", name, reason))
		}
	}

	if len(problems) > 0 {
		t.Fatalf("словарь алгоритма в схеме разошёлся с закрытым перечнем кода — %d находка(и):\n  %s\n\n"+
			"Совпадение двух объявлений одного множества держится ГЕЙТОМ, а не совпадением "+
			"формулировок. Расхождение даёт одно из двух: строку, которую схема примет, а "+
			"проверяющий не признает — клиент заведён и аутентифицироваться не может; либо "+
			"алгоритм, который проверяющий считает допустимым, а вставить его нельзя.\n"+
			"Снятие: НОВАЯ миграция, приводящая ограничение к перечню кода (применённая не "+
			"правится, ban #5), либо правка перечня кода, если решение принято в его пользу.",
			len(problems), strings.Join(problems, "\n  "))
	}

	for _, name := range names {
		c := live[name]
		algorithms, hasEmpty := SplitAlgorithmValues(c.Values)
		t.Logf("%s:%d %s — алгоритмы %v, пустое значение допускается: %v",
			c.File, c.Line, name, algorithms, hasEmpty)
	}
}

// migrationOrdinal — числовой префикс имени миграции; им же задан порядок наката.
func migrationOrdinal(rel string) int {
	base := filepath.Base(rel)
	n := 0
	for i := 0; i < len(base); i++ {
		if base[i] < '0' || base[i] > '9' {
			break
		}
		n = n*10 + int(base[i]-'0')
	}
	return n
}

// setDifference — элементы a, которых нет в b.
func setDifference(a, b []string) []string {
	have := map[string]bool{}
	for _, x := range b {
		have[x] = true
	}
	var out []string
	for _, x := range a {
		if !have[x] {
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// sqlstatehome_test.go — отказ хранилища разбирается ОДНИМ домом, а ветка по
// умолчанию отвечает ФИКСИРОВАННЫМ текстом.
//
// # Предмет
//
// Корпус правил формулирует отображение как ОДНО правило: нарушение внешнего
// ключа — одно, уникальности — другое, проверки — третье, исключения —
// четвёртое (`data-integrity.md` §«Within-service инварианты»). Пока исполнений
// много, различие между ними НЕ ВЫРАЖЕНО и потому не может покраснеть: каждое по
// отдельности защитимо, а первая же правка одного до остальных не доезжает —
// и не доезжает молча.
//
// Второе свойство того же предмета: ветка по умолчанию обязана давать
// фиксированный непрозрачный текст (`security.md` §Hardening-инварианты, п.1).
// Сегодня оно держится вниманием СТОЛЬКО РАЗ, сколько в дереве отображений, и
// каждое из них — отдельный случай не ошибиться.
//
// # Что здесь считается находкой
//
// Не «код упомянут», а «по коду принято решение». Токен `"23505"` в этом дереве
// стоит преимущественно в прозе: замер на ревизии заведения — 85 файлов с
// токеном, 18 принимают решение, 67 документируют маршрут комментарием. Разбор
// читает строковый литерал и комментария не видит by construction.
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
)

const (
	// sqlStateHome — единственный дом решения о классе отказа.
	sqlStateHome = "pkg/db/pgfault/"
	// sqlStateCensusFloor — порог переписи: ниже него «ноль находок» означало бы
	// «ноль прочитанного». Величина взята НЕ из сегодняшнего числа файлов Go
	// (их на порядок больше) — она отвечает на вопрос «сколько файлов обязано
	// быть прочитано, чтобы молчание что-то значило», и потому лежит заведомо
	// ниже фактического объёма: порог, подогнанный под текущее дерево, зеленел
	// бы ровно до следующего коммита.
	sqlStateCensusFloor = 500
	// sqlStateLiteralFloor — порог осмотренных строковых литералов. Отдельное
	// число, потому что файлы можно прочитать и не разобрать: ноль литералов при
	// тысяче файлов означает сломанный разбор, а не чистое дерево.
	sqlStateLiteralFloor = 5000
	// sqlStateHomeCodesFloor — сколько кодов дом обязан объявлять. Это
	// ПОЛОЖИТЕЛЬНЫЙ контроль предиката: гейт, чей предмет — отсутствие, молчит и
	// когда предмет исчез, и когда сломался поиск. Дом — место, которое
	// распознаватель ОБЯЗАН находить.
	sqlStateHomeCodesFloor = 9
)

// sqlStateException — отступление: место, которому позволено решать по коду
// самому.
//
// Отступление называет ПРИЧИНУ и живёт, пока у него есть предмет. Запись,
// которой больше нечего исключать, — находка: она унаследует следующую слепую
// зону.
type sqlStateException struct {
	// File — путь в дереве.
	File string
	// Func — имя функции; отступление даётся функции, а не файлу. У одного файла
	// бывает и переведённая часть, и та, что законно осталась своей, — послабление
	// по файлу простило бы обе.
	Func string
	// Why — чем это место отличается от общего правила.
	Why string
}

// sqlStateExceptions — ведомость отступлений.
//
// Пусто здесь — норма и цель, а не поломка: гейт на пустой ведомости проходит.
var sqlStateExceptions = []sqlStateException{
	{
		File: "internal/repohygiene/sqlstatehome.go",
		Func: "",
		Why: "перечень кодов самого разбора: узнавание формы неотличимо от её " +
			"употребления, и без самоисключения гейт находил бы себя",
	},
}

// TestIntegritySQLStateIsDecidedInOnePlace — сам гейт, сторона первая: МЕСТО
// решения.
func TestIntegritySQLStateIsDecidedInOnePlace(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	var rels []string
	for rel := range tt.files {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		if skipPath(rel) {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var (
		parsed, literals, funcs int
		inHome, outside         []SQLStateSite
	)
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		sites, census, err := ScanSQLStateLiterals(rel, src)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		parsed++
		literals += census.Literals
		funcs += census.Funcs
		for _, s := range sites {
			if strings.HasPrefix(s.File, sqlStateHome) {
				inHome = append(inHome, s)
				continue
			}
			outside = append(outside, s)
		}
	}

	t.Logf("перепись: не-тестовых файлов Go разобрано %d, объявлений функций прочитано %d, "+
		"строковых литералов осмотрено %d, кодов целостности найдено %d "+
		"(в доме %s — %d, вне дома — %d), отступлений объявлено %d",
		parsed, funcs, literals, len(inHome)+len(outside),
		sqlStateHome, len(inHome), len(outside), len(sqlStateExceptions))

	// (1) Предпосылка: дерево вообще прочитано.
	if parsed < sqlStateCensusFloor {
		t.Fatalf("перепись обвалилась: разобрано %d файлов при пороге %d — на таком "+
			"объёме «ноль находок» означало бы «ноль прочитанного»", parsed, sqlStateCensusFloor)
	}
	if literals < sqlStateLiteralFloor {
		t.Fatalf("осмотрено %d строковых литералов при пороге %d — разбор перестал видеть "+
			"предмет, и его молчание сказано ни о чём", literals, sqlStateLiteralFloor)
	}

	// (2) ПОЛОЖИТЕЛЬНЫЙ контроль: распознаватель обязан находить дом. Гейт, чей
	// предмет — отсутствие, молчит одинаково и когда предмет исчез, и когда
	// сломался он сам; эта проверка разводит два молчания.
	homeCodes := map[string]bool{}
	for _, s := range inHome {
		homeCodes[s.Code] = true
	}
	if len(homeCodes) < sqlStateHomeCodesFloor {
		t.Fatalf("в доме %s распознано %d различных кодов при ожидаемых %d — либо дом "+
			"перестал объявлять правило целиком, либо распознаватель ослеп. В обоих случаях "+
			"молчание этого гейта о дереве не свидетельствует.",
			sqlStateHome, len(homeCodes), sqlStateHomeCodesFloor)
	}

	// (3) Отступления: снимаем объявленные, остальное — находка.
	allowed := map[string]sqlStateException{}
	for _, e := range sqlStateExceptions {
		allowed[e.File+"\x00"+e.Func] = e
	}
	used := map[string]bool{}
	var findings []string
	for _, s := range outside {
		if e, ok := allowed[s.File+"\x00"+s.Func]; ok {
			used[e.File+"\x00"+e.Func] = true
			continue
		}
		if e, ok := allowed[s.File+"\x00"]; ok { // отступление, данное файлу целиком
			used[e.File+"\x00"] = true
			continue
		}
		findings = append(findings, fmt.Sprintf("%s:%d %s() решает по коду %s (%s)",
			s.File, s.Line, s.Func, s.Code, IntegritySQLStates[s.Code]))
	}

	// (4) Самоистечение: отступление, которому нечего исключать, — находка.
	for _, e := range sqlStateExceptions {
		if !used[e.File+"\x00"+e.Func] {
			findings = append(findings, fmt.Sprintf(
				"отступление %s %s() потеряло предмет: кодов целостности там больше нет — "+
					"снимите запись, иначе она унаследует следующую слепую зону",
				e.File, e.Func))
		}
	}

	if len(findings) > 0 {
		sort.Strings(findings)
		t.Fatalf("решение о классе отказа хранилища принимается не только в доме %s "+
			"(находок %d):\n  %s\n\nОдно правило, много исполнений: различие между ними НЕ ВЫРАЖЕНО, "+
			"поэтому первая же правка одного до остальных не доедет и не доедет молча. "+
			"Переведите место на дом либо объявите отступление с причиной в sqlStateExceptions.",
			sqlStateHome, len(findings), strings.Join(findings, "\n  "))
	}
}

// TestErrorMappersTailReturnsAFixedText — сторона вторая: ТЕКСТ ветки по
// умолчанию.
//
// Отображение опознаётся ИСХОДОМ (производит больше одного кода gRPC), а не
// именем: имя задаёт автор, и восьмое отображение он назовёт иначе.
func TestErrorMappersTailReturnsAFixedText(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	var rels []string
	for rel := range tt.files {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		if skipPath(rel) {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var (
		parsed, funcs, errTaking, statusCalls int
		mappers                               []StatusMapper
	)
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		ms, census, err := ScanStatusMappers(rel, src)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		parsed++
		funcs += census.Funcs
		errTaking += census.ErrTaking
		statusCalls += census.StatusCall
		mappers = append(mappers, ms...)
	}

	var withStatusTail int
	for _, m := range mappers {
		if m.TailIsStatus {
			withStatusTail++
		}
	}

	t.Logf("перепись: не-тестовых файлов Go разобрано %d, объявлений функций прочитано %d "+
		"(из них принимающих error — %d), вызовов status.Error/Errorf осмотрено %d, "+
		"отображений «ошибка → код gRPC» найдено %d, из них с терминальным возвратом-статусом %d",
		parsed, funcs, errTaking, statusCalls, len(mappers), withStatusTail)

	if parsed < sqlStateCensusFloor {
		t.Fatalf("перепись обвалилась: разобрано %d файлов при пороге %d", parsed, sqlStateCensusFloor)
	}
	// ПОЛОЖИТЕЛЬНЫЙ контроль: отображения обязаны находиться. Ноль означает не
	// «дерево чисто», а «распознаватель ослеп»; такой гейт молчал бы навсегда.
	if errTaking == 0 {
		t.Fatalf("функций, принимающих error, найдено НОЛЬ на %d файлах — разбор сигнатур "+
			"сломан, и знаменателя у предмета нет", parsed)
	}
	if withStatusTail == 0 {
		t.Fatalf("отображений с терминальным возвратом-статусом найдено НОЛЬ на %d файлах "+
			"(функций, принимающих error, — %d) — распознаватель перестал видеть предмет. "+
			"Его молчание не свидетельствует о дереве.", parsed, errTaking)
	}

	var findings []string
	for _, m := range mappers {
		if !m.TailIsStatus || m.TailFixed {
			continue
		}
		findings = append(findings, fmt.Sprintf(
			"%s:%d %s() (производит %d кодов gRPC): ветка по умолчанию на строке %d выносит наружу %s",
			m.File, m.Line, m.Func, m.Codes, m.TailLine, m.TailText))
	}

	if len(findings) > 0 {
		sort.Strings(findings)
		t.Fatalf("ветка по умолчанию отображения ошибки отдаёт производный текст (находок %d):\n  %s\n\n"+
			"Отображение доходит до этой ветки ровно тогда, когда род отказа не распознан, — "+
			"то есть с ошибкой, о содержании которой мы ничего не знаем. Текст СУБД несёт имя узла, "+
			"базу, пользователя и обрывок запроса (`security.md` §Hardening-инварианты, п.1). "+
			"Отдайте фиксированный текст (`pgfault.OpaqueMessage` либо ярлык оператора).",
			len(findings), strings.Join(findings, "\n  "))
	}
}

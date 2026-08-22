// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// signedmaterialtypes_test.go — объявленные типы обоих видов подписанного
// объявлены В ОДНОМ месте и ПОПАРНО РАЗЛИЧНЫ (приёмка F2, сценарий F2-36).
//
// # Предмет
//
// Виды подписанного разделены ТРЕМЯ независимыми признаками, и объявленный тип
// — первый из них. Единственным его назначать нельзя (клиент, тип не
// проставивший, снял бы его целиком), но и потерять нельзя: требование
// выполняется каждым признаком ПО ОТДЕЛЬНОСТИ.
//
// Совпади два значения по недосмотру — одно из двух направлений разделения
// перестало бы работать МОЛЧА: положительный путь при этом зелен, потому что
// оставшиеся два признака его держат. Различие двух значений обязано быть
// выражено там, где оно может покраснеть.
//
// # Ведомость вторых объявлений — счётчик, а не список прощённых
//
// Значение токена доступа объявлено в дереве не только политикой: две
// поверхности объявили его у себя до того, как политика появилась. Записи
// ведомости ниже несут причину и предикат снятия; НОВОЕ второе объявление
// роняет прогон немедленно, а запись, которой больше нечего исключать, роняет
// его тоже. Это ровно та форма, что у ведомости долга профилей
// (`profileknobreader_test.go`): долг не прячется, а называется и считается.
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
	// signedTypesOwnerFile — единственный дом обоих объявленных типов.
	signedTypesOwnerFile = "pkg/tokenpolicy/policy.go"
	// signedTypesCensusFloor — порог переписи.
	signedTypesCensusFloor = 1000
)

// signedTypeDebtEntry — второе объявление одного из типов, ещё не сведённое к
// политике.
type signedTypeDebtEntry struct {
	// File — путь объявления относительно корня дерева.
	File string
	// Name — идентификатор, которым значение названо на месте.
	Name string
	// Why — почему объявление ещё живёт и кто это решает.
	Why string
	// Until — предикат снятия: наблюдаемое условие, при котором записи здесь
	// больше не место. «Когда дойдут руки» предикатом не является.
	Until string
}

// signedTypeDebt — ведомость. Отсортирована по файлу.
//
// Замер на день заведения гейта: значение токена доступа объявлено в дереве
// ТРИЖДЫ (политика и две записи ниже), значение утверждения клиента — ОДИН раз.
var signedTypeDebt = []signedTypeDebtEntry{
	{
		File: "services/iam/internal/registrytokenwire/local_minter.go",
		Name: "registryTokenType",
		Why: "объявленный тип токена контура выдачи докер-токена; заведён фазой F1, до " +
			"появления политики, и в её область эта поверхность не переводилась",
		Until: "файл читает значение из pkg/tokenpolicy вместо своего объявления",
	},
}

// signedTypeDebtDefects — что не так с самой ведомостью, безотносительно дерева.
//
// Отдельной функцией: опустей ведомость — тело утверждало бы свойство ни о чём,
// и его способность упасть нечем было бы показать.
func signedTypeDebtDefects(entries []signedTypeDebtEntry) []string {
	var out []string
	seen := map[string]bool{}
	for _, d := range entries {
		if d.File == "" || d.Name == "" {
			out = append(out, fmt.Sprintf("запись без координаты: %+v", d))
			continue
		}
		key := d.File + "\x00" + d.Name
		if seen[key] {
			out = append(out, fmt.Sprintf("дубль в ведомости: %s / %s", d.File, d.Name))
		}
		seen[key] = true
		if strings.TrimSpace(d.Why) == "" {
			out = append(out, fmt.Sprintf("%s: %s — запись без письменного обоснования: "+
				"неотличима от упущения", d.File, d.Name))
		}
		if strings.TrimSpace(d.Until) == "" {
			out = append(out, fmt.Sprintf("%s: %s — запись без предиката снятия: послабление, "+
				"которое не умеет истечь, переживает свой предмет", d.File, d.Name))
		}
	}
	sort.Strings(out)
	return out
}

// signedTypeDebtStale — записи, которым больше нечего исключать.
func signedTypeDebtStale(entries []signedTypeDebtEntry, live map[string]bool) []string {
	var stale []string
	for _, d := range entries {
		if !live[d.File+"\x00"+d.Name] {
			stale = append(stale, fmt.Sprintf("%s: %s", d.File, d.Name))
		}
	}
	sort.Strings(stale)
	return stale
}

// signedTypesDistinctnessDefects — что не так с самими значениями.
//
// Отдельной функцией, а не телом теста: значения приходят из политики и в пробе
// не мутируются, поэтому тело утверждало бы свойство, способность которого
// упасть нечем показать. Инъекция подаёт синтетику в ЭТУ ЖЕ функцию.
func signedTypesDistinctnessDefects(values []string) []string {
	var out []string
	for i := 0; i < len(values); i++ {
		if strings.TrimSpace(values[i]) == "" {
			out = append(out, fmt.Sprintf(
				"вид подписанного №%d объявлен ПУСТЫМ типом. Пустой тип означает «тип не "+
					"назван», а не «этот тип»: проверяющий, требующий своего типа явно, принял "+
					"бы отсутствие — то есть снял бы признак целиком", i+1))
		}
		for j := i + 1; j < len(values); j++ {
			if values[i] != values[j] {
				continue
			}
			out = append(out, fmt.Sprintf(
				"виды подписанного №%d и №%d объявлены ОДНИМ типом %q (%s). Два вида, у "+
					"которых совпал объявленный тип, — это один вид: разделение между ними "+
					"держалось бы оставшимися двумя признаками (адресат и чей ключ подписал), "+
					"ни один из которых не назначен единственным, — и одно из двух направлений "+
					"перестало бы работать МОЛЧА, потому что положительный путь держат другие "+
					"признаки", i+1, j+1, values[i], signedTypesOwnerFile))
		}
	}
	sort.Strings(out)
	return out
}

// scanSignedTypeDeclarations — объявления обоих типов по всему дереву.
func scanSignedTypeDeclarations(t *testing.T) ([]StringValueDeclaration, int, StringValueCensus) {
	t.Helper()
	root := repoRoot(t)
	tt := newTrackedTree(t, root)
	values := tokenpolicy.SignedMaterialTypes()

	var (
		rels    []string
		parsed  int
		census  StringValueCensus
		results []StringValueDeclaration
	)
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
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		decls, c, err := ScanDeclaredStringValues(rel, src, values)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		parsed++
		census.ValueSpecs += c.ValueSpecs
		census.StringConstants += c.StringConstants
		census.Matches += c.Matches
		results = append(results, decls...)
	}
	return results, parsed, census
}

// TestSignedMaterialTypesAreDeclaredOnceAndDistinct — сам гейт.
func TestSignedMaterialTypesAreDeclaredOnceAndDistinct(t *testing.T) {
	values := tokenpolicy.SignedMaterialTypes()

	// (1) Предпосылка: видов подписанного ДВА и более. Один вид разделять не с
	// чем, и гейт молчал бы и тогда, когда предмет исчез.
	if len(values) < 2 {
		t.Fatalf("политика объявляет %d вид(а) подписанного (%v) — разделять нечего, и "+
			"«попарно различны» сказано ни о чём", len(values), values)
	}

	// (2) Находка: значения совпали. Это и есть предмет сценария.
	for _, bad := range signedTypesDistinctnessDefects(values) {
		t.Error(bad)
	}
	if t.Failed() {
		t.FailNow()
	}

	decls, parsed, census := scanSignedTypeDeclarations(t)

	byFile := map[string]map[string]bool{}
	for _, d := range decls {
		if byFile[d.File] == nil {
			byFile[d.File] = map[string]bool{}
		}
		byFile[d.File][d.Value] = true
	}
	var pairFiles []string
	for file, vals := range byFile {
		if len(vals) >= 2 {
			pairFiles = append(pairFiles, file)
		}
	}
	sort.Strings(pairFiles)

	perValue := map[string][]StringValueDeclaration{}
	for _, d := range decls {
		perValue[d.Value] = append(perValue[d.Value], d)
	}
	var breakdown []string
	for _, v := range values {
		var where []string
		for _, d := range perValue[v] {
			where = append(where, fmt.Sprintf("%s:%d %s", d.File, d.Line, d.Name))
		}
		sort.Strings(where)
		breakdown = append(breakdown, fmt.Sprintf("%q — %d: %s", v, len(perValue[v]),
			strings.Join(where, ", ")))
	}

	t.Logf("перепись: не-тестовых файлов Go разобрано %d, спецификаций const/var прочитано %d, "+
		"из них объявляющих строку %d, из них объявляющих типы подписанного %d; файлов, "+
		"объявляющих ОБА вида, %d; ведомость вторых объявлений: %d записей",
		parsed, census.ValueSpecs, census.StringConstants, census.Matches,
		len(pairFiles), len(signedTypeDebt))
	for _, b := range breakdown {
		t.Logf("объявления: %s", b)
	}

	if parsed < signedTypesCensusFloor {
		t.Fatalf("перепись обвалилась: разобрано %d файлов при пороге %d", parsed, signedTypesCensusFloor)
	}
	if census.Matches == 0 {
		t.Fatalf("на %d файлах не найдено НИ ОДНОГО объявления типов %v — разбор перестал "+
			"видеть предмет, и его молчание сказано ни о чём", parsed, values)
	}

	// (3) Предпосылка: оба вида объявлены В ОДНОМ месте, и место это — владелец.
	if len(pairFiles) != 1 {
		t.Fatalf("файлов, объявляющих ОБА вида подписанного, %d, ожидался 1: %v\n\n"+
			"«Объявлены в одном месте» — это не стиль: пока значения живут по своим пакетам, "+
			"их РАЗЛИЧИЕ не является ничьей находкой. Оно не выражено, и потому не может "+
			"покраснеть ни у кого.", len(pairFiles), pairFiles)
	}
	if pairFiles[0] != signedTypesOwnerFile {
		t.Fatalf("оба вида объявлены в %s, а владельцем объявлен %s — гейт стережёт "+
			"координату, которой больше не существует", pairFiles[0], signedTypesOwnerFile)
	}

	// (4) Находка: второе объявление вне владельца, не названное ведомостью.
	debt := map[string]bool{}
	for _, d := range signedTypeDebt {
		debt[d.File+"\x00"+d.Name] = true
	}
	var fresh []string
	for _, d := range decls {
		if d.File == signedTypesOwnerFile {
			continue
		}
		if debt[d.File+"\x00"+d.Name] {
			continue
		}
		fresh = append(fresh, fmt.Sprintf("%s:%d  %s %s = %q", d.File, d.Line, d.Kind, d.Name, d.Value))
	}
	sort.Strings(fresh)
	if len(fresh) > 0 {
		t.Fatalf("тип подписанного объявлен ВНЕ %s и не назван ведомостью — %d место(а):\n  %s\n\n"+
			"Второе объявление одного значения не расходится с первым сразу: оно расходится "+
			"при первой же правке одной стороны, и расходится молча — обе стороны по "+
			"отдельности выглядят исправными. Исходов три: читать значение из "+
			"pkg/tokenpolicy · снять объявление вместе с кодом, который его подпирал · "+
			"завести запись ведомости с причиной и предикатом снятия.",
			signedTypesOwnerFile, len(fresh), strings.Join(fresh, "\n  "))
	}

	var where []string
	for _, d := range decls {
		if d.File == signedTypesOwnerFile {
			where = append(where, fmt.Sprintf("%s:%d %s", d.File, d.Line, d.Name))
		}
	}
	t.Logf("оба вида объявлены в одном месте: %s; попарно различны: %v",
		strings.Join(where, ", "), values)
}

// TestSignedTypeDebtIsWellFormed — каждая запись ведомости несёт координату,
// письменное обоснование и предикат снятия, и ни одна не повторяется.
func TestSignedTypeDebtIsWellFormed(t *testing.T) {
	t.Logf("ведомость: %d записей", len(signedTypeDebt))
	for _, bad := range signedTypeDebtDefects(signedTypeDebt) {
		t.Error(bad)
	}
}

// TestSignedTypeDebtExpiresOnItsOwn — запись, которой больше нечего исключать,
// роняет прогон.
//
// Без этой половины ведомость пережила бы свой предмет: сведённое к политике
// объявление осталось бы в списке, следующий читатель принял бы его за
// действующий долг, а освободившееся место унаследовал бы новый дефект с тем же
// путём.
func TestSignedTypeDebtExpiresOnItsOwn(t *testing.T) {
	decls, parsed, census := scanSignedTypeDeclarations(t)
	t.Logf("перепись: файлов разобрано %d, объявлений типов подписанного найдено %d, "+
		"записей ведомости %d", parsed, census.Matches, len(signedTypeDebt))

	live := map[string]bool{}
	for _, d := range decls {
		if d.File == signedTypesOwnerFile {
			continue
		}
		live[d.File+"\x00"+d.Name] = true
	}
	stale := signedTypeDebtStale(signedTypeDebt, live)
	if len(stale) > 0 {
		t.Fatalf("в ведомости %d записей, которым больше нечего исключать:\n%s\n\n"+
			"Объявление сведено к политике или снято — запись обязана уйти из ведомости тем "+
			"же изменением. Оставленная, она объявляет живым закрытый долг и освобождает "+
			"место новому дефекту с тем же путём.", len(stale), strings.Join(stale, "\n"))
	}
}

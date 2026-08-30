// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_addr_nameform_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что
// анализатор способен упасть, называет координату и ось расхождения, и молчит на
// законном близнеце.
//
// Стенд синтетический: настоящее дерево нельзя ни сломать, ни вернуть, а вердикт
// о нём (`clienttruth_addr_nameform_test.go`) о способности падать не говорит
// ничего — зелёный получает и та проверка, что не смотрит никуда.
//
// Инъекции вносятся ПО ОДНОЙ, и каждая снимает РОВНО ОДНО свойство у носителя,
// чьи остальные свойства целы: инъекция вида «завести ещё один негодный носитель»
// нарушает всё, что требуется от носителей вообще, и краснота от неё ничего не
// доказывает (`testing.md` §«Гейт на класс», п.2в).
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Форма, применяемая на синтетическом стенде. Намеренно НЕ равна форме
// настоящего дерева: совпадение сделало бы прогон зависимым от того, что мы же и
// правим, — стенд обязан доказывать механизм, а не сегодняшнее состояние ствола.
const probeForm = "^[a-c0-8]([-a-c0-8]{0,44}[a-c0-8])?$"
const probeAlphabet = "[a-c0-8]([-a-c0-8]{0,44}[a-c0-8])?"

// nameFormStand — синтетическое дерево: источник формы и носители всех трёх
// видов, где каждое утверждение верно. Это ЗАКОННОЕ состояние, и на нём
// анализатор обязан молчать.
type nameFormStand struct{ root string }

func newNameFormStand(t *testing.T) *nameFormStand {
	t.Helper()
	s := &nameFormStand{root: t.TempDir()}

	// Источник истины. В комментарии рядом стоит ДРУГАЯ форма: анализатор,
	// читающий сырой текст, взял бы истину из прозы о ней.
	s.write(t, "pkg/validate/nameform/nameform.go", "package nameform\n"+
		"\n// Прежняя форма была `[z-z9-9]([-z9]{0,7}[z9])?` — она снята.\n"+
		"const Form = `"+probeForm+"`\n")

	// Контракт: утверждение в скобочном строении, с экранированием носителя.
	s.write(t, "proto/kacho/cloud/probe/v1/thing.proto",
		"syntax = \"proto3\";\n"+
			"message Thing {\n"+
			"  // Value must match the regular expression ``\\|"+probeAlphabet+"``.\n"+
			"  string name = 1;\n"+
			"}\n")

	// Контракт: то же утверждение в БЕСскобочном строении — так пишется форма в
	// описании фильтра. Без этого вида носителя целая полоса объявлений была бы
	// вне наблюдения.
	s.write(t, "proto/kacho/cloud/probe/v1/thing_service.proto",
		"syntax = \"proto3\";\n"+
			"message ListThingsRequest {\n"+
			"  // 3. The value in double quotes. Matches [a-c0-8][-a-c0-8]{0,44}[a-c0-8].\n"+
			"  string filter = 1;\n"+
			"}\n")

	// Сайт: таблица ограничений.
	s.write(t, "services/probe/docs/src/constants/restrictions.ts",
		"export const RESTRICTIONS = {\n"+
			"  name: ['regex ^"+probeAlphabet+"$'],\n"+
			"};\n")

	// Сайт: страница.
	s.write(t, "services/probe/docs/content/api/thing.mdx",
		"# Thing\n\nИмя — `^"+probeAlphabet+"$`.\n")

	// ЗАКОННЫЙ БЛИЗНЕЦ №1: regex ключа метки. Строение то же, граница другая —
	// это ДРУГОЙ предмет, и судить его значило бы краснеть на верном тексте.
	s.write(t, "services/probe/docs/content/api/labels.mdx",
		"# Метки\n\nКлюч — `^[a-z][-_./a-z0-9]{0,62}$`, значение до 63 байт.\n")

	// ЗАКОННЫЙ БЛИЗНЕЦ №2: носитель НЕ из перечня видов. Тот же текст в файле,
	// который сайтом документации не является, судиться не должен.
	s.write(t, "services/probe/docs/src/constants/other.ts",
		"export const X = 'regex ^[q-r1-2]([-q-r1-2]{0,44}[q-r1-2])?$';\n")

	return s
}

func (s *nameFormStand) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(s.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func (s *nameFormStand) run(
	t *testing.T, ex ...NameFormClaimExemption,
) ([]NameFormClaimFinding, NameFormClaimCensus, error) {
	t.Helper()
	var log strings.Builder
	f, c, err := AuditNameFormClaims(NameFormClaimOptions{
		Tree:       clientTruthSyntheticTree(t, s.root),
		ProtoRoot:  "proto",
		DocsRoots:  []string{"services"},
		Exemptions: ex,
	}, &log)
	if s := strings.TrimSpace(log.String()); s != "" {
		t.Log(s)
	}
	return f, c, err
}

func (s *nameFormStand) mustRun(
	t *testing.T, ex ...NameFormClaimExemption,
) ([]NameFormClaimFinding, NameFormClaimCensus) {
	t.Helper()
	f, c, err := s.run(t, ex...)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	return f, c
}

// TestNameFormInjection_CleanStandIsSilent — КОНТРОЛЬ. Без него всякая
// последующая краснота неотличима от анализатора, краснеющего на всём.
func TestNameFormInjection_CleanStandIsSilent(t *testing.T) {
	s := newNameFormStand(t)
	findings, census := s.mustRun(t)
	if len(findings) != 0 {
		t.Fatalf("на законном дереве находок %d, ожидался ноль: %v", len(findings), findings)
	}
	if census.ProtoFiles != 2 {
		t.Fatalf("файлов контракта %d, ожидалось 2 — стенд прочитан не весь", census.ProtoFiles)
	}
	// Три носителя сайта: таблица ограничений, страница ресурса, страница меток.
	// `other.ts` в число НЕ входит — вид носителя не тот.
	if census.DocsFiles != 3 {
		t.Fatalf("носителей сайта %d, ожидалось 3 — отбор вида носителя разошёлся", census.DocsFiles)
	}
	// Четыре утверждения: два контракта и два носителя сайта. Regex ключа метки
	// и текст из `other.ts` в счёт не попали — это и есть молчание на близнецах.
	if census.Claims != 4 || census.Agreeing != 4 {
		t.Fatalf("утверждений %d (ожидалось 4), совпало %d (ожидалось 4)",
			census.Claims, census.Agreeing)
	}
}

// TestNameFormInjection_DivergentAlphabetIsFound — снято ОДНО свойство:
// алфавит контракта разошёлся с применяемым. Прочее у носителя цело.
func TestNameFormInjection_DivergentAlphabetIsFound(t *testing.T) {
	s := newNameFormStand(t)
	s.write(t, "proto/kacho/cloud/probe/v1/thing.proto",
		"syntax = \"proto3\";\n"+
			"message Thing {\n"+
			"  // Value must match the regular expression ``\\|[a-cA-C0-8]([-_a-cA-C0-8]{0,44}[a-c0-8])?``.\n"+
			"  string name = 1;\n"+
			"}\n")
	findings, census := s.mustRun(t)
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась одна: %v", len(findings), findings)
	}
	f := findings[0]
	if f.File != "proto/kacho/cloud/probe/v1/thing.proto" || f.Line != 3 {
		t.Fatalf("координата %s:%d — не та, что у внесённого расхождения", f.File, f.Line)
	}
	// Диагностика — часть свойства: находка обязана называть ОСЬ, иначе читатель
	// сличает две строки посимвольно (`testing.md` §«Гейт на класс», п.8).
	msg := f.String()
	for _, want := range []string{"первый знак", "середина"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("находка не называет ось %q: %s", want, msg)
		}
	}
	if census.Agreeing != 3 {
		t.Fatalf("совпало %d, ожидалось 3 — задеты соседние носители", census.Agreeing)
	}
}

// TestNameFormInjection_DivergentLengthIsFound — снято ОДНО свойство: алфавит
// совпадает, разошлась ГРАНИЦА ДЛИНЫ. Без этой пробы ось длины была бы объявлена
// и не проверена.
func TestNameFormInjection_DivergentLengthIsFound(t *testing.T) {
	s := newNameFormStand(t)
	s.write(t, "services/probe/docs/src/constants/restrictions.ts",
		"export const RESTRICTIONS = {\n"+
			"  name: ['regex ^[a-c0-8]([-a-c0-8]{1,44}[a-c0-8])?$'],\n"+
			"};\n")
	findings, _ := s.mustRun(t)
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась одна: %v", len(findings), findings)
	}
	if !strings.Contains(findings[0].String(), "длина {1,44} против {0,44}") {
		t.Fatalf("находка не называет ось длины: %s", findings[0].String())
	}
}

// TestNameFormInjection_ParenlessFormIsJudged — снято ОДНО свойство у носителя
// БЕСскобочного строения. Проба существует потому, что прежняя редакция
// распознавателя требовала скобок и не видела этот вид ВОВСЕ: не «редкий край», а
// шесть объявлений вне наблюдения (`testing.md` §«Гейт на класс», п.7).
func TestNameFormInjection_ParenlessFormIsJudged(t *testing.T) {
	s := newNameFormStand(t)
	s.write(t, "proto/kacho/cloud/probe/v1/thing_service.proto",
		"syntax = \"proto3\";\n"+
			"message ListThingsRequest {\n"+
			"  // 3. The value in double quotes. Matches [a-c][-a-c0-8]{1,44}[a-c0-8].\n"+
			"  string filter = 1;\n"+
			"}\n")
	findings, _ := s.mustRun(t)
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась одна — бесскобочное строение не судится: %v",
			len(findings), findings)
	}
	if findings[0].File != "proto/kacho/cloud/probe/v1/thing_service.proto" {
		t.Fatalf("координата %s — не та", findings[0].File)
	}
}

// TestNameFormInjection_LiveExemptionSuppresses — послабление с ЖИВЫМ предметом
// снимает находку и считается переписью.
func TestNameFormInjection_LiveExemptionSuppresses(t *testing.T) {
	s := newNameFormStand(t)
	s.write(t, "services/probe/docs/content/api/thing.mdx",
		"# Thing\n\nИмя — `^[a-c0-8]([-a-c0-8]{1,44}[a-c0-8])?$`.\n")
	claimed := NameAlphabet{First: "a-c0-8", Mid: "-a-c0-8", Lo: "1", Hi: "44", Last: "a-c0-8"}
	findings, census := s.mustRun(t, NameFormClaimExemption{
		File:    "services/probe/docs/content/api/thing.mdx",
		Claimed: claimed.String(),
		Reason:  "правится полосой сайта",
	})
	if len(findings) != 0 {
		t.Fatalf("живое послабление не сняло находку: %v", findings)
	}
	if census.Exempted != 1 {
		t.Fatalf("снято послаблением %d, ожидалось 1", census.Exempted)
	}
}

// TestNameFormInjection_StaleExemptionIsAFinding — послабление, которому нечего
// исключать, обязано быть находкой: иначе слепая зона переживёт свой предмет.
func TestNameFormInjection_StaleExemptionIsAFinding(t *testing.T) {
	s := newNameFormStand(t)
	findings, _ := s.mustRun(t, NameFormClaimExemption{
		File:    "services/probe/docs/content/api/thing.mdx",
		Claimed: "[q-r]([-q-r]{0,44}[q-r])?",
		Reason:  "предмет давно выправлен",
	})
	if len(findings) != 1 || !findings[0].StaleExemption {
		t.Fatalf("устаревшее послабление не стало находкой: %v", findings)
	}
	if !strings.Contains(findings[0].String(), "предмет давно выправлен") {
		t.Fatalf("находка не называет причину послабления: %s", findings[0].String())
	}
}

// TestNameFormInjection_EmptyWalkIsNotSilentSuccess — «ноль находок» обязано быть
// отличимо от «ноль прочитанного». Обход пустого дерева даёт нулевую перепись, и
// вердикт о настоящем дереве обязан на ней падать своей премисой.
func TestNameFormInjection_EmptyWalkIsNotSilentSuccess(t *testing.T) {
	s := newNameFormStand(t)
	empty := t.TempDir()
	s.write(t, "unused", "")
	var log strings.Builder
	_, census, err := AuditNameFormClaims(NameFormClaimOptions{
		Tree:       clientTruthSyntheticTree(t, s.root),
		ProtoRoot:  filepath.Base(empty), // каталога такого имени в стенде нет
		DocsRoots:  []string{filepath.Base(empty)},
		FormSource: "pkg/validate/nameform/nameform.go",
	}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if census.ProtoFiles != 0 || census.DocsFiles != 0 || census.Claims != 0 {
		t.Fatalf("перепись непуста на пустом обходе: %+v", census)
	}
}

// TestNameFormInjection_UnparseableSourceIsAnError — истину брать неоткуда, и это
// ОТКАЗ, а не молчаливый зелёный: анализатор без источника сравнивал бы с пустотой.
func TestNameFormInjection_UnparseableSourceIsAnError(t *testing.T) {
	s := newNameFormStand(t)
	s.write(t, "pkg/validate/nameform/nameform.go", "package nameform\n// формы здесь больше нет\n")
	if _, _, err := s.run(t); err == nil {
		t.Fatal("источник без объявления формы принят молча")
	}

	s2 := newNameFormStand(t)
	s2.write(t, "pkg/validate/nameform/nameform.go",
		"package nameform\n\nconst Form = `^[a-z]+$`\n")
	if _, _, err := s2.run(t); err == nil {
		t.Fatal("форма, не разобравшаяся в тройку классов, принята молча")
	}
}

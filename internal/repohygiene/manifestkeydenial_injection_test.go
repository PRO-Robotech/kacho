// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// manifestkeydenial_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что анализатор
// способен упасть, называет координату и молчит на законном близнеце.
//
// Стенд синтетический: настоящее дерево нельзя ни сломать, ни вернуть, а вердикт
// о нём (`manifestkeydenial_test.go`) о способности падать не говорит ничего —
// зелёный получает и та проверка, что не смотрит никуда.
//
// Законные близнецы взяты НЕ выдуманные: это дословные строки этого дерева,
// на которых предикат «слово рядом со словом» давал шесть ложных находок из
// десяти. Близнец, сочинённый под свой же разбор, доказывал бы только то, что
// автор знает свой разбор.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type keyDenialStand struct{ root string }

func (s *keyDenialStand) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(s.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// newKeyDenialStand — ЗАКОННОЕ состояние: манифест несёт четыре ключа, приёмка
// содержит пять форм, каждая из которых обязана остаться незамеченной.
func newKeyDenialStand(t *testing.T) *keyDenialStand {
	t.Helper()
	s := &keyDenialStand{root: t.TempDir()}
	s.write(t, "services/probe/internal/manifest/rules.go", `package manifest

type Rule struct {
	Classes       []string `+"`"+`yaml:"classes"`+"`"+`
	Verbs         []string `+"`"+`yaml:"verbs"`+"`"+`
	Roles         []string `+"`"+`yaml:"roles"`+"`"+`
	Seed          []string `+"`"+`yaml:"seed"`+"`"+`
}
`)
	// Проба рядом объявляет ключ, которого продукт НЕ несёт: словарь живых
	// ключей строится по прод-коду, иначе фикстура пробы завела бы предмет.
	s.write(t, "services/probe/internal/manifest/rules_test.go", `package manifest
// yaml:"grants" — ключ фикстуры, живым он не является
`)
	s.write(t, "services/probe/docs/engineering/acceptance/legal.md", `# Законные близнецы

| `+"`grants`"+` | **не заводится** | ключа нет ни в контракте, ни в домене |

- **Колонка жизненного цикла у `+"`roles`"+` не заводится.** Соблазн велик.
| снятие импорта `+"`seed`"+` из `+"`pg`"+` (инверсия ребра) | не заводится: предмет ребра — другой |
| `+"`grants`"+` | **не заводится** | развернуть его в `+"`verbs`"+` некому — см. ниже |

> **вместе с `+"`classes`"+`**. Неверно дважды: ключа `+"`classes`"+` нет, а подстановка
> ресурса системна by construction — исторический разбор круга 1.
`)
	return s
}

func (s *keyDenialStand) audit(t *testing.T) ([]ManifestKeyDenialFinding, ManifestKeyDenialCensus) {
	t.Helper()
	var log strings.Builder
	f, c, err := AuditManifestKeyDenial(ManifestKeyDenialOptions{
		Root:                s.root,
		ManifestDirSuffix:   "internal/manifest",
		AcceptanceDirSuffix: "docs/engineering/acceptance",
	}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))
	return f, c
}

// TestKeyDenialStandIsSilentOnTheLegalTwins — КОНТРОЛЬ. Без него всякая инъекция
// ниже доказывала бы лишь то, что покраснело хоть что-то.
func TestKeyDenialStandIsSilentOnTheLegalTwins(t *testing.T) {
	s := newKeyDenialStand(t)
	findings, census := s.audit(t)

	// Премиса контроля: прочитано было ЧТО-ТО. Молчание над пустым обходом
	// доказывает только пустоту обхода.
	if census.ManifestFiles != 1 || census.DocFiles != 1 {
		t.Fatalf("обход: файлов манифеста %d, приёмок %d — стенд не прочитан",
			census.ManifestFiles, census.DocFiles)
	}
	// Ключи берутся у ПРОД-кода: `grants` объявлен только в фикстуре пробы и
	// живым считаться не вправе — иначе законный близнец стал бы находкой.
	if census.LiveKeys != 4 {
		t.Fatalf("живых ключей %d, ожидалось 4 (classes·verbs·roles·seed) — "+
			"словарь построен не по прод-коду", census.LiveKeys)
	}
	if len(findings) != 0 {
		t.Fatalf("на законном состоянии находок %d, ожидался ноль:\n%s",
			len(findings), renderKeyDenial(findings))
	}
	// Отдельно: молчание получено НЕ оттого, что разбор ничего не разобрал.
	if census.DocLines < 5 {
		t.Fatalf("строк приёмки %d — читать было нечего", census.DocLines)
	}
}

// TestKeyDenialInjectionLiveKeyWithoutAMarker — ИНЪЕКЦИЯ 1: живой ключ объявлен
// незаводимым, маркера нет.
func TestKeyDenialInjectionLiveKeyWithoutAMarker(t *testing.T) {
	s := newKeyDenialStand(t)
	s.write(t, "services/probe/docs/engineering/acceptance/injected.md", `# Инъекция

| `+"`classes`"+` | **не заводится** | ключа нет ни в контракте, ни в домене |
`)
	findings, _ := s.audit(t)
	if len(findings) != 1 {
		t.Fatalf("инъекция обязана дать РОВНО одну находку, дала %d:\n%s",
			len(findings), renderKeyDenial(findings))
	}
	f := findings[0]
	if f.Kind != DenialOfALiveKey || f.Key != "classes" {
		t.Fatalf("находка не о том: род %q, ключ %q", f.Kind, f.Key)
	}
	// Находка обязана называть КООРДИНАТУ: находка, называющая симптом, посылает
	// читателя искать не там.
	if !strings.HasSuffix(f.File, "injected.md") || f.Line != 3 {
		t.Fatalf("координата не названа: %s:%d", f.File, f.Line)
	}
}

// TestKeyDenialInjectionMarkerSilencesTheSameClaim — ИНЪЕКЦИЯ 2, обратная
// сторона: то же утверждение под маркером состояния — молчание.
func TestKeyDenialInjectionMarkerSilencesTheSameClaim(t *testing.T) {
	s := newKeyDenialStand(t)
	s.write(t, "services/probe/docs/engineering/acceptance/injected.md", `# Та же строка под маркером

> [!important] СОСТОЯНИЕ ключа `+"`classes`"+` на ревизии `+"`ab771fe83`"+`: ключ ЗАВЕДЁН.

| `+"`classes`"+` | **не заводится** | ключа нет ни в контракте, ни в домене |
`)
	findings, census := s.audit(t)
	if census.MarkerBlocks != 1 || census.KeysMarked != 1 {
		t.Fatalf("маркер не распознан: блоков %d, ключей под маркером %d",
			census.MarkerBlocks, census.KeysMarked)
	}
	// Утверждение НЕ исчезло — текст круга остался на месте; исчезла находка.
	if census.ClaimLines != 1 {
		t.Fatalf("строк-утверждений %d, ожидалась 1: маркер обязан гасить НАХОДКУ, "+
			"а не делать утверждение невидимым разбору", census.ClaimLines)
	}
	if len(findings) != 0 {
		t.Fatalf("под маркером находок %d, ожидался ноль:\n%s",
			len(findings), renderKeyDenial(findings))
	}
}

// TestKeyDenialInjectionMarkerWithoutARevisionDoesNotCount — ИНЪЕКЦИЯ 3: маркер
// без ревизии маркером не является. Утверждение о дереве без ревизии проверить
// нечем, и «состояние» без неё было бы вторым утверждением того же рода.
func TestKeyDenialInjectionMarkerWithoutARevisionDoesNotCount(t *testing.T) {
	s := newKeyDenialStand(t)
	s.write(t, "services/probe/docs/engineering/acceptance/injected.md", `# Маркер без ревизии

> [!important] СОСТОЯНИЕ ключа `+"`classes`"+`: ключ теперь заведён.

| `+"`classes`"+` | **не заводится** | ключа нет ни в контракте, ни в домене |
`)
	findings, census := s.audit(t)
	if census.MarkerBlocks != 0 {
		t.Fatalf("блоков состояния %d — маркер без ревизии зачтён", census.MarkerBlocks)
	}
	if len(findings) != 1 || findings[0].Kind != DenialOfALiveKey {
		t.Fatalf("ожидалась одна находка рода %q, получено %d:\n%s",
			DenialOfALiveKey, len(findings), renderKeyDenial(findings))
	}
}

// TestKeyDenialInjectionMarkerOutlivesItsSubject — ИНЪЕКЦИЯ 4: самоистечение в
// ОБРАТНУЮ сторону. Ключ снят с манифеста, маркер остался: послабление, которому
// нечего снимать, унаследует следующая слепая зона.
func TestKeyDenialInjectionMarkerOutlivesItsSubject(t *testing.T) {
	s := newKeyDenialStand(t)
	s.write(t, "services/probe/docs/engineering/acceptance/injected.md", `# Маркер пережил предмет

> [!important] СОСТОЯНИЕ ключа `+"`grants`"+` на ревизии `+"`ab771fe83`"+`: ключ ЗАВЕДЁН.
`)
	findings, _ := s.audit(t)
	if len(findings) != 1 {
		t.Fatalf("ожидалась одна находка, получено %d:\n%s",
			len(findings), renderKeyDenial(findings))
	}
	if findings[0].Kind != MarkerWithoutASubject || findings[0].Key != "grants" {
		t.Fatalf("находка не о том: род %q, ключ %q", findings[0].Kind, findings[0].Key)
	}
}

// TestKeyDenialInjectionRemovingTheKeyFromTheManifestSilencesTheClaim — ИНЪЕКЦИЯ
// 5: у утверждения исчез ПРЕДМЕТ. Ключ снят с манифеста — утверждение стало
// верным, и находки быть не должно. Без этой пробы гейт судил бы слово, а не
// расхождение слова с деревом.
func TestKeyDenialInjectionRemovingTheKeyFromTheManifestSilencesTheClaim(t *testing.T) {
	s := newKeyDenialStand(t)
	s.write(t, "services/probe/docs/engineering/acceptance/injected.md", `# Утверждение без маркера

| `+"`classes`"+` | **не заводится** | ключа нет ни в контракте, ни в домене |
`)
	if f, _ := s.audit(t); len(f) != 1 {
		t.Fatalf("предпосылка инъекции не воспроизведена: находок %d", len(f))
	}
	s.write(t, "services/probe/internal/manifest/rules.go", `package manifest

type Rule struct {
	Verbs []string `+"`"+`yaml:"verbs"`+"`"+`
	Roles []string `+"`"+`yaml:"roles"`+"`"+`
	Seed  []string `+"`"+`yaml:"seed"`+"`"+`
}
`)
	findings, census := s.audit(t)
	if census.LiveKeys != 3 {
		t.Fatalf("живых ключей %d, ожидалось 3 — ключ не снят", census.LiveKeys)
	}
	if len(findings) != 0 {
		t.Fatalf("ключ снят с манифеста — утверждение стало верным, находок %d:\n%s",
			len(findings), renderKeyDenial(findings))
	}
}

func renderKeyDenial(findings []ManifestKeyDenialFinding) string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, "  "+f.String())
	}
	return strings.Join(out, "\n")
}

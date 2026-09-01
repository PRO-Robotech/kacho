// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// licensesubject_injection_test.go — доказательство способности гейта
// licensesubject_test.go упасть и смолчать.
//
// Гейт судит одну строку текстового файла, и вакуумным его сделать проще всего:
// достаточно, чтобы распознаватель перестал видеть предмет — и он молча
// пропустит всё дерево. Поэтому каждая ось проверяется В ОБЕ СТОРОНЫ, а рядом с
// каждым отрицанием стоит законный близнец той же формы.
//
// Инъекция идёт по СИНТЕТИЧЕСКОМУ корпусу: гейт разложен на чистую функцию и
// обход, поэтому доказательство не пишет в живое дерево и не зависит от того,
// что в нём сегодня лежит.
package repohygiene

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// injLicenseBody — тело LICENSE с подставленным предметом. Форма воспроизводит
// живой файл: строка предмета, перенос хвоста и строка копирайта со скобками,
// которую распознаватель обязан НЕ принять за идентификатор.
func injLicenseBody(subject string) string {
	return "" +
		"Business Source License 1.1\n" +
		"\n" +
		"Parameters\n" +
		"\n" +
		"Licensor:             PRO-Robotech\n" +
		"\n" +
		"Licensed Work:        " + subject + " and all source code, configuration,\n" +
		"                      documentation, and other materials in this repository.\n" +
		"                      The Licensed Work is (c) PRO-Robotech.\n" +
		"\n" +
		"Change Date:          2029-01-01\n"
}

type injLicenseCorpus map[string]string

func (c injLicenseCorpus) paths() []string {
	out := make([]string, 0, len(c))
	for k := range c {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (c injLicenseCorpus) read(rel string) ([]byte, error) {
	body, ok := c[rel]
	if !ok {
		return nil, fmt.Errorf("нет такого файла")
	}
	return []byte(body), nil
}

func injLicenseScan(c injLicenseCorpus) ([]licenseSubjectFinding, licenseSubjectCensus) {
	return scanLicenseSubjects(c.paths(), c.read)
}

// ── сторона (а): дефект краснеет и называет координату ───────────────────────

// Наблюдавшийся дефект: корневой файл — копия сервисного, предмет не правлен.
func TestLicenseSubjectGate_RedsWhenRootNamesOneService(t *testing.T) {
	findings, census := injLicenseScan(injLicenseCorpus{
		"LICENSE": injLicenseBody("Kachō Compute (kacho-compute)"),
	})
	if len(findings) != 1 {
		t.Fatalf("ожидалась одна находка, получено %d (перепись: %s)", len(findings), census)
	}
	got := findings[0].String()
	for _, want := range []string{"LICENSE", "kacho-compute", "`kacho`"} {
		if !strings.Contains(got, want) {
			t.Errorf("находка не называет %q: %s", want, got)
		}
	}
}

// Второй наблюдавшийся дефект: сервисный файл называет ЧУЖОЙ сервис.
func TestLicenseSubjectGate_RedsWhenServiceNamesAnotherService(t *testing.T) {
	findings, _ := injLicenseScan(injLicenseCorpus{
		"services/registry/LICENSE": injLicenseBody("Kachō Geography (kacho-geo)"),
		"services/storage/LICENSE":  injLicenseBody("Kachō Geography (kacho-geo)"),
	})
	if len(findings) != 2 {
		t.Fatalf("оба чужих предмета обязаны быть названы, получено %d: %v", len(findings), findings)
	}
	if findings[0].file != "services/registry/LICENSE" || findings[1].file != "services/storage/LICENSE" {
		t.Fatalf("находки не называют оба файла: %v", findings)
	}
}

// Место, для которого предмет не объявлен, — находка, а не пропуск. Без этой
// ветви первый же LICENSE в новом каталоге выпал бы из наблюдения молча.
func TestLicenseSubjectGate_RedsOnAnUndeclaredLocation(t *testing.T) {
	findings, census := injLicenseScan(injLicenseCorpus{
		"pkg/LICENSE": injLicenseBody("Kachō (kacho)"),
	})
	if len(findings) != 1 {
		t.Fatalf("LICENSE в неизвестном месте обязан быть находкой, получено %d (перепись: %s)",
			len(findings), census)
	}
	if census.derived != 0 {
		t.Fatalf("предмет не выводится для неизвестного места, а перепись насчитала %d", census.derived)
	}
	if !strings.Contains(findings[0].String(), "предмет не объявлен") {
		t.Fatalf("находка не называет причину: %s", findings[0])
	}
}

// Файл без строки предмета вовсе: лицензия есть, предмета нет.
func TestLicenseSubjectGate_RedsWhenSubjectLineIsAbsent(t *testing.T) {
	findings, _ := injLicenseScan(injLicenseCorpus{
		"services/vpc/LICENSE": "Business Source License 1.1\n\nChange Date: 2029-01-01\n",
	})
	if len(findings) != 1 || !strings.Contains(findings[0].String(), "не объявлен вовсе") {
		t.Fatalf("отсутствие строки предмета не распознано: %v", findings)
	}
}

// Предмет назван прозой, без машинной половины: сверить не с чем.
func TestLicenseSubjectGate_RedsWhenIdentifierIsMissing(t *testing.T) {
	findings, _ := injLicenseScan(injLicenseCorpus{
		"services/vpc/LICENSE": injLicenseBody("The Kacho VPC service"),
	})
	if len(findings) != 1 || !strings.Contains(findings[0].String(), "нет идентификатора в скобках") {
		t.Fatalf("отсутствие идентификатора не распознано: %v", findings)
	}
}

// ── сторона (б): законный близнец обязан молчать ─────────────────────────────

// Верное дерево: корень, псевдоним каталога и обычный сервис.
func TestLicenseSubjectGate_SilentOnACorrectTree(t *testing.T) {
	findings, census := injLicenseScan(injLicenseCorpus{
		"LICENSE":                  injLicenseBody("Kachō (kacho), the cloud control-plane platform,"),
		"gateway/LICENSE":          injLicenseBody("Kachō API Gateway (kacho-api-gateway)"),
		"services/vpc/LICENSE":     injLicenseBody("Kachō VPC (kacho-vpc)"),
		"services/compute/LICENSE": injLicenseBody("Kachō Compute (kacho-compute)"),
	})
	if len(findings) != 0 {
		t.Fatalf("верное дерево объявлено находкой: %v — гейт, краснеющий на верном "+
			"тексте, отключают первым", findings)
	}
	if census.licenses != 4 || census.derived != 4 {
		t.Fatalf("близнецы не дошли до предиката: %s — молчание тогда означает "+
			"«не читал», а не «сошлось»", census)
	}
}

// Новый сервис под services/ карты НЕ требует: предмет выводится механически.
// Ось отдельная, потому что она и есть довод против полной карты путь→предмет.
func TestLicenseSubjectGate_SilentOnANewServiceWithoutAnyMapEntry(t *testing.T) {
	if _, declared := licenseSubjectAliases["services/objectstore"]; declared {
		t.Fatal("предпосылка оси: сервис не должен стоять в карте псевдонимов")
	}
	findings, census := injLicenseScan(injLicenseCorpus{
		"services/objectstore/LICENSE": injLicenseBody("Kachō Object Storage (kacho-objectstore)"),
	})
	if len(findings) != 0 {
		t.Fatalf("новый сервис потребовал записи в карте: %v", findings)
	}
	if census.derived != 1 {
		t.Fatalf("предмет нового сервиса не выведен: %s", census)
	}
}

// Скобки копирайта `(c)` стоят в КАЖДОМ файле и на строку предмета не влияют.
// Без этой оси распознаватель, читающий файл целиком, выглядел бы исправным.
func TestLicenseSubjectGate_ReadsTheSubjectLineOnlyNotTheCopyright(t *testing.T) {
	body := injLicenseBody("Kachō VPC (kacho-vpc)")
	if !strings.Contains(body, "(c) PRO-Robotech") {
		t.Fatal("предпосылка оси: в теле обязана быть скобка копирайта")
	}
	findings, _ := injLicenseScan(injLicenseCorpus{"services/vpc/LICENSE": body})
	if len(findings) != 0 {
		t.Fatalf("скобка копирайта принята за предмет: %v", findings)
	}
}

// ── перепись: «ноль находок» отличимо от «ноль прочитанного» ─────────────────

func TestLicenseSubjectGate_CensusSeparatesCleanFromUnread(t *testing.T) {
	_, empty := injLicenseScan(injLicenseCorpus{"README.md": "нет тут лицензии\n"})
	if empty.licenses != 0 {
		t.Fatalf("перепись насчитала файлы LICENSE там, где их нет: %s", empty)
	}
	if empty.indexed == 0 {
		t.Fatalf("перепись не считает осмотренное вовсе: %s", empty)
	}
	_, seen := injLicenseScan(injLicenseCorpus{"LICENSE": injLicenseBody("Kachō (kacho)")})
	if seen.licenses != 1 {
		t.Fatalf("перепись не отличает прочитанное от пустого: %s", seen)
	}
}

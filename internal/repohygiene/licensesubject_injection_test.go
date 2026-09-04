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
	return scanLicenseSubjects(c.paths(), c.read, nil)
}

// injLicenseScanVendored — тот же разбор, но каталог объявлен корнем
// вендоренного пространства. Корень подаётся ВХОДОМ, а не выводится тут заново:
// деривация живёт в единственном экземпляре (vendorednotice.go).
func injLicenseScanVendored(c injLicenseCorpus, roots ...string) ([]licenseSubjectFinding, licenseSubjectCensus) {
	set := map[string]bool{}
	for _, r := range roots {
		set[r] = true
	}
	return scanLicenseSubjects(c.paths(), c.read, set)
}

// injApacheCopy — копия ЧУЖОЙ лицензии: строки предмета в ней нет и быть не может.
const injApacheCopy = "\n" +
	"                                 Apache License\n" +
	"                           Version 2.0, January 2004\n" +
	"                        http://www.apache.org/licenses/\n"

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

// Место, для которого лицензия не объявлена, — находка, а не пропуск. Без этой
// ветви первый же LICENSE в новом каталоге выпал бы из наблюдения молча.
//
// Координата оси сменена 2026-09-04: `pkg/LICENSE` был неизвестным местом ровно
// пока предмет фундамента был не решён. Решение владельца его закрыло, и ось,
// оставленная на прежней координате, стала бы утверждать обратное принятому —
// то есть краснела бы на верном дереве. Неизвестным осталось место ВНУТРИ
// уровня, но не его корень.
func TestLicenseSubjectGate_RedsOnAnUndeclaredLocation(t *testing.T) {
	findings, census := injLicenseScan(injLicenseCorpus{
		"pkg/internal/LICENSE": injLicenseBody("Kachō (kacho)"),
	})
	if len(findings) != 1 {
		t.Fatalf("LICENSE в неизвестном месте обязан быть находкой, получено %d (перепись: %s)",
			len(findings), census)
	}
	if census.derived != 0 {
		t.Fatalf("предмет не выводится для неизвестного места, а перепись насчитала %d", census.derived)
	}
	if !strings.Contains(findings[0].String(), "лицензия не объявлена") {
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
	if census.licenses != 4 || census.derived != 4 || census.withSubj != 4 {
		t.Fatalf("близнецы не дошли до предиката: %s — молчание тогда означает "+
			"«не читал», а не «сошлось»", census)
	}
}

// ── уровни с ДРУГОЙ формой лицензии: предмета у них нет вовсе ────────────────

// injApacheBody / injAGPLBody — тела, узнаваемые по тем же маркерам, что и
// живые файлы. Форма важна: у обеих лицензий параметра `Licensed Work:` НЕТ, и
// гейт обязан не требовать его от них.
func injApacheBody() string {
	return "                                 Apache License\n" +
		"                           Version 2.0, January 2004\n"
}

func injAGPLBody() string {
	return "                    GNU AFFERO GENERAL PUBLIC LICENSE\n" +
		"                       Version 3, 19 November 2007\n"
}

// Корень уровня с ВЕРНЫМ телом — молчание. Положительный контроль к двум осям
// ниже: без него их красное зеленело бы на чём угодно.
func TestLicenseSubjectGate_SilentOnTierRootsWithTheirOwnLicenseText(t *testing.T) {
	findings, census := injLicenseScan(injLicenseCorpus{
		"pkg/LICENSE":          injApacheBody(),
		"proto/LICENSE":        injApacheBody(),
		"services/iam/LICENSE": injAGPLBody(),
	})
	if len(findings) != 0 {
		t.Fatalf("верные корни уровней объявлены находкой: %v", findings)
	}
	if census.derived != 3 {
		t.Fatalf("ожидание не выведено для корней уровней: %s", census)
	}
	if census.withSubj != 0 {
		t.Fatalf("у Apache-2.0 и AGPL-3.0 параметра предмета НЕТ, а перепись насчитала %d — "+
			"требовать его от них значило бы требовать строки, которой в лицензии не бывает",
			census.withSubj)
	}
}

// Тело не той лицензии, которую объявляет уровень: файл валиден для всякого
// читателя, кроме юриста, и без этой оси прошёл бы молча.
func TestLicenseSubjectGate_RedsWhenTierRootCarriesAnotherLicenseText(t *testing.T) {
	findings, _ := injLicenseScan(injLicenseCorpus{
		"pkg/LICENSE": injLicenseBody("Kachō (kacho)"),
	})
	if len(findings) != 1 {
		t.Fatalf("ожидалась одна находка, получено %d: %v", len(findings), findings)
	}
	got := findings[0].String()
	for _, want := range []string{"pkg/LICENSE", "фундамент", "Apache-2.0"} {
		if !strings.Contains(got, want) {
			t.Errorf("находка не называет %q: %s", want, got)
		}
	}
}

// Обратное направление той же оси: вынесенный продукт под текстом монорепо.
func TestLicenseSubjectGate_RedsWhenTheProductCarriesTheMonorepoLicenseText(t *testing.T) {
	findings, _ := injLicenseScan(injLicenseCorpus{
		"services/iam/LICENSE": injLicenseBody("Kachō IAM (kacho-iam)"),
	})
	if len(findings) != 1 || !strings.Contains(findings[0].String(), "AGPL-3.0-or-later") {
		t.Fatalf("текст монорепо у вынесенного продукта не распознан: %v", findings)
	}
}

// Корень уровня BUSL предмет по-прежнему НЕСЁТ — половина, которую легко
// потерять при заведении второй формы.
func TestLicenseSubjectGate_BuslTierRootStillCarriesItsSubject(t *testing.T) {
	findings, census := injLicenseScan(injLicenseCorpus{
		"LICENSE": injLicenseBody("Kachō Compute (kacho-compute)"),
	})
	if len(findings) != 1 || !strings.Contains(findings[0].String(), "`kacho`") {
		t.Fatalf("предмет корня монорепо перестал проверяться: %v", findings)
	}
	if census.withSubj != 1 {
		t.Fatalf("форма с предметом не сосчитана: %s", census)
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

// ── ось: копия ЧУЖОЙ лицензии в корне вендоренного пространства ──────────────

// Законный близнец: чужая копия там, где ей и место. Требовать от неё нашей
// строки предмета нельзя — она не наша.
func TestLicenseSubjectGate_SilentOnAVendoredLicenseCopy(t *testing.T) {
	findings, census := injLicenseScanVendored(injLicenseCorpus{
		"proto/google/LICENSE": injApacheCopy,
	}, "proto/google")
	if len(findings) != 0 {
		t.Fatalf("копия чужой лицензии объявлена находкой: %v", findings)
	}
	if census.vendored != 1 || census.derived != 0 {
		t.Fatalf("пропуск не назван переписью: %s", census)
	}
}

// Та же копия ВНЕ корня вендоренного пространства — по-прежнему находка.
// Контроль послабления: оно не течёт за пределы своего предмета.
func TestLicenseSubjectGate_RedsOnTheSameCopyOutsideAVendorRoot(t *testing.T) {
	// Место выбрано так, чтобы объявленная уровнем лицензия ОТЛИЧАЛАСЬ от чужой
	// копии: у pkg/ соседняя полоса объявила Apache-2.0, и копия Apache там стала
	// законной — предмет инъекции от этого не изменился, изменилось место.
	findings, census := injLicenseScanVendored(injLicenseCorpus{
		"services/vpc/LICENSE": injApacheCopy,
	}, "proto/google")
	if len(findings) != 1 {
		t.Fatalf("чужая копия вне корня обязана быть находкой, получено %d (перепись: %s)",
			len(findings), census)
	}
	if census.vendored != 0 {
		t.Fatalf("послабление применилось не к своему предмету: %s", census)
	}
}

// НАШ файл, положенный в корень вендоренного пространства, послаблением НЕ
// накрывается: он несёт строку предмета, то есть объявляет НАШУ лицензию на
// чужой код. Вторая половина двойного признака — ради этой оси она и заведена.
func TestLicenseSubjectGate_RedsWhenOurLicenseSitsInAVendorRoot(t *testing.T) {
	findings, census := injLicenseScanVendored(injLicenseCorpus{
		"proto/google/LICENSE": injLicenseBody("Kachō (kacho)"),
	}, "proto/google")
	if len(findings) != 1 {
		t.Fatalf("наш файл в чужом корне обязан быть находкой, получено %d (перепись: %s)",
			len(findings), census)
	}
	if census.vendored != 0 {
		t.Fatalf("наш файл принят за чужую копию: %s", census)
	}
}

// Проба антимаски: рядом с законным пропуском находка объявляется по-прежнему.
func TestLicenseSubjectGate_VendoredExemptionDoesNotMaskAFinding(t *testing.T) {
	findings, census := injLicenseScanVendored(injLicenseCorpus{
		"proto/google/LICENSE": injApacheCopy,
		"LICENSE":              injLicenseBody("Kachō Compute (kacho-compute)"),
	}, "proto/google")
	if len(findings) != 1 || findings[0].file != "LICENSE" {
		t.Fatalf("послабление замаскировало находку: %v", findings)
	}
	if census.vendored != 1 || census.licenses != 2 {
		t.Fatalf("перепись разошлась: %s", census)
	}
}

// Послабление ИСТЕКАЕТ САМО: уехал вендоренный код — корня больше нет, и
// оставшийся файл снова находка. Без этой оси запись пережила бы свой предмет.
func TestLicenseSubjectGate_VendoredExemptionExpiresWithItsSubject(t *testing.T) {
	corpus := injLicenseCorpus{"proto/google/LICENSE": injApacheCopy}
	if findings, _ := injLicenseScanVendored(corpus, "proto/google"); len(findings) != 0 {
		t.Fatalf("предпосылка оси: при живом корне пропуск обязан быть: %v", findings)
	}
	findings, census := injLicenseScanVendored(corpus) // корней больше нет
	if len(findings) != 1 {
		t.Fatalf("послабление пережило свой предмет: находок %d (перепись: %s)",
			len(findings), census)
	}
	// Причина та же, формулировка сменилась вместе с переходом гейта на отображение
	// путь→лицензия: раньше «предмет не объявлен», теперь «лицензия не объявлена».
	if !strings.Contains(findings[0].String(), "лицензия не объявлена") {
		t.Fatalf("находка не называет причину: %s", findings[0])
	}
}

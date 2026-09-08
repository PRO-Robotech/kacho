// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// licensescope_injection_test.go — доказательство способности гейта
// licensescope.go упасть и смолчать.
//
// Гейт судит прозу правового документа, и вакуумным его сделать проще всего
// двумя способами: распознаватель перестаёт видеть форму (тогда молчание
// означает «не прочитал»), либо суждение вырождается в поиск подстроки (тогда
// красное не зависит от дерева и приходит на верном тексте). Обе стороны
// проверяются здесь, и рядом с каждым отрицанием стоит законный близнец той же
// формы, отличающийся ОДНИМ фактом.
//
// Корпус синтетический: гейт разложен на чистую функцию и обход, поэтому
// доказательство не пишет в живое дерево и не зависит от того, что в нём лежит
// сегодня.
package repohygiene

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// injScopeBody — тело BUSL с подставленным хвостом абзаца области. Форма
// воспроизводит живой файл: перенос хвоста, строка копирайта внутри абзаца и
// следующий параметр, которым абзац кончается.
func injScopeBody(subject, scopeTail string) string {
	return "" +
		"Business Source License 1.1\n" +
		"\n" +
		"Parameters\n" +
		"\n" +
		"Licensor:             PRO-Robotech\n" +
		"\n" +
		"Licensed Work:        " + subject + " and all source code, configuration,\n" +
		"                      documentation, and other materials " + scopeTail + "\n" +
		"                      The Licensed Work is (c) PRO-Robotech.\n" +
		"\n" +
		"Change Date:          None.\n" +
		"\n" +
		"You must conspicuously display this License on each copy of the Licensed\n" +
		"Work, except as stated in the terms, including any part of it that carries\n" +
		"its own LICENSE file.\n"
}

// Три хвоста абзаца области: наблюдавшийся, исправленный и промежуточные.
const (
	injScopeWholeRepo = "in this repository."
	injScopeOwnDir    = "in the directory that contains this License file, including its\n" +
		"                      subdirectories."
	injScopeOwnDirYielding = "in the directory that contains this License file, including its\n" +
		"                      subdirectories, EXCEPT any subdirectory that contains its own\n" +
		"                      LICENSE file."
	injScopeWholeRepoYielding = "in this repository, EXCEPT any directory that contains its own\n" +
		"                      LICENSE file."
)

const (
	injScopeApacheBody = "                                 Apache License\n" +
		"                           Version 2.0, January 2004\n"
	injScopeAGPLBody = "                    GNU AFFERO GENERAL PUBLIC LICENSE\n" +
		"                       Version 3, 19 November 2007\n"
)

type injScopeCorpus map[string]string

func (c injScopeCorpus) paths() []string {
	out := make([]string, 0, len(c))
	for k := range c {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (c injScopeCorpus) read(rel string) ([]byte, error) {
	body, ok := c[rel]
	if !ok {
		return nil, fmt.Errorf("нет такого файла")
	}
	return []byte(body), nil
}

func injScopeScan(c injScopeCorpus) ([]licenseScopeFinding, licenseScopeCensus) {
	return scanLicenseScopes(c.paths(), c.read)
}

// ── сторона (а): дефект краснеет и называет координату ───────────────────────

// Наблюдавшийся дефект kacho#2161: корневой текст объявляет предметом ВСЁ, при
// поддереве под другой лицензией.
func TestLicenseScopeGate_RedsWhenRootSwallowsAPermissiveSubtree(t *testing.T) {
	findings, census := injScopeScan(injScopeCorpus{
		"LICENSE":     injScopeBody("Kachō (kacho)", injScopeWholeRepo),
		"pkg/LICENSE": injScopeApacheBody,
	})
	if len(findings) != 1 {
		t.Fatalf("ожидалась одна находка, получено %d (перепись: %s)", len(findings), census)
	}
	got := findings[0].String()
	for _, want := range []string{"LICENSE", "ВЕСЬ репозиторий", "pkg"} {
		if !strings.Contains(got, want) {
			t.Errorf("находка не называет %q: %s", want, got)
		}
	}
}

// Второй наблюдавшийся дефект той же развёртки: КОМПОНЕНТНЫЙ файл заведён
// копированием и объявляет своей областью весь репозиторий — то есть чужие
// уровни. В дереве таких оказалось семь, и поодиночке каждый выглядит верным.
func TestLicenseScopeGate_RedsWhenAComponentClaimsTheWholeRepository(t *testing.T) {
	findings, _ := injScopeScan(injScopeCorpus{
		"services/vpc/LICENSE": injScopeBody("Kachō VPC (kacho-vpc)", injScopeWholeRepo),
		"services/nlb/LICENSE": injScopeBody("Kachō NLB (kacho-nlb)", injScopeWholeRepo),
		"services/iam/LICENSE": injScopeAGPLBody,
	})
	if len(findings) != 2 {
		t.Fatalf("оба компонентных файла обязаны быть названы, получено %d: %v", len(findings), findings)
	}
	if findings[0].file != "services/nlb/LICENSE" || findings[1].file != "services/vpc/LICENSE" {
		t.Fatalf("находки не называют оба файла: %v", findings)
	}
	if !strings.Contains(findings[0].String(), "services/iam") {
		t.Fatalf("находка не называет поглощённый уровень: %s", findings[0])
	}
}

// Форма якоря, которой распознаватель не знает, — НАХОДКА, а не пропуск. Без
// этой ветви первая же правка текста вывела бы файл из наблюдения молча: гейт
// не дал бы ни красного, ни зелёного.
func TestLicenseScopeGate_RedsOnAnUnrecognisedScopeForm(t *testing.T) {
	findings, census := injScopeScan(injScopeCorpus{
		"LICENSE":     injScopeBody("Kachō (kacho)", "wherever they may be found."),
		"pkg/LICENSE": injScopeApacheBody,
	})
	if len(findings) != 1 {
		t.Fatalf("незнакомая форма обязана быть находкой, получено %d (перепись: %s)",
			len(findings), census)
	}
	if census.unanchored != 1 || census.anchored != 0 {
		t.Fatalf("перепись не отличает нераспознанное от распознанного: %s", census)
	}
	if !strings.Contains(findings[0].String(), "licenseScopeAnchors") {
		t.Fatalf("находка не называет, где чинится распознаватель: %s", findings[0])
	}
}

// Оговорка узнаётся ПАРОЙ. Половина первая: отрицание есть, механизм не назван.
func TestLicenseScopeGate_RedsOnANegationWithoutTheMechanism(t *testing.T) {
	findings, _ := injScopeScan(injScopeCorpus{
		"LICENSE":     injScopeBody("Kachō (kacho)", "in this repository, except as noted elsewhere."),
		"pkg/LICENSE": injScopeApacheBody,
	})
	if len(findings) != 1 {
		t.Fatalf("отрицание без названного механизма оговоркой не является: %v", findings)
	}
}

// Половина вторая: механизм назван, отрицания нет — такая фраза способна и
// РАСШИРЯТЬ область, поэтому оговоркой не считается.
func TestLicenseScopeGate_RedsOnTheMechanismWithoutANegation(t *testing.T) {
	findings, _ := injScopeScan(injScopeCorpus{
		"LICENSE": injScopeBody("Kachō (kacho)",
			"in this repository, including any directory that contains its own\n"+
				"                      LICENSE file."),
		"pkg/LICENSE": injScopeApacheBody,
	})
	if len(findings) != 1 {
		t.Fatalf("механизм без отрицания оговоркой не является: %v", findings)
	}
}

// Оговорка ЗА пределами абзаца параметра не считается: область определяет
// абзац, а слово `License` стоит в теле BUSL десятки раз. Тело фикстуры несёт
// ровно такую фразу ниже по файлу — без сужения до абзаца гейт молчал бы здесь
// и на всём живом дереве.
func TestLicenseScopeGate_RedsWhenTheYieldSitsOutsideTheParameterBlock(t *testing.T) {
	body := injScopeBody("Kachō (kacho)", injScopeWholeRepo)
	if !strings.Contains(body, "its own LICENSE file") {
		t.Fatal("фикстура утратила фразу вне абзаца — ось перестала что-либо доказывать")
	}
	findings, _ := injScopeScan(injScopeCorpus{
		"LICENSE":     body,
		"pkg/LICENSE": injScopeApacheBody,
	})
	if len(findings) != 1 {
		t.Fatalf("оговорка вне абзаца параметра принята за оговорку: %v", findings)
	}
}

// ── сторона (б): законный близнец обязан молчать ─────────────────────────────

// Верное дерево: корень уступает вложенным, компоненты привязаны к своему
// каталогу и тоже уступают.
func TestLicenseScopeGate_SilentOnACorrectTree(t *testing.T) {
	findings, census := injScopeScan(injScopeCorpus{
		"LICENSE":              injScopeBody("Kachō (kacho)", injScopeOwnDirYielding),
		"gateway/LICENSE":      injScopeBody("Kachō API Gateway (kacho-api-gateway)", injScopeOwnDirYielding),
		"services/vpc/LICENSE": injScopeBody("Kachō VPC (kacho-vpc)", injScopeOwnDirYielding),
		"pkg/LICENSE":          injScopeApacheBody,
		"services/iam/LICENSE": injScopeAGPLBody,
	})
	if len(findings) != 0 {
		t.Fatalf("верное дерево объявлено находкой: %v — гейт, краснеющий на верном "+
			"тексте, отключают первым", findings)
	}
	if census.licenses != 5 || census.withScope != 3 || census.anchored != 3 || census.yielding != 3 {
		t.Fatalf("близнецы не дошли до предиката: %s — молчание тогда означает "+
			"«не читал», а не «сошлось»", census)
	}
	if census.swallowed == 0 {
		t.Fatal("уступать было нечему — молчание вакуумно и ничего не доказывает")
	}
}

// Тот же корневой якорь «весь репозиторий», но с оговоркой — законный близнец
// первой оси: отличается ОДНИМ фактом.
func TestLicenseScopeGate_SilentOnAWholeRepoScopeThatYields(t *testing.T) {
	findings, census := injScopeScan(injScopeCorpus{
		"LICENSE":     injScopeBody("Kachō (kacho)", injScopeWholeRepoYielding),
		"pkg/LICENSE": injScopeApacheBody,
	})
	if len(findings) != 0 {
		t.Fatalf("область с оговоркой объявлена находкой: %v", findings)
	}
	if census.yielding != 1 || census.swallowed != 1 {
		t.Fatalf("оговорка не дошла до предиката: %s", census)
	}
}

// Суждение — ОБ ОТНОШЕНИИ К ДЕРЕВУ, а не о подстроке: тот же самый текст, что
// краснеет выше, молчит в дереве, где уступать некому. Без этой оси гейт был бы
// поиском фразы и краснел бы независимо от того, что в дереве лежит.
func TestLicenseScopeGate_SilentOnTheSameTextWhenNothingIsSwallowed(t *testing.T) {
	body := injScopeBody("Kachō (kacho)", injScopeWholeRepo)
	findings, census := injScopeScan(injScopeCorpus{"LICENSE": body})
	if len(findings) != 0 {
		t.Fatalf("текст без соседних лицензий объявлен находкой: %v — тогда гейт судит "+
			"подстроку, а не отношение к дереву", findings)
	}
	if census.withScope != 1 || census.anchored != 1 {
		t.Fatalf("файл не дошёл до предиката: %s", census)
	}
	if census.swallowed != 0 {
		t.Fatalf("поглощать было некого, а перепись насчитала %d", census.swallowed)
	}
}

// Формы без параметра области (Apache-2.0, AGPL-3.0, копия чужой лицензии):
// область в них не объявляется ВОВСЕ, требовать её значило бы требовать строки,
// которой в лицензии не бывает.
func TestLicenseScopeGate_SilentOnFormsThatDeclareNoScopeAtAll(t *testing.T) {
	findings, census := injScopeScan(injScopeCorpus{
		"pkg/LICENSE":          injScopeApacheBody,
		"proto/LICENSE":        injScopeApacheBody,
		"proto/google/LICENSE": injScopeApacheBody,
		"services/iam/LICENSE": injScopeAGPLBody,
	})
	if len(findings) != 0 {
		t.Fatalf("формы без параметра области объявлены находкой: %v", findings)
	}
	if census.licenses != 4 || census.withScope != 0 {
		t.Fatalf("перепись не отличает «нечего судить» от «прочитано»: %s", census)
	}
}

// Пустой обход не даёт зелёного: перепись обязана показать ноль, и обходчик
// в licensescope_test.go на этом Fatal'ит.
func TestLicenseScopeGate_EmptyTraversalIsNotAPass(t *testing.T) {
	findings, census := injScopeScan(injScopeCorpus{})
	if len(findings) != 0 {
		t.Fatalf("на пустом корпусе находок быть не может: %v", findings)
	}
	if census.licenses != 0 || census.withScope != 0 || census.swallowed != 0 {
		t.Fatalf("пустой обход отчитался непустой переписью: %s", census)
	}
}

// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// bothidentityformsproducer_test.go — у входа «обе формы личности разом» за
// краем НЕТ производителя (приёмка KAN-AUTHN-1, редакция 2; задача продукта
// #2246).
//
// # Что случилось и почему это гейт, а не строка в приёмке
//
// Приёмка отозвала решение «обе формы разом — отказ»: полосы разведены
// ПОСТРОЕНИЕМ — край снимает арендаторское удостоверение перед пересылкой за
// себя. Держателей снимаемой ветки приёмка перечислила предикатом ПО ИМЕНИ
// пробы (`func TestKAN_DUP_01`) и получила два файла. Держателей было ТРИ:
// третий жил кейсом внутри пробы СОСЕДНЕГО семейства — перечня причин отказа,
// закрытого счётчиком, — и имени снятого сценария не содержал вовсе. Предикат
// по имени не видел его by construction.
//
// Между именем пробы и предметом отношения нет. Предмет — ВХОД, на котором
// ветка срабатывает; утверждение о нём живёт под любым именем и в любом
// семействе. Поэтому держателей ищет разбор входа, а не поиск имени.
//
// # Что этот гейт утверждает
//
// За краем производителя входа «удостоверение ВМЕСТЕ с переданной личностью»
// нет ни одного. Он остаётся ровно там, где его предмет — на самом крае, в
// пробах СНЯТИЯ: они собирают запрос с обеими формами затем, чтобы показать,
// что за край уезжает только одна. Эта группа обязана быть НЕПУСТОЙ: опустев,
// она означала бы, что разборщик ослеп, а не что дерево чисто.
//
// # Единица счёта названа, потому что их две
//
// «Место» — координата (файл и строка). «Держатель» — проба, в теле которой
// место стоит: одно утверждение может собирать вход двумя строками. Предикат
// приёмки считал ФАЙЛЫ и получил 2; держателей было 3, и два из них лежали в
// одном файле.
package repohygiene

import (
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// bothFormsCorpusAnchors — по чему выбирается КОРПУС: пакет ЧИТАТЕЛЯ
// предъявленного. Снятая ветка жила в нём, и вход «обе формы разом» имеет
// смысл ровно там, где читатель его увидит; позвать читателя, не импортировав
// его пакет, нельзя.
//
// Отбор идёт по ИМПОРТУ, а не по упоминанию имени в тексте. Разница измерена:
// по упоминанию корпус — 780 файлов и 5 каталогов (в него попадают собственные
// фикстуры гейтов, где имя пакета стоит строкой), по импорту — 3 каталога.
//
// Расширять якорь до соседних пакетов КРАЯ нельзя, и это тоже измерено: якорь
// по узлу снятия втягивает весь край (11 каталогов, 16 держателей), где обе
// формы в одном запросе — обычное дело и предметом этого гейта не являются.
// Второй конец полосы добавляется КАТАЛОГОМ, поимённо, ниже.
//
// Чего этот отбор НЕ видит: пробу, дотягивающуюся до читателя через цепочку,
// собранную в третьем пакете, который сам читателя не импортирует. Признак —
// падение числа каталогов корпуса в переписи.
var bothFormsCorpusAnchors = []string{"/presentedcred"}

// bothFormsAllowedProducerDirs — АДЪЮДИКАЦИЯ каталогов, которым производить
// вход «обе формы разом» законно.
//
// Перечень выписан намеренно: «зачем этот вход собран» есть суждение, и
// машинного предиката у него нет. Машинно проверяется ДРУГОЕ — что перечень
// сходится с деревом: каталог, начавший производить вход и здесь не названный,
// роняет гейт, и запись, которой больше нечего разрешать, — тоже.
var bothFormsAllowedProducerDirs = map[string]string{
	"gateway/internal/principalmeta": "пробы СНЯТИЯ на самом крае: вход собирается затем, " +
		"чтобы показать, что за край уезжает только одна форма",
}

// bothFormsExtraCorpusDirs — второй конец полосы, добавляемый к корпусу
// КАТАЛОГОМ. Он не импортирует читателя и по якорю не попал бы, а нужен как
// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: пока здесь держатели есть, ноль держателей у
// читателя означает вердикт, а не слепоту разборщика. Опустев, запись
// самоистекает — её ловит проверка осиротевших записей ниже.
var bothFormsExtraCorpusDirs = []string{"gateway/internal/principalmeta"}

// bothFormsRootEnv — ручка, меняющая ТОЛЬКО читаемое дерево.
//
// Она заведена затем, чтобы утверждение приёмки «на дереве ДО снятия предикат
// даёт три, а не два» можно было ПРОГНАТЬ, а не принять на слово:
//
//	git worktree add --detach <путь> <ревизия-до-снятия>
//	KACHO_BOTH_FORMS_ROOT=<путь> go test ./internal/repohygiene \
//	    -run TestBothIdentityFormsHaveNoProducerBehindTheEdge -count=1 -v
//
// Ручка не трогает НИ ОДНОГО решения: что считается формой, держателем и
// находкой, от неё не зависит. Корень печатается всегда — «прогнали по чужому
// дереву» обязано быть отличимо от «прогнали по своему».
const bothFormsRootEnv = "KACHO_BOTH_FORMS_ROOT"

func TestBothIdentityFormsHaveNoProducerBehindTheEdge(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	rootSource := "дерево этой рабочей копии"
	if v := os.Getenv(bothFormsRootEnv); v != "" {
		root, rootSource = v, "объявлено ручкой "+bothFormsRootEnv
	}
	t.Logf("корень: %s (%s)", root, rootSource)
	tt := newTrackedTree(t, root)

	// (1) Корпус: каталоги, чьи пробы вообще видят читателя предъявленного.
	sources := map[string][]byte{}
	corpusDirs := map[string]bool{}
	var testFilesInTree int
	for rel := range tt.files {
		if !strings.HasSuffix(rel, "_test.go") {
			continue
		}
		testFilesInTree++
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		sources[rel] = b
		if bothFormsImportsAnAnchor(rel, b) {
			corpusDirs[path.Dir(rel)] = true
		}
	}
	for _, dir := range bothFormsExtraCorpusDirs {
		corpusDirs[dir] = true
	}
	if testFilesInTree == 0 {
		t.Fatalf("перепись беспредметна: в составе дерева НОЛЬ файлов проб — сломан обход "+
			"состава, а не дерево чисто (прочитано файлов состава %d)", tt.count())
	}
	if len(corpusDirs) == 0 {
		t.Fatalf("перепись беспредметна: осмотрено %d файлов проб, каталогов, импортирующих "+
			"хотя бы один конец полосы (%v), НОЛЬ. Либо концы полосы сняты — тогда "+
			"снимается и этот гейт вместе с ними, — либо отбор корпуса перестал видеть "+
			"предмет.", testFilesInTree, bothFormsCorpusAnchors)
	}

	byDir := map[string]map[string][]byte{}
	var corpusFiles int
	for rel, b := range sources {
		d := path.Dir(rel)
		if !corpusDirs[d] {
			continue
		}
		if byDir[d] == nil {
			byDir[d] = map[string][]byte{}
		}
		byDir[d][rel] = b
		corpusFiles++
	}

	// (2) Разбор корпуса по пакетам: помощник, кладущий форму в запрос, живёт в
	// соседнем файле того же каталога, и разбор по одному файлу не увидел бы ни
	// одного производителя.
	var (
		sites                             []BothFormsSite
		funcs, calls, credSites, fwdSites int
		carriers, substitutions           int
		credProducers, fwdProducers       int
		dirsScanned                       int
	)
	for _, d := range bothFormsSortedKeys(corpusDirs) {
		found, census, err := ScanBothIdentityFormsProducers(byDir[d])
		if err != nil {
			t.Fatalf("разбор корпуса %s: %v — разбор сломан, и его молчание сказано ни о чём", d, err)
		}
		dirsScanned++
		funcs += census.Funcs
		calls += census.Calls
		credSites += census.CredentialSites
		fwdSites += census.ForwardedSites
		carriers += census.Carriers
		substitutions += census.Substitutions
		credProducers += len(census.CredentialProducers)
		fwdProducers += len(census.ForwardedProducers)
		sites = append(sites, found...)
	}

	t.Logf("перепись: файлов проб в составе дерева %d · в корпусе %d · каталогов корпуса %d · "+
		"функций %d · вызовов %d · подстановок имён %d · носителей запроса %d",
		testFilesInTree, corpusFiles, dirsScanned, funcs, calls, substitutions, carriers)
	t.Logf("перепись форм: помощников-производителей удостоверения %d · переданной личности %d · "+
		"мест с удостоверением %d · мест с переданной личностью %d",
		credProducers, fwdProducers, credSites, fwdSites)
	t.Logf("найдено: мест «обе формы разом» %d · держателей (проб) %d",
		len(sites), AdjudicateBothFormsProducers(sites, bothFormsAllowedProducerDirs).Holders)

	// (3) Предпосылка разборщика. Ноль мест ЛЮБОЙ из форм означает, что признак
	// не производится ничем, и ноль держателей сказано ни о чём.
	if credSites == 0 {
		t.Fatalf("в корпусе (%d файлов) не опознано НИ ОДНОГО места с предъявленным "+
			"удостоверением: разборщик ослеп либо ключ удостоверения записывается формой, "+
			"которой он не знает. Ноль держателей на таком разборе — молчание, а не вердикт.",
			corpusFiles)
	}
	if fwdSites == 0 {
		t.Fatalf("в корпусе (%d файлов) не опознано НИ ОДНОГО места с переданной личностью: "+
			"разборщик ослеп либо ключи личности записываются формой, которой он не знает.",
			corpusFiles)
	}

	// (4) Адъюдикация — ТОЙ ЖЕ функцией, которую гоняет инъекция.
	verdict := AdjudicateBothFormsProducers(sites, bothFormsAllowedProducerDirs)
	for _, dir := range bothFormsSortedKeys(bothFormsDirSet(bothFormsAllowedProducerDirs)) {
		t.Logf("законный производитель: %s — %s (мест %d)",
			dir, bothFormsAllowedProducerDirs[dir], verdict.AllowedByDir[dir])
	}

	// (5) Находка: производитель входа появился ЗА краем.
	if len(verdict.Offenders) > 0 {
		var lines []string
		for _, s := range verdict.Offenders {
			lines = append(lines, s.File+":"+strconv.Itoa(s.Line)+"  "+s.Func+"  ["+s.Form+"] "+s.Why)
		}
		t.Errorf("вход «предъявленное удостоверение ВМЕСТЕ с переданной личностью» собран "+
			"там, где его больше никто не производит:\n  %s\n\n"+
			"Приёмка KAN-AUTHN-1 редакцией 2 развела полосы ПОСТРОЕНИЕМ: край снимает "+
			"арендаторское удостоверение перед пересылкой за себя, установив личность сам. "+
			"Сочетания двух форм в одном запросе за краем не бывает, поэтому утверждение о "+
			"нём — зелёный вердикт о мире, которого нет: покраснеть оно не может ни при "+
			"каком поведении продукта.\n"+
			"Исходов два: снять утверждение вместе с предметом либо перенести его на самый "+
			"край (%s), где вход производится и означает ровно снятие.",
			strings.Join(lines, "\n  "),
			strings.Join(bothFormsSortedKeys(bothFormsDirSet(bothFormsAllowedProducerDirs)), ", "))
	}

	// (6) Самоистечение адъюдикации: запись, которой больше нечего разрешать.
	for _, dir := range verdict.OrphanEntries {
		t.Errorf("запись адъюдикации потеряла предмет: каталог %s больше не производит "+
			"вход «обе формы разом». Либо пробы снятия ушли — тогда снимается и запись, — "+
			"либо разборщик перестал видеть их форму, и тогда ноль держателей за краем "+
			"означает слепоту, а не чистоту.", dir)
	}
}

// bothFormsSortedKeys / setOf / bothFormsDirSet / itoa — мелочи вывода: находка обязана
// печататься в устойчивом порядке, иначе два прогона над одним деревом дают
// разный текст.
func bothFormsSortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func bothFormsDirSet(m map[string]string) map[string]bool {
	out := map[string]bool{}
	for k := range m {
		out[k] = true
	}
	return out
}

// bothFormsImportsAnAnchor — импортирует ли файл проб один из концов полосы.
//
// Разбор берёт только раздел импортов: он дёшев на тысячах файлов и, главное,
// судит по УЗЛУ, а не по подстроке — имя пакета встречается и в комментарии, и
// в строке фикстуры, и по упоминанию корпус вырос бы всемеро.
func bothFormsImportsAnAnchor(rel string, src []byte) bool {
	f, err := parser.ParseFile(token.NewFileSet(), rel, src, parser.ImportsOnly)
	if err != nil {
		return false
	}
	for _, imp := range f.Imports {
		p := strings.Trim(imp.Path.Value, `"`)
		for _, anchor := range bothFormsCorpusAnchors {
			if strings.HasSuffix(p, anchor) {
				return true
			}
		}
	}
	return false
}

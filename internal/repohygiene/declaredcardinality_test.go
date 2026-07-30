// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// declaredcardinality_test.go — гейт против объявленного предела без исполнителя.
//
// Предмет. В объявлениях сообщений есть семейство расширений-проверок
// (`(size)`, `(length)`, `(required)`, `(pattern)`…). Их не читает НИ ОДНА строка
// прод-кода: генератор кода их не разбирает, интерсептора-проверяльщика нет ни в
// одном сервисе, а единственный читатель расширений в дереве разбирает совсем
// другие опции — про права. То есть объявление `(size) = "<=8"` было обещанием,
// за которым не стоял никто: поле принимало сколько угодно элементов.
//
// Это особенно дорого там, где кратность повторяемого поля умножает работу за
// пределами процесса: список интерфейсов машины стоит ОДНОГО обращения к соседу на
// элемент, а сосед каждое такое обращение ещё и авторизует у себя.
//
// Чем гейт НЕ является. Он не делает читателя из всего семейства расширений — их в
// дереве больше тысячи, они принадлежат девяти доменам, и общий проверяльщик менял
// бы поведение всех сразу. Гейт закрывает ОДНО измерение (кратность повторяемых
// полей) в ОДНОЙ зоне (машины и блочное хранение) и ЯВНО объявляет свою область:
// см. scannedPackages и утверждение об объёме осмотренного в логе. Остальные домены
// — открытый остаток, а не молчание.
//
// Освобождения. Поле, которого не читает ни одна строка прод-кода, работы не
// гейтит — гейтить там нечего. Но такое освобождение обязано ЖИТЬ, пока живёт его
// предпосылка: у каждой записи есть проверка «читателя по-прежнему нет», и
// появление читателя красит гейт. Иначе список освобождений переживёт свой предмет
// и станет описанием вчерашнего дерева.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// scannedPackages — область гейта: пакеты объявлений, за которые отвечает эта
// зона. Расширение области — отдельная работа с владельцем соответствующего
// домена, а не строчка сюда.
var scannedPackages = []string{
	"proto/kacho/cloud/compute/v1",
	"proto/kacho/cloud/storage/v1",
}

// enforcer — где живёт исполнитель объявленного предела и как он называется.
//
// Запись обязана называть КОНСТАНТУ, а не «проверяется где-то»: гейт читает её
// значение и сверяет с объявленным. Разъехавшись, объявление и проверка дают ровно
// тот дефект, ради которого гейт заведён, — только теперь с двумя источниками
// истины вместо одного.
type enforcer struct {
	file  string
	ident string
}

// cardinalityEnforcer — ключ «<сообщение>.<поле>» → исполнитель.
var cardinalityEnforcer = map[string]enforcer{
	"CreateInstanceRequest.secondary_volume_specs": {
		file:  "services/compute/internal/domain/constants.go",
		ident: "MaxSecondaryVolumeSpecsPerInstance",
	},
}

// unreadFields — освобождения: поле не читает ни одна строка прод-кода, поэтому
// предел ничего не гейтит. Каждая запись несёт ПРОВЕРЯЕМУЮ предпосылку: маркер,
// присутствие которого в прод-дереве означает, что читатель появился и
// освобождение истекло.
var unreadFields = map[string]exemption{
	"ContainerSolutionSpec.secrets": {
		why:    "конфигурация приложения не читается ни одной строкой прод-кода compute — гейтить нечего",
		reader: "GetApplication(",
	},
	"ContainerSolutionSpec.environment": {
		why:    "конфигурация приложения не читается ни одной строкой прод-кода compute — гейтить нечего",
		reader: "GetApplication(",
	},
	"BackupSpec.initial_policy_ids": {
		why:    "конфигурация приложения не читается ни одной строкой прод-кода compute — гейтить нечего",
		reader: "GetApplication(",
	},
	"RelocateInstanceRequest.network_interface_specs": {
		why:    "перемещение машины в сервисе не реализовано (хендлер его не несёт) — принимать нечего",
		reader: ") Relocate(",
	},
}

type exemption struct {
	why string
	// reader — фрагмент, появление которого в прод-дереве compute означает, что
	// читатель поля появился и освобождение больше не действует.
	reader string
}

// labelsMapLimit — объявление `(kacho.cloud.size) = "<=64"` на картах меток.
// Исполнитель у него общий и уже существует (pkg/validate.Labels), поэтому в
// поимённой таблице он не нужен; предпосылка проверяется отдельно
// (TestLabelsLimitPremiseHolds).
const labelsMapLimit = 64

// sizeAnnotation — объявление предела кратности: `(size) = "<=N"` либо
// `(kacho.cloud.size) = "<=N"`.
var sizeAnnotation = regexp.MustCompile(`\((?:kacho\.cloud\.)?size\)\s*=\s*"([^"]+)"`)

// fieldNameRe — имя повторяемого поля / карты в строке объявления.
var fieldNameRe = regexp.MustCompile(`(?m)^\s*(?:repeated\s+\S+|map<[^>]+>)\s+([a-z_0-9]+)\s*=`)

// messageNameRe — начало объявления сообщения.
var messageNameRe = regexp.MustCompile(`(?m)^message\s+([A-Za-z0-9_]+)\s*\{`)

// TestDeclaredCardinalityLimitsHaveAnEnforcer — сам гейт.
func TestDeclaredCardinalityLimitsHaveAnEnforcer(t *testing.T) {
	root := repoRoot(t)

	type decl struct{ key, bound, file string }
	var decls []decl
	scannedFiles := 0

	for _, pkg := range scannedPackages {
		dir := filepath.Join(root, pkg)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("%s не читается (%v) — область обхода гейта сломана", pkg, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".proto") {
				continue
			}
			body, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
			if rerr != nil {
				t.Fatalf("%s/%s: %v", pkg, e.Name(), rerr)
			}
			scannedFiles++
			for _, d := range sizeDeclarations(string(body)) {
				decls = append(decls, decl{key: d.message + "." + d.field, bound: d.bound, file: e.Name()})
			}
		}
	}

	if scannedFiles == 0 {
		t.Fatalf("гейт не прочитал ни одного объявления в %v — молчание ничего не доказывает", scannedPackages)
	}
	t.Logf("осмотрено файлов объявлений: %d (области: %s); объявленных пределов кратности: %d; "+
		"освобождений (поле без читателя): %d",
		scannedFiles, strings.Join(scannedPackages, ", "), len(decls), len(unreadFields))

	if len(decls) == 0 {
		t.Fatal("ни одного объявленного предела кратности не найдено — распознавание сломано " +
			"(пределы в этих объявлениях есть)")
	}

	var problems []string
	seen := make(map[string]bool, len(decls))
	for _, d := range decls {
		seen[d.key] = true

		// Карты меток — общий исполнитель (предпосылка проверяется отдельно).
		if strings.HasSuffix(d.key, ".labels") {
			if d.bound != "<="+strconv.Itoa(labelsMapLimit) {
				problems = append(problems, d.key+": объявлен предел "+d.bound+
					", а общий исполнитель меток знает "+strconv.Itoa(labelsMapLimit))
			}
			continue
		}
		if ex, ok := unreadFields[d.key]; ok {
			if strings.TrimSpace(ex.why) == "" {
				problems = append(problems, d.key+": освобождение без обоснования")
			}
			if hasProductionReader(t, root, ex.reader) {
				problems = append(problems, d.key+": освобождение истекло — читатель появился ("+
					ex.reader+" встречается в прод-дереве). Поле теперь что-то гейтит: заведи "+
					"константу, проверку в запросе и запись в cardinalityEnforcer")
			}
			continue
		}

		enf, ok := cardinalityEnforcer[d.key]
		if !ok {
			problems = append(problems, d.key+
				": предел объявлен, исполнителя нет. Расширения проверок из объявлений не читает ни одна "+
					"строка прод-кода, поэтому объявление само по себе ничего не ограничивает — "+
					"заведи константу и проверку в запросе, затем запись в cardinalityEnforcer "+
					"(либо, если поля не читает никто, — запись в unreadFields с проверяемой предпосылкой)")
			continue
		}
		got, gerr := goConstValue(root, enf.file, enf.ident)
		if gerr != nil {
			problems = append(problems, d.key+": исполнитель "+enf.file+"."+enf.ident+" не читается: "+gerr.Error())
			continue
		}
		if want := "<=" + strconv.Itoa(got); want != d.bound {
			problems = append(problems, d.key+": объявлено "+d.bound+", исполнитель "+enf.ident+
				" знает "+strconv.Itoa(got)+" — объявление и проверка разъехались")
		}
	}

	// Запись, которой больше нечего защищать, — находка.
	for key := range cardinalityEnforcer {
		if !seen[key] {
			problems = append(problems, key+": запись об исполнителе есть, а объявленного предела больше нет — удали запись")
		}
	}
	for key := range unreadFields {
		if !seen[key] {
			problems = append(problems, key+": освобождение есть, а объявленного предела больше нет — удали запись")
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		t.Errorf("объявленных пределов кратности без работающего исполнителя: %d\n  %s",
			len(problems), strings.Join(problems, "\n  "))
	}
}

// TestEnforcedCardinalityLimitsAreDeclared — обратная сторона: проверка, о которой
// контракт молчит, тоже расхождение. Вызывающий узнаёт о пределе отказом, а не из
// объявления, и не может рассчитать запрос заранее.
//
// Пока предел не объявлен в контракте, запись живёт здесь — с причиной, а не молча.
func TestEnforcedCardinalityLimitsAreDeclared(t *testing.T) {
	root := repoRoot(t)

	// Пределы, которые прод-код ПРОВЕРЯЕТ, но контракт пока не объявляет.
	undeclared := map[string]string{
		"CreateInstanceRequest.network_interface_specs": "" +
			"предел кратности проверяется (domain.MaxNetworkInterfaceSpecsPerInstance), но в объявление " +
			"поля не внесён: правка объявления требует перегенерации кода из объявлений, что задевает " +
			"весь домен и делается отдельно. Запись здесь — чтобы расхождение было видно, а не молчало.",
	}
	for key, why := range undeclared {
		if strings.TrimSpace(why) == "" {
			t.Errorf("%s: запись без причины", key)
		}
	}
	// Предпосылка: исполнитель на месте. Исчезнет — запись потеряет предмет.
	if _, err := goConstValue(root, "services/compute/internal/domain/constants.go",
		"MaxNetworkInterfaceSpecsPerInstance"); err != nil {
		t.Fatalf("исполнитель предела кратности интерфейсов не читается (%v) — запись потеряла предмет", err)
	}
	t.Logf("проверяемых, но не объявленных пределов: %d", len(undeclared))
}

// TestLabelsLimitPremiseHolds — предпосылка освобождения карт меток: общий
// исполнитель существует и знает то же число. Без этой проверки строка «у меток
// исполнитель общий» была бы утверждением без предмета — ровно тем, что ловит гейт.
func TestLabelsLimitPremiseHolds(t *testing.T) {
	root := repoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, "pkg/validate/validate.go"))
	if err != nil {
		t.Fatalf("pkg/validate/validate.go не читается: %v", err)
	}
	if !strings.Contains(string(body), "func Labels(") {
		t.Fatal("общего исполнителя меток (pkg/validate.Labels) больше нет — освобождение карт меток " +
			"в гейте потеряло предмет")
	}
	if !strings.Contains(string(body), strconv.Itoa(labelsMapLimit)) {
		t.Fatalf("общий исполнитель меток не содержит числа %d — объявление и проверка могли разъехаться",
			labelsMapLimit)
	}
}

// hasProductionReader — встречается ли маркер в прод-дереве compute (без тестов и
// без сгенерированного кода).
func hasProductionReader(t *testing.T, root, marker string) bool {
	t.Helper()
	found := false
	err := filepath.Walk(filepath.Join(root, "services/compute"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "vendor", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if strings.Contains(string(body), marker) {
			found = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("обход прод-дерева compute: %v", err)
	}
	return found
}

type sizeDecl struct{ message, field, bound string }

// sizeDeclarations выбирает из текста объявления тройки «сообщение → поле →
// объявленный предел». Сообщение в ключе обязательно: одно и то же имя поля живёт
// в разных сообщениях одного файла с РАЗНЫМИ пределами, и ключ без сообщения
// сводил бы их в одну запись.
func sizeDeclarations(text string) []sizeDecl {
	lines := strings.Split(text, "\n")
	var out []sizeDecl
	msg := ""
	for i, line := range lines {
		if m := messageNameRe.FindStringSubmatch(line); m != nil {
			msg = m[1]
			continue
		}
		m := fieldNameRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		// Блок опций поля: от строки объявления до ближайшего «;».
		block := line
		for j := i + 1; j < len(lines) && !strings.Contains(block, ";"); j++ {
			block += "\n" + lines[j]
		}
		if s := sizeAnnotation.FindStringSubmatch(block); s != nil {
			out = append(out, sizeDecl{message: msg, field: m[1], bound: strings.TrimSpace(s[1])})
		}
	}
	return out
}

// goConstValue читает целочисленное значение именованной константы из Go-файла.
func goConstValue(root, rel, ident string) (int, error) {
	body, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return 0, err
	}
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(ident) + `\s*=\s*(\d+)`)
	m := re.FindStringSubmatch(string(body))
	if m == nil {
		return 0, os.ErrNotExist
	}
	return strconv.Atoi(m[1])
}

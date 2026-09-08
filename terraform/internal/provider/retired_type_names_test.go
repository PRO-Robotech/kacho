// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

// Остаток прежних имён типов по дереву.
//
// Предмет узкий и назван точно: «осталось ли в дереве прежнее имя там, где оно уже ничего
// не называет». Имя, пережившее свой тип, читается арендатором как действующее — он пишет
// его в конфигурацию и получает «Invalid resource type». Обратный вопрос — «называет ли
// страница тип, которого в реестре нет» — предмет соседней пробы (documented_types_test.go):
// источник там другой, не остаток, а выдумка или опечатка.
//
// # Почему прежние имена дозволены ровно в двух местах, и это не список исключений
//
// Список прощённых пришлось бы вести руками, и он пережил бы свой предмет. Дозволение здесь
// СТРУКТУРНОЕ: прежнее имя законно (а) в словаре, который его и объявляет снятым, и (б) в
// источнике объявления переезда — `from = <прежнее имя>.<адрес>`. Второе не послабление, а
// требование: объявление переезда, не называющее прежнего имени, не работает вовсе. Всё
// прочее — находка.
//
// # Почему сверка идёт по ТОЧНОМУ имени, а не по приставке
//
// Дерево полно однофамильцев: `kacho_iam_fga_outbox` — таблица очереди, `kacho_iam_` —
// приставка имён метрик, `kacho_iam_subjects` — представление в схеме. Ни одно из них
// переименованием типов не затронуто. Узнавание по приставке объявило бы их находками —
// прибор, у которого все находки ложные, перестают читать. Снятые имена известны точно,
// поэтому и сверяются точно.

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// reMovedSource — строка, объявляющая ИСТОЧНИК переезда. Прежнее имя типа здесь законно и
// обязательно: без него объявление переезда не работает.
var reMovedSource = regexp.MustCompile(`(?m)^\s*from\s*=\s*([a-z0-9_]+)\.`)

// typeNameDictionaryFile — файл, объявляющий имена типов и снятые имена.
//
// Единственное место дерева, где снятое имя стоит само по себе: он и есть его объявление.
// Названо координатой, а не признаком, потому что предмет ровно один; появится второй —
// проба покраснеет на нём и потребует решения, а не примет его молча.
const typeNameDictionaryFile = "internal/provider/iam_type_names.go"

// Прежнее имя типа, оставшееся в дереве, читается как действующее.
func TestRetiredTypeNamesSurviveOnlyAsMoveSources(t *testing.T) {
	root := repoTreeRoot(t)
	if len(retiredResourceTypeNames) == 0 {
		t.Skip("снятых имён типов нет — переписывать нечего")
	}

	retired := make([]string, 0, len(retiredResourceTypeNames))
	for _, old := range retiredResourceTypeNames {
		retired = append(retired, old)
	}
	sort.Strings(retired)
	re := regexp.MustCompile(`\b(` + strings.Join(retired, "|") + `)\b`)

	files, err := treecorpus.Under(root)
	if err != nil {
		t.Fatalf("перепись дерева: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("обход дерева пуст — вердикт беспредметен")
	}

	dictionary := filepath.Join(root, "terraform", filepath.FromSlash(typeNameDictionaryFile))
	dictionarySeen := false

	var scanned, occurrences, asMoveSource int
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		body, err := os.ReadFile(path) // #nosec G304 -- путь пришёл из индекса репозитория
		if err != nil {
			// Двоичное и нечитаемое дерево не молчит: файл, который не прочли, обязан
			// быть назван, иначе «ноль находок» неотличимо от «ноль прочитанного».
			t.Fatalf("чтение %s: %v", path, err)
		}
		scanned++
		s := string(body)
		if !re.MatchString(s) {
			continue
		}
		if path == dictionary {
			dictionarySeen = true
			continue
		}

		// Строки объявления переезда собираются ОТДЕЛЬНО: имя в них законно, и
		// сверяется оно по номеру строки, а не по факту присутствия где-то в файле.
		legal := map[int]bool{}
		for _, idx := range reMovedSource.FindAllStringSubmatchIndex(s, -1) {
			legal[strings.Count(s[:idx[0]], "\n")] = true
		}

		rel, _ := filepath.Rel(root, path)
		for i, line := range strings.Split(s, "\n") {
			for _, m := range re.FindAllString(line, -1) {
				occurrences++
				if legal[i] {
					asMoveSource++
					continue
				}
				t.Errorf("%s:%d: снятое имя типа %s.\n"+
					"  %s\n"+
					"Имя, пережившее свой тип, читается как действующее: читатель напишет "+
					"его в конфигурацию и получит «Invalid resource type». Законных мест "+
					"два — словарь снятых имён и источник объявления переезда "+
					"(`from = %s.<адрес>`).",
					rel, i+1, m, strings.TrimSpace(line), m)
			}
		}
	}

	if !dictionarySeen {
		t.Errorf("словарь снятых имён %s не найден в дереве по координате %s — "+
			"либо он переехал, либо перепись читает не то дерево",
			typeNameDictionaryFile, dictionary)
	}
	t.Logf("осмотрено: файлов %d, снятых имён в словаре %d, вхождений в дереве %d "+
		"(из них источником переезда %d)",
		scanned, len(retiredResourceTypeNames), occurrences, asMoveSource)
}

// Страница, называющая тип службы доступа, называет ТОЛЬКО существующий.
//
// # Чем это дополняет соседнюю пробу и чем от неё отличается
//
// documented_types_test.go сверяет ПЕРЕЧНИ: страница, объявившая таблицу типов, обязана
// назвать все типы своего домена и не назвать несуществующих. Страниц с перечнем пять;
// страниц, УПОМИНАЮЩИХ тип в примере или в прозе, больше, и перечня у них нет — сверять
// с реестром там нечего, а вопрос «существует ли названное» остаётся.
//
// # Почему только семейство службы доступа
//
// Приставка платформы в прозе означает не только тип: `kacho_storage_outbox_backlog_depth`
// — имя метрики, `kacho_nlb_fga_register_outbox` — таблица очереди, `kacho_token` —
// переменная примера. Прибор, объявляющий их выдумками, перестают читать, и вместе с ним
// перестают читать настоящую находку. У семейства службы доступа такой неоднозначности
// сегодня нет — предикат назван ниже, и он же обязан покраснеть, когда она появится.
func TestPagesNameOnlyExistingAccessTypes(t *testing.T) {
	root := repoTreeRoot(t)
	known := registeredProviderTypeNames(t)
	if len(known) == 0 {
		t.Fatal("реестр провайдера пуст — сверять не с чем")
	}

	family := strings.SplitN(typeNameIAMAccount, "_", 2)[0]
	re := regexp.MustCompile(`\b` + family + `_[a-z0-9]+(?:_[a-z0-9]+)*\b`)

	pages, err := treecorpus.UnderWithSuffix(root, ".mdx")
	if err != nil {
		t.Fatalf("перепись страниц: %v", err)
	}
	if len(pages) == 0 {
		t.Fatal("страниц не найдено — обход пуст, вердикт беспредметен")
	}

	var naming, checked int
	for _, path := range pages {
		body, err := os.ReadFile(path) // #nosec G304 -- путь пришёл из индекса репозитория
		if err != nil {
			t.Fatalf("чтение %s: %v", path, err)
		}
		found := re.FindAllString(string(body), -1)
		if len(found) == 0 {
			continue
		}
		naming++
		rel, _ := filepath.Rel(root, path)
		for _, tok := range found {
			checked++
			if known[tok] {
				continue
			}
			t.Errorf("%s: названного типа %s в реестре провайдера НЕТ.\n"+
				"Читатель напишет его в конфигурацию и получит «Invalid resource type».\n"+
				"Если это НЕ имя типа — имя метрики, таблицы, переменной, — то приставка "+
				"%s_ перестала быть однозначной, и предикат узнавания обязан переехать "+
				"вместе с ней, а не быть снятым.", rel, tok, family)
		}
	}

	if checked == 0 {
		t.Fatal("на страницах не найдено ни одного имени типа службы доступа — предикат " +
			"узнавания устарел, и «фантомов нет» означало бы «ничего не прочитано»")
	}
	t.Logf("осмотрено: страниц %d, из них называют типы службы доступа %d, имён сверено %d, "+
		"типов в реестре %d", len(pages), naming, checked, len(known))
}

// repoTreeRoot — корень дерева продукта.
//
// Строится от каталога terraform/, а не от текущего каталога вызывающего: `go test ./...`
// из корня и `go test .` из пакета обязаны осматривать одно дерево.
func repoTreeRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Dir(terraformTreeRoot(t))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("корень дерева не опознан (%s): %v", root, err)
	}
	return root
}

// registeredProviderTypeNames — имена типов, которые провайдер объявляет СЕГОДНЯ.
//
// Спрашивается сам провайдер, а не список: перечень, выписанный рядом, разошёлся бы с
// реестром молча — ровно тем классом, который эти пробы и ловят.
func registeredProviderTypeNames(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	p := New().(*kachoProvider)
	for _, ctor := range p.Resources(context.Background()) {
		out[typeNameOfResource(ctor())] = true
	}
	for _, ctor := range p.DataSources(context.Background()) {
		out[typeNameOfDataSource(ctor())] = true
	}
	return out
}

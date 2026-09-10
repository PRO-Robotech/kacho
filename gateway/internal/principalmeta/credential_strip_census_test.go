// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package principalmeta_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// credential_strip_census_test.go — KAN-STRIP-03 приёмки KAN-AUTHN-1.
//
// # Предмет
//
// Радиус снятия берётся ПО СВОЙСТВУ («кто читает входящее удостоверение»), а не
// по каталогу, в котором правку заметили. Перепись обходит не-тестовое дерево,
// делит читателей на группы и печатает ОБЕ величины — сколько осмотрено и
// сколько отнесено к каждой группе.
//
// # Почему разборщик обязан знать ОБЕ формы записи имени
//
// Первый предикат этой редакции знал только строчную форму и дал 3 файла;
// знающий обе — 11. Разница не край и не редкость: заглавная форма столь же
// законна и в этом дереве обычна. Слепой предикат не дал бы ни красного, ни
// зелёного — он ПРОМОЛЧАЛ бы о восьми файлах, и радиус снятия был бы взят
// наугад.
//
// # Пустой обход роняет прогон
//
// «Ноль находок» обязано быть отличимо от «ноль прочитанного»: перепись,
// не нашедшая ни одного читателя, свидетельствует о сломанном обходе, а не о
// чистом дереве.

// credentialReaderGroup — куда отнесён читатель входящего удостоверения.
type credentialReaderGroup int

const (
	// groupWritesOutgoing — ставит заголовок в СВОЙ запрос к соседу. Не читатель.
	groupWritesOutgoing credentialReaderGroup = iota
	// groupReadsAtTheEdge — читает на самом крае, ДО пересылки. Снятие его не
	// трогает: оно происходит после чтения.
	groupReadsAtTheEdge
	// groupTransportMarker — перечень «не писать в журнал» в общем транспорте.
	// Маркер, а не читатель.
	groupTransportMarker
	// groupOwnSurface — читает на СОБСТВЕННОЙ поверхности службы, до которой
	// край запрос не проксирует. Продолжает читать сам.
	groupOwnSurface
	// groupBehindTheEdge — читает на поверхности, до которой край ПРОКСИРУЕТ
	// запрос. Ровно этот читатель — предмет приёмки KAN-AUTHN-1.
	groupBehindTheEdge
	// groupNamesTheKeyToSearchForIt — НЕ читатель и не узел снятия: гейт,
	// называющий ключ затем, чтобы ИСКАТЬ его в дереве. Перепись находит его по
	// тому же признаку, что и читателей, и группа ему нужна отдельная: отнести
	// гейт к читателям значило бы сказать о нём неправду, а вычесть из обхода —
	// завести исключение, которому нечего исключать.
	groupNamesTheKeyToSearchForIt
	// groupStripsIt — САМ узел снятия. Он имя удостоверения не читает, а
	// объявляет: перепись находит его по тому же признаку, что и читателей, и
	// адъюдицировать его надо отдельной группой — отнести к читателям значило
	// бы сказать о нём неправду, а вычесть из обхода — завести исключение,
	// которому нечего исключать.
	groupStripsIt
)

func (g credentialReaderGroup) String() string {
	switch g {
	case groupWritesOutgoing:
		return "пишет исходящее"
	case groupReadsAtTheEdge:
		return "читает на самом крае"
	case groupTransportMarker:
		return "маркер общего транспорта"
	case groupOwnSurface:
		return "читает на собственной поверхности"
	case groupBehindTheEdge:
		return "читает ЗА краем"
	case groupNamesTheKeyToSearchForIt:
		return "называет ключ, чтобы искать его"
	case groupStripsIt:
		return "САМ узел снятия"
	default:
		return "не адъюдицирован"
	}
}

// credentialReaderCensus — АДЪЮДИКАЦИЯ по каждому файлу, а не по каталогу.
//
// Перечень выписан намеренно: группа файла есть суждение о том, что этот файл
// делает, и машинного предиката у него нет. Зато машинно проверяемо ДРУГОЕ —
// что перечень сходится с деревом: файл, появившийся и не адъюдицированный,
// роняет пробу, и файл перечня, исчезнувший из дерева, — тоже.
var credentialReaderCensus = map[string]credentialReaderGroup{
	// Здесь стояли две записи службы доступа (клиенты внешнего провайдера
	// личности) и одна её же ниже, на собственной поверхности. Все три сняты
	// ВМЕСТЕ СО СВОИМ ПРЕДМЕТОМ: служба вынесена отдельным репозиторием
	// (задача #1111). Запись, чей файл в дереве не резолвится, — находка, а не
	// след: перечень перестал бы сходиться с деревом, и следующий читатель искал
	// бы координату, которой нет.
	"terraform/internal/client/client.go": groupWritesOutgoing,

	"gateway/internal/handler/logout_handler.go":          groupReadsAtTheEdge,
	"gateway/internal/middleware/auth.go":                 groupReadsAtTheEdge,
	"gateway/internal/middleware/dpop_http_middleware.go": groupReadsAtTheEdge,

	"pkg/grpcsrv/principal_extract.go": groupTransportMarker,

	"services/registry/internal/dataplane/handler.go": groupOwnSurface,
	"services/registry/internal/dataplane/proxy.go":   groupOwnSurface,

	"internal/repohygiene/bothidentityformsproducer.go": groupNamesTheKeyToSearchForIt,

	"gateway/internal/principalmeta/credential_strip.go": groupStripsIt,
}

// bothNameForms — разборщик, знающий ОБЕ законные формы записи имени.
var bothNameForms = regexp.MustCompile(`"[Aa]uthorization"`)

func TestKAN_STRIP_03_CredentialReaderCensus(t *testing.T) {
	root := repoRootForCensus(t)

	files, err := gitLsFiles(root, "*.go")
	if err != nil {
		t.Skipf("перепись беспредметна: дерево git не читается (%v)", err)
	}
	if len(files) == 0 {
		t.Fatal("перепись беспредметна: обход дал НОЛЬ файлов Go — сломан обход, а не дерево чисто")
	}

	var scanned int
	found := map[string]bool{}
	for _, rel := range files {
		if strings.HasSuffix(rel, "_test.go") {
			continue
		}
		scanned++
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		if bothNameForms.Match(b) {
			found[rel] = true
		}
	}
	if scanned == 0 {
		t.Fatal("перепись беспредметна: прочитано НОЛЬ не-тестовых файлов Go")
	}
	if len(found) == 0 {
		t.Fatalf("перепись беспредметна: осмотрено %d файлов, читателей удостоверения НОЛЬ — "+
			"так выглядит слепой разборщик, а не чистое дерево", scanned)
	}

	byGroup := map[credentialReaderGroup][]string{}
	var unadjudicated []string
	for rel := range found {
		g, ok := credentialReaderCensus[rel]
		if !ok {
			unadjudicated = append(unadjudicated, rel)
			continue
		}
		byGroup[g] = append(byGroup[g], rel)
	}
	var vanished []string
	for rel := range credentialReaderCensus {
		if !found[rel] {
			vanished = append(vanished, rel)
		}
	}

	sort.Strings(unadjudicated)
	sort.Strings(vanished)
	t.Logf("перепись: осмотрено не-тестовых файлов Go %d · читателей входящего удостоверения %d",
		scanned, len(found))
	for g := groupWritesOutgoing; g <= groupStripsIt; g++ {
		list := byGroup[g]
		sort.Strings(list)
		t.Logf("  %-36s %d  %s", g.String(), len(list), strings.Join(list, " "))
	}

	if len(unadjudicated) > 0 {
		t.Errorf("читатель входящего удостоверения появился и НЕ адъюдицирован: %v\n"+
			"радиус снятия берётся по свойству, а не по каталогу, где правку заметили: "+
			"отнесите файл к группе в credentialReaderCensus", unadjudicated)
	}
	if len(vanished) > 0 {
		t.Errorf("запись переписи потеряла предмет — файла в дереве больше нет: %v", vanished)
	}

	// Читатель ЗА краем: их обязано быть не больше одного, и это несущее —
	// снятие на крае безопасно именно потому, что за ним удостоверение читает
	// только механизм этой приёмки. Появится второй — снятие обязано быть
	// пересмотрено, а не исполнено молча.
	//
	// НИЖНЯЯ ГРАНИЦА СНЯТА, и снята она с названной причиной. Единственным
	// читателем за краем была служба доступа, вынесенная отдельным
	// репозиторием (задача #1111): в РАНТАЙМЕ она читает удостоверение
	// по-прежнему — умбрелла поднимает её из опубликованного образа, — но в
	// ЭТОМ дереве её исходников нет, и измерить эту полосу здесь нечем.
	//
	// Требовать «ровно один» значило бы требовать координату, которой в дереве
	// не существует: проба краснела бы на верно исполненном разрезе. Требовать
	// «ни одного» — тоже неверно: читатель есть, он просто судится своим
	// деревом. Поэтому здесь остаётся ВЕРХНЯЯ граница (второй читатель — по-
	// прежнему находка), а нижняя названа остатком вслух, а не проглочена нулём.
	if got := len(byGroup[groupBehindTheEdge]); got > 1 {
		t.Errorf("за краем удостоверение читает %d потребител(я/ей), допустим не больше одного: %v\n"+
			"снятие на крае обосновано единственностью этого читателя — пересмотрите его",
			got, byGroup[groupBehindTheEdge])
	}
	if len(byGroup[groupBehindTheEdge]) == 0 {
		t.Log("за краем читателей удостоверения в ЭТОМ дереве нет: единственный " +
			"(служба доступа) вынесен отдельным репозиторием и судится там. Полоса " +
			"здесь НЕ ИЗМЕРЯЕТСЯ — молчание по ней не означает «читателей нет»")
	}
}

// repoRootForCensus поднимается до корня монорепо.
func repoRootForCensus(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("рабочий каталог: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "gateway")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("корень монорепо не найден — перепись беспредметна")
		}
		dir = parent
	}
}

func gitLsFiles(root, pattern string) ([]string, error) {
	// Через помощник дерева, а не напрямую: `cmd.Dir` не выбирает репозиторий,
	// когда в окружении есть GIT_DIR — переменная сильнее рабочего каталога.
	out, err := gitenv.Command(root, "ls-files", pattern).Output()
	if err != nil {
		return nil, err
	}
	var files []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			files = append(files, l)
		}
	}
	return files, nil
}

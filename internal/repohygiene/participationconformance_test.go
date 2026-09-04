// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// participationconformance_test.go — участник контура прав доказывает участие
// ПОДЪЁМОМ, а не только объявлением.
//
// # Предмет
//
// Гейт принятия носителя (`servicehostadoption_test.go`) утверждает, что сервис
// не собирает сервер сам. Это утверждение о ДЕРЕВЕ, и оно ничего не говорит о
// том, поднимется ли сервис носителем: половина отказов старта считается только
// там, где известен СЛУЖИМЫЙ НАБОР — метод без строки каталога, домен без единой
// записи карты, служимый стрим при неприменимой оси его бюджета, сверка
// объявленных осей с каталогом.
//
// Разрыв между «провязан» и «поднимается» не гипотеза: у storage дескриптор нёс
// ЛОЖНОЕ заявление о скрытии существования, и оно проходило, потому что судья
// читал не тот предикат, а проверить его было негде; у nlb первый круг перевода
// потерял отсечку шторма отказов — величина, стоявшая в старом корне литералом,
// просто исчезла, и конструктор этого не увидел.
//
// # Что требует гейт
//
// У каждого участника — проба, поднимающая его ЧЕРЕЗ НОСИТЕЛЬ. Перечень
// участников ВЫВОДИТСЯ из дерева (кто зовёт `servicehost.Serve` в своём
// композиционном корне), а не выписывается: выписанный список молчит ровно про
// того, кого в него забыли внести, — то есть про седьмой сервис в день его
// появления.
//
// Имя файла пробы гейт не требует: он ищет вызов `servicehost.Serve` в ТЕСТОВОМ
// файле корня. Требование к имени сделало бы гейт проверкой соглашения об
// именовании, а не наличия утверждения.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// carrierServeCall — вызов, которым сервис поднимается носителем.
const (
	carrierPkg  = "github.com/PRO-Robotech/kacho/pkg/servicehost"
	carrierFunc = "Serve"
)

// participationSite — что найдено у одного сервиса.
type participationSite struct {
	prodCall bool // корень зовёт носитель в прод-коде → сервис участник
	testCall bool // тестовый файл корня зовёт носитель → участие доказано подъёмом
	where    string
}

// TestEveryCarrierParticipantIsRaisedByAProbe — у каждого участника есть проба
// подъёма.
//
// Что делать, если гейт сработал, — два исхода, третьего нет:
//
//  1. сервис участник, пробы нет → написать пробу подъёма (образец —
//     `services/storage/cmd/storage/carrier_start_test.go`): дескриптор через
//     свой `describe`, отменённый контекст, регистраторы корня, чтение переписи
//     носителя из журнала;
//  2. вызов носителя в прод-коде — не подъём сервиса (совпадение формы) →
//     уточнить распознавание, а не заводить исключение: перечня исключений у
//     этого гейта нет намеренно.
func TestEveryCarrierParticipantIsRaisedByAProbe(t *testing.T) {
	participants, proven, missing, files, err := auditCarrierParticipation(repoRoot(t))
	if err != nil {
		t.Fatalf("обход композиционных корней: %v", err)
	}

	t.Logf("перепись: файлов композиционных корней прочитано %d, участников контура %d "+
		"(выведено из дерева, не выписано), участие доказано подъёмом у %d",
		files, participants, proven)

	if files == 0 {
		t.Fatal("не прочитано ни одного файла композиционных корней — «пробы на месте» здесь " +
			"означало бы «ничего не читал», а не исправное дерево")
	}
	if participants == 0 {
		t.Fatal("участников контура ноль — либо носитель снят, либо распознавание вызова " +
			"сломано; «у всех есть проба» на пустом множестве истинно и потому бессмысленно")
	}

	for _, m := range missing {
		t.Errorf("участник %s поднимается носителем в проде, но НИ ОДНА его проба носитель не "+
			"зовёт.\nЗначит половина отказов старта у него не проверяется ничем: конструктор "+
			"дескриптора судит объявленное, а служимый набор он не видит. Образец пробы — "+
			"services/storage/cmd/storage/carrier_start_test.go", m)
	}
}

// auditCarrierParticipation — исход обхода: сколько участников, у скольких есть
// проба подъёма и кого недостаёт.
//
// Принимает КОРЕНЬ и возвращает ошибку вместо того чтобы ронять прогон изнутри:
// иначе гейт нельзя прогнать на синтетическом дереве, то есть нельзя доказать
// инъекцией — ни что он краснеет на участнике без пробы, ни что он молчит на
// участнике с пробой.
func auditCarrierParticipation(root string) (participants, proven int, missing []string, files int, err error) {
	sites, files, err := scanCarrierParticipation(root)
	if err != nil {
		return 0, 0, nil, 0, err
	}
	for svc, site := range sites {
		if !site.prodCall {
			continue
		}
		participants++
		if site.testCall {
			proven++
			continue
		}
		missing = append(missing, fmt.Sprintf("%s (корень: %s)", svc, site.where))
	}
	sort.Strings(missing)
	return participants, proven, missing, files, nil
}

// scanCarrierParticipation — обход композиционных корней сервисов.
//
// Состав берётся у индекса git: обход диска прочитал бы игнорируемое, и вердикт
// стал бы свойством рабочего каталога, а не коммита.
func scanCarrierParticipation(root string) (map[string]*participationSite, int, error) {
	dir := filepath.Join(root, "services")
	tracked, err := treecorpus.Under(dir)
	if err != nil {
		return nil, 0, fmt.Errorf("состав дерева под %s не читается: %w", dir, err)
	}

	out := map[string]*participationSite{}
	files := 0
	fset := token.NewFileSet()
	for _, abs := range tracked {
		if !strings.HasSuffix(abs, ".go") {
			continue
		}
		rel, rerr := filepath.Rel(root, abs)
		if rerr != nil {
			return nil, 0, fmt.Errorf("относительный путь для %s: %w", abs, rerr)
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) < 4 || parts[0] != "services" || parts[2] != "cmd" {
			continue
		}
		svc := parts[1]
		files++

		f, perr := parser.ParseFile(fset, abs, nil, parser.SkipObjectResolution)
		if perr != nil {
			return nil, 0, fmt.Errorf("разбор %s: %w", abs, perr)
		}
		if !callsCarrierServe(f) {
			continue
		}
		site, ok := out[svc]
		if !ok {
			site = &participationSite{}
			out[svc] = site
		}
		if strings.HasSuffix(abs, "_test.go") {
			site.testCall = true
			continue
		}
		site.prodCall = true
		if site.where == "" {
			site.where = filepath.ToSlash(rel)
		}
	}
	return out, files, nil
}

// callsCarrierServe — зовёт ли файл `servicehost.Serve`.
//
// Локальное имя пакета берётся из ОБЪЯВЛЕНИЯ импорта этого файла: псевдоним не
// должен становиться слепым пятном. Строковые литералы и комментарии сюда не
// попадают by construction — разбор идёт по синтаксическому дереву.
func callsCarrierServe(f *ast.File) bool {
	local := ""
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != carrierPkg {
			continue
		}
		local = filepath.Base(path)
		if imp.Name != nil {
			local = imp.Name.Name
		}
	}
	if local == "" {
		return false
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != carrierFunc {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == local {
			found = true
		}
		return true
	})
	return found
}

// ─────────────────────────────────────────────────────────────────────────────
// ИНЪЕКЦИЯ: обе стороны гоняют ТУ ЖЕ функцию, что и гейт по дереву
//
// Дефект и законный случай отличаются РОВНО одним файлом — тестовым, зовущим
// носитель. Без стороны «молчит» гейт был бы неотличим от проверки «сервис зовёт
// носитель» и краснел бы на каждом участнике сразу.

// synthCarrierTree — синтетический композиционный корень, видимый индексу.
func synthCarrierTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	synthTrack(t, root)
	return root
}

const carrierProdSrc = `package main

import "github.com/PRO-Robotech/kacho/pkg/servicehost"

func main() { _ = servicehost.Serve(nil, nil) }
`

const carrierProbeSrc = `package main

import (
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/servicehost"
)

func TestCarrierStarts(t *testing.T) { _ = servicehost.Serve(nil, nil) }
`

// Сторона дефекта: участник есть, пробы подъёма нет — гейт называет участника.
func TestCarrierParticipationGateRedOnAParticipantWithoutAProbe(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/cmd/x/main.go": carrierProdSrc,
	})
	participants, proven, missing, files, err := auditCarrierParticipation(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if files == 0 {
		t.Fatal("синтетическое дерево не прочитано")
	}
	if participants != 1 || proven != 0 {
		t.Fatalf("участников %d, доказано подъёмом %d — ожидалось 1 и 0", participants, proven)
	}
	if len(missing) != 1 || !strings.Contains(missing[0], "x") {
		t.Fatalf("участник без пробы не назван: %v", missing)
	}
}

// Законный близнец той же формы: тот же участник, та же сборка — плюс проба,
// зовущая носитель.
func TestCarrierParticipationGateSilentWhenTheProbeRaisesIt(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/cmd/x/main.go":               carrierProdSrc,
		"services/x/cmd/x/carrier_start_test.go": carrierProbeSrc,
	})
	participants, proven, missing, files, err := auditCarrierParticipation(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if files == 0 {
		t.Fatal("синтетическое дерево не прочитано")
	}
	if participants != 1 || proven != 1 {
		t.Fatalf("участников %d, доказано подъёмом %d — ожидалось 1 и 1", participants, proven)
	}
	if len(missing) != 0 {
		t.Fatalf("гейт нашёл дефект в законном дереве: %v", missing)
	}
}

// Отрицательный контроль распознавания: файл, зовущий ДРУГУЮ функцию того же
// пакета, участником не делает.
//
// Без него гейт ловил бы «корень тронул носитель», а не «корень им поднимается»,
// и любой сервис, читающий из пакета константу, становился бы участником.
func TestCarrierParticipationGateIgnoresANonServeCall(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/cmd/x/main.go": `package main

import "github.com/PRO-Robotech/kacho/pkg/servicehost"

func main() { _ = servicehost.DefaultShutdownGrace }
`,
	})
	participants, _, _, files, err := auditCarrierParticipation(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if files == 0 {
		t.Fatal("синтетическое дерево не прочитано")
	}
	if participants != 0 {
		t.Fatalf("участников %d — обращение к пакету засчитано подъёмом, и тогда участником "+
			"станет всякий, кто прочтёт оттуда константу", participants)
	}
}

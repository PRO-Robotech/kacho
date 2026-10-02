// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// subscriptionownerdocs_test.go — гейт «владелец журнала говорит об этом В СВОЕЙ
// клиентской документации».
//
// # Предмет — утверждение, пережившее свой предмет
//
// Клиентская документация домена, ставшего владельцем журнала подписки,
// продолжала отрицать возможность: страница compute объявляла «публичного Watch в
// API нет и не планируется» ровно в том дереве, где compute уже был назначен
// первым владельцем. Клиент, читающий документацию, останавливался и отвечал
// «платформа этого не умеет». Ответ неверен, а цена его — архитектурное решение
// строить на опросе, которое переделывают месяцами.
//
// # Что именно требуется — и почему НЕ отсутствие отрицания
//
// Требуется ПОЛОЖИТЕЛЬНОЕ: клиентские страницы владельца называют адрес ручки.
// Соблазн проверять обратное — «нет фразы „такого механизма нет“» — велик, но
// такой предикат неисправим в обе стороны: лексикон над естественным языком уже
// проверялся этим корпусом и провалил контроль (он не отличает законного близнеца
// «подписки на ОПЕРАЦИИ нет» — утверждение верное и обязанное остаться — от
// отрицания самого потока). Проверка, краснеющая на верном тексте, будет снята
// первой.
//
// Положительное требование сомнений не допускает: адрес либо назван, либо нет.
// И оно закрывает наблюдавшийся исход целиком — страница, называющая адрес и тут
// же его отрицающая, противоречила бы себе в одном абзаце.
//
// # Как он истекает и как заводится
//
// От ФАКТА В ДЕРЕВЕ. Перестанет домен служить глагол — требование к его
// документации снимется само. Станет владельцем седьмой домен — гейт потребует от
// него того же, не будучи ни разу тронут.
//
// Обратной стороны («не-владелец не смеет упоминать адрес») здесь НЕТ намеренно:
// упоминание платформенной возможности у не-владельца законно и полезно — так
// страница vpc честно говорит, что поток есть, а журнала у vpc пока нет.

// subscriptionVerbRegistrar — имя функции, которой владелец регистрирует глагол
// подписки на своём внутреннем слушателе.
//
// Судится ВЫЗОВ разобранного дерева, а не подстрока: имя стоит и в комментариях
// (в том числе в этом), и сверка по тексту краснела бы на собственном объяснении.
const subscriptionVerbRegistrar = "RegisterInternalSubscriptionServiceServer"

// subscriptionHandlePath — адрес единственной проекции потока.
//
// Подлинная величина — константа `subscriptionstream.Path`, но она лежит под
// `gateway/internal/`, а этот пакет вне поддерева `gateway/` и импортировать её
// не может: свойство языка, а не небрежность. Согласие ЭТОЙ строки с подлинной
// держит проба края (`gateway/deploy/subscription_stream_serving_declared_test.go`
// сверяет её же с объявлениями посредников), а здесь она сверяется отдельным
// утверждением ниже — по объявлению в исходнике ручки.
const subscriptionHandlePath = "/subscription/v1/events"

// subscriptionPathDeclRel — исходник, объявляющий адрес ручки.
const subscriptionPathDeclRel = "gateway/internal/subscriptionstream/request.go"

// subscriptionDocsLister — как гейт узнаёт состав дерева.
//
// Отдельным типом ради ОДНОГО: доказательство способности гейта упасть обязано
// прогонять ТУ ЖЕ функцию суждения, а не её копию. Состав в бою берётся у индекса
// git (обход диска читал бы игнорируемые каталоги, и вердикт стал бы свойством
// рабочего каталога, а не коммита); инъекция подаёт синтетический состав, не
// заводя репозитория.
type subscriptionDocsLister func(dir string, suffixes ...string) ([]string, error)

// subscriptionInternalOnlyOwners — владельцы журнала, чей вид служится ТОЛЬКО на
// внутреннем слушателе и краем не маршрутизируется: клиентской возможности
// нет, и требование назвать адрес ручки в клиентской документации было бы
// требованием обещать то, чего клиент не получит.
//
// Запись самоистекает от ФАКТА В ДЕРЕВЕ (subscriptionInternalOnlyExpired):
// домен перестал служить глагол — записи нечего исключать; край узнал
// внутренний адрес домена (ключ домена в `BackendAddrs` конфигурации края) —
// вид стал досягаем снаружи, и требование к документации возвращается.
var subscriptionInternalOnlyOwners = map[string]string{
	"notify": "вид notification_feed только внутренний (kacho#2915, З32, Д74): ленту и подписку " +
		"служит корень пробы-источника cmd/notify-probe на внутреннем слушателе, читатель — " +
		"шлюз notify; край его не маршрутизирует (ban #6). Предикат снятия — ключ \"notify\" в " +
		"gateway/internal/config BackendAddrs",
}

// subscriptionEdgeConfigRel — исходник конфигурации края, чья карта адресов
// решает, каких владельцев край может назвать (`DomainsWithInternalBackend`).
const subscriptionEdgeConfigRel = "gateway/internal/config/config.go"

// subscriptionEdgeDomains — строковые ключи карты, которую возвращает
// `BackendAddrs`: домены, чей адрес край знает. Разбором, а не подстрокой.
func subscriptionEdgeDomains(root string) (map[string]bool, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(root, subscriptionEdgeConfigRel), nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "BackendAddrs" || fn.Body == nil {
			return true
		}
		ast.Inspect(fn.Body, func(m ast.Node) bool {
			kv, ok := m.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			if lit, ok := kv.Key.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				out[strings.Trim(lit.Value, "\"`")] = true
			}
			return true
		})
		return false
	})
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: ключей BackendAddrs не найдено — предикат снятия внутренних "+
			"владельцев читать не с чего", subscriptionEdgeConfigRel)
	}
	return out, nil
}

// subscriptionInternalOnlyExpired — записи ведомости, пережившие предмет.
func subscriptionInternalOnlyExpired(owners []string, edge map[string]bool) []string {
	isOwner := map[string]bool{}
	for _, o := range owners {
		isOwner[o] = true
	}
	var out []string
	for name := range subscriptionInternalOnlyOwners {
		switch {
		case !isOwner[name]:
			out = append(out, fmt.Sprintf("запись внутреннего владельца %q пережила предмет: "+
				"домен глагол подписки не служит — снимите запись", name))
		case edge[name]:
			out = append(out, fmt.Sprintf("запись внутреннего владельца %q пережила предмет: край "+
				"знает адрес домена (%s BackendAddrs), вид досягаем снаружи — снимите запись, "+
				"и клиентская документация обязана назвать адрес ручки", name, subscriptionEdgeConfigRel))
		}
	}
	sort.Strings(out)
	return out
}

// ownerReport — что гейт установил про одного владельца журнала.
type ownerReport struct {
	name       string
	pagesRead  int
	pagesNamed []string
	docsErr    error
}

// subscriptionOwners обходит каталог сервисов и отдаёт тех, кто РЕГИСТРИРУЕТ
// глагол подписки в не-тестовом коде.
//
// Судится ВЫЗОВ разобранного дерева: имя регистратора стоит и в комментариях,
// поэтому сверка по подстроке краснела бы на собственном объяснении.
func subscriptionOwners(root string, list subscriptionDocsLister) (owners []string, filesRead int, unparsed []string, err error) {
	files, err := list(filepath.Join(root, "services"), ".go")
	if err != nil {
		return nil, 0, nil, err
	}
	found := map[string]bool{}
	fset := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		filesRead++
		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			// Неразбираемый исходник — находка самого гейта: он судит по узлам,
			// и файл, который он не разобрал, он не осмотрел.
			unparsed = append(unparsed, path)
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel == nil || sel.Sel.Name != subscriptionVerbRegistrar {
				return true
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return true
			}
			parts := strings.Split(filepath.ToSlash(rel), "/")
			if len(parts) >= 2 && parts[0] == "services" {
				found[parts[1]] = true
			}
			return true
		})
	}
	for name := range found {
		owners = append(owners, name)
	}
	sort.Strings(owners)
	return owners, filesRead, unparsed, nil
}

// ownerDocReports читает клиентскую документацию каждого владельца.
func ownerDocReports(root string, owners []string, list subscriptionDocsLister) []ownerReport {
	reports := make([]ownerReport, 0, len(owners))
	for _, owner := range owners {
		contentDir := filepath.Join(root, "services", owner, "docs", "content")
		rep := ownerReport{name: owner}
		pages, err := list(contentDir, ".mdx", ".md")
		if err != nil {
			rep.docsErr = err
			reports = append(reports, rep)
			continue
		}
		for _, page := range pages {
			body, readErr := os.ReadFile(page) // #nosec G304 -- путь из состава дерева
			if readErr != nil {
				rep.docsErr = readErr
				continue
			}
			rep.pagesRead++
			if strings.Contains(string(body), subscriptionHandlePath) {
				rel, _ := filepath.Rel(root, page)
				rep.pagesNamed = append(rep.pagesNamed, filepath.ToSlash(rel))
			}
		}
		reports = append(reports, rep)
	}
	return reports
}

// TestSubscriptionOwnersSaySoInTheirClientDocs — владелец журнала обязан назвать
// адрес ручки в своей клиентской документации.
func TestSubscriptionOwnersSaySoInTheirClientDocs(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	list := subscriptionDocsLister(treecorpus.UnderWithSuffix)

	owners, filesRead, unparsed, err := subscriptionOwners(root, list)
	if err != nil {
		t.Fatalf("состав исходников сервисов у корпуса дерева: %v", err)
	}
	for _, path := range unparsed {
		t.Errorf("исходник %s не разбирается — гейт судит по узлам дерева, и неосмотренный "+
			"файл его молчания не оправдывает", path)
	}
	edge, err := subscriptionEdgeDomains(root)
	if err != nil {
		t.Fatalf("домены края: %v", err)
	}
	for _, finding := range subscriptionInternalOnlyExpired(owners, edge) {
		t.Error(finding)
	}
	clientOwners := make([]string, 0, len(owners))
	for _, o := range owners {
		if why, internal := subscriptionInternalOnlyOwners[o]; internal {
			t.Logf("  · %s: вид только внутренний — клиентская документация не судится (%s)", o, why)
			continue
		}
		clientOwners = append(clientOwners, o)
	}
	reports := ownerDocReports(root, clientOwners, list)

	named := 0
	for _, rep := range reports {
		if len(rep.pagesNamed) > 0 {
			named++
		}
	}
	t.Logf("перепись: исходников сервисов осмотрено %d · владельцев журнала %d %v · "+
		"из них называют адрес ручки в клиентской документации %d",
		filesRead, len(owners), owners, named)
	for _, rep := range reports {
		t.Logf("  · %s: клиентских страниц %d · называют адрес %d %v",
			rep.name, rep.pagesRead, len(rep.pagesNamed), rep.pagesNamed)
	}

	if filesRead == 0 {
		t.Fatal("не осмотрено ни одного исходника сервисов — прочитано ноль, и зелёное " +
			"здесь неотличимо от пустого обхода")
	}
	if len(owners) == 0 {
		t.Fatal("не найдено ни одного владельца журнала — предмета у гейта нет. Если глагол " +
			"действительно перестали служить, снимите гейт вместе с ним; если служат, " +
			"а он не нашёл, — сломан обход")
	}

	for _, finding := range ownerDocFindings(reports) {
		t.Error(finding)
	}
}

// ownerDocFindings — суждение о владельцах. Вынесено отдельно, чтобы инъекция
// прогоняла ЕГО, а не свою копию.
func ownerDocFindings(reports []ownerReport) []string {
	out := make([]string, 0, 2)
	for _, rep := range reports {
		if rep.docsErr != nil {
			out = append(out, fmt.Sprintf(
				"владелец журнала %q: клиентская документация не читается (%v) — гейту нечего "+
					"осматривать, и его молчание не было бы утверждением", rep.name, rep.docsErr))
			continue
		}
		if rep.pagesRead == 0 {
			out = append(out, fmt.Sprintf(
				"владелец журнала %q: клиентских страниц ноль — требование к документации "+
					"невыполнимо by construction, и это находка о дереве, а не о тексте", rep.name))
			continue
		}
		if len(rep.pagesNamed) == 0 {
			out = append(out, fmt.Sprintf(
				"владелец журнала %q служит глагол подписки, а его клиентская документация "+
					"(%d страниц) НИ РАЗУ не называет адрес ручки %q. Клиент, читающий только её, "+
					"заключит, что платформа так не умеет, — и построит на опросе то, что "+
					"переделывают месяцами. Ровно этот исход наблюдался на compute (#1389).",
				rep.name, rep.pagesRead, subscriptionHandlePath))
		}
	}
	return out
}

// TestSubscriptionHandlePathHereMatchesTheEdgeDeclaration — величина, повторённая
// в этом гейте, сверяется с ПОДЛИННОЙ.
//
// Повторить её пришлось: подлинная константа лежит под `gateway/internal/`, куда
// этому пакету хода нет. Повторённая величина расходится молча — поэтому здесь
// стоит утверждение, а не доверие.
func TestSubscriptionHandlePathHereMatchesTheEdgeDeclaration(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	path := filepath.Join(root, subscriptionPathDeclRel)
	raw, err := os.ReadFile(path) // #nosec G304 -- обход собственного дерева
	if err != nil {
		t.Fatalf("исходник ручки %s не читается (%v) — сверять величину не с чем",
			subscriptionPathDeclRel, err)
	}
	// Ищется ОБЪЯВЛЕНИЕ константы, а не любое вхождение строки: адрес стоит в
	// исходнике и в комментариях, а предмет сверки — то, что ручка объявляет.
	fset := token.NewFileSet()
	file, parseErr := parser.ParseFile(fset, path, raw, parser.SkipObjectResolution)
	if parseErr != nil {
		t.Fatalf("исходник ручки не разбирается: %v", parseErr)
	}
	declared := ""
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || spec.Names[0].Name != "Path" || len(spec.Values) != 1 {
			return true
		}
		lit, ok := spec.Values[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		declared = strings.Trim(lit.Value, `"`)
		return false
	})

	t.Logf("перепись: объявление ручки %q · величина этого гейта %q",
		declared, subscriptionHandlePath)

	if declared == "" {
		t.Fatal("в исходнике ручки не найдено объявление константы `Path` — прочитано ноль, " +
			"и согласие величин здесь не утверждается ничем")
	}
	if declared != subscriptionHandlePath {
		t.Fatalf("адрес ручки объявлен как %q, а гейт сверяет документацию с %q: величина "+
			"повторена и разошлась — сверка идёт с адресом, которого нет",
			declared, subscriptionHandlePath)
	}
}

// TestSubscriptionInternalOnlyOwnersExpireOnTheirFacts — запись внутреннего
// владельца истекает от факта в дереве в обе стороны; близнец — оба факта на
// месте, находок нет.
func TestSubscriptionInternalOnlyOwnersExpireOnTheirFacts(t *testing.T) {
	t.Parallel()
	edge := map[string]bool{"vpc": true}
	if got := subscriptionInternalOnlyExpired([]string{"notify", "vpc"}, edge); len(got) != 0 {
		t.Fatalf("близнец: владелец служит глагол, край его не знает — находок быть не должно: %v", got)
	}
	if got := subscriptionInternalOnlyExpired([]string{"vpc"}, edge); len(got) != 1 ||
		!strings.Contains(got[0], "не служит") {
		t.Fatalf("домен перестал служить глагол — запись обязана истечь: %v", got)
	}
	if got := subscriptionInternalOnlyExpired([]string{"notify"}, map[string]bool{"notify": true}); len(got) != 1 ||
		!strings.Contains(got[0], "BackendAddrs") {
		t.Fatalf("край узнал адрес домена — запись обязана истечь: %v", got)
	}
}

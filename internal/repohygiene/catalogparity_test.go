// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// catalogparity_test.go — гейт против расхождения между картой прав сервиса и
// каталогом шлюза.
//
// На один вызов принимается ДВА независимых решения о доступе: шлюз спрашивает
// модель по записи каталога, а сервис-владелец переспрашивает по своей карте.
// Пока обе стороны называют одно и то же отношение на одном и том же объекте,
// это защита в глубину. Как только они называют разное, фактическим требованием
// становится ПЕРЕСЕЧЕНИЕ, и оно не записано нигде: каталог — документ, который
// читает оператор и по которому выдаются права, а карта сервиса — то, что
// реально исполняется.
//
// Наблюдаемое следствие измерено, а не предположено (2026-07-29): по семи
// списочным RPC каталог называл глагольное отношение на проекте, а карта —
// читательский ярус, и это в модели РАЗНЫЕ множества (ни одно не выводится из
// другого). Субъект, которому выдали ровно объявленное каталогом, получал отказ
// на методном гейте — при живом пообъектном гранте, по которому чтение
// одиночного объекта проходило. Дефект пережил и обзоры, и прогоны, потому что
// администратор кластера удовлетворяет ОБА отношения сразу: расхождение видно
// только обычному тенанту.
//
// Пообъектные тесты чётности уже стоят в каждом сервисе (catalog_parity_test.go
// рядом с его permission_map.go). Здесь — то, чего они не могут сказать про
// себя:
//
//   - сервис может приехать БЕЗ такого теста, и его карта окажется вне гейта
//     целиком (свой тест не умеет заметить собственное отсутствие);
//   - каждый список исключений локален, поэтому репо-широкой картины «сколько
//     сейчас расхождений и каких» не существует ни в одном месте — а именно она
//     и нужна, чтобы исключение не разрослось молча.
package repohygiene

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// catalogEmbedPath / iamCatalogMirrorPath — две вшитые копии каталога. Первая —
// та, которую исполняет шлюз (pkg/authz/catalogparity.CatalogPath); вторая —
// зеркало, вшитое в iam. Гейт ниже говорит «каталог» в единственном числе, и это
// осмысленно ровно пока копии совпадают побайтово.
const (
	catalogEmbedPath     = "gateway/internal/middleware/embed/permission_catalog.json"
	iamCatalogMirrorPath = "services/iam/internal/apps/kacho/seed/embedded/permission_catalog.json"
)

// divergenceVarName — имя переменной, в которой каждый пообъектный тест чётности
// перечисляет свои исключения. Реестр ниже собирается разбором ИМЕННО этой
// переменной, поэтому её имя — часть предпосылки гейта (см.
// TestCatalogParityRosterPremiseHolds).
const divergenceVarName = "knownCatalogDivergences"

// knownDivergenceRoster — репо-широкий реестр расхождений «каталог ↔ карта
// сервиса», которые сегодня существуют осознанно.
//
// Реестр — ПИН, а не разрешение: он обязан совпадать с объединением локальных
// списков по всему дереву. Появилось расхождение где угодно — падает здесь;
// исчезло — тоже падает, чтобы запись не пережила свой предмет.
//
// Что осталось и почему (обе записи — ОДИН класс, отличный от списочного):
// внутренний административный RPC, где каталог якорится на кластерном ярусе
// (`system_viewer` на singleton-объекте кластера), а сервис — на глагольном
// отношении САМОГО ресурса. Это не «занижено на одно отношение», а два разных
// вопроса о двух разных объектах, и свести их — продуктовое решение про то, кто
// вправе читать состояние конкретного балансировщика/реестра: кластерный
// наблюдатель или держатель гранта на объект. Оно принимается в своём домене со
// своим обоснованием, а не заодно со списочным классом.
//
// Третья запись (registry TriggerGarbageCollection) — тот же домен, тот же
// разбор: объект один и тот же, но `admin` и `v_delete` в модели независимы
// (`v_delete` дополнительно выводится из `owner`), поэтому это снова
// пересечение, а не строгость одной стороны.
var knownDivergenceRoster = []string{
	`/kacho.cloud.loadbalancer.v1.InternalLoadBalancerAnnounceService/GetAnnounceState: catalog anchors scope on object type "cluster", service map anchors on "nlb_network_load_balancer"`,
	`/kacho.cloud.loadbalancer.v1.InternalLoadBalancerAnnounceService/GetAnnounceState: catalog requires relation "system_viewer", service map requires "v_get"`,
	`/kacho.cloud.registry.v1.InternalRegistryService/GetRegistryStats: catalog anchors scope on object type "cluster", service map anchors on "registry_registry"`,
	`/kacho.cloud.registry.v1.InternalRegistryService/GetRegistryStats: catalog requires relation "system_viewer", service map requires "v_get"`,
	`/kacho.cloud.registry.v1.InternalRegistryService/TriggerGarbageCollection: catalog requires relation "admin", service map requires "v_delete"`,
}

// unbackedScopeFilteredRows — строки каталога, которые ОБЕЩАЮТ, что сужение
// делает сервис-владелец, тогда как ни одна карта прав в дереве этого не
// подтверждает.
//
// Почему это отдельный гейт, а не следствие сверки выше. `catalogparity.Compare`
// обходит методы КАРТЫ СЕРВИСА и сверяет их с каталогом. Метод, которого в карте
// нет вовсе — включая случай «у сервиса карты нет ни одной», — в обход не
// попадает, поэтому самое сильное расхождение (обещали сужение, не заявил никто)
// проходит молча. Полоса `scope_filtered` — это ПОЛОЖИТЕЛЬНОЕ обещание, на
// основании которого край перестаёт спрашивать модель; необеспеченное, оно
// оставляет вызов с одной лишь аутентификацией.
//
// Что осталось и почему: девять RPC iam про сам граф выдач. У iam карты прав нет
// (он и есть сервис авторизации — своё решение он принимает в хендлерах, а не
// картой), поэтому подтвердить обещание в этом дереве нечем. Запись здесь — не
// разрешение, а СЧЁТ: пока она стоит, «край не спрашивает» держится на разборе
// хендлеров iam, а не на проверяемом объявлении.
var unbackedScopeFilteredRows = []string{
	"kacho.cloud.iam.v1.AccessBindingService/ExpandAccess",
	"kacho.cloud.iam.v1.AccessBindingService/List",
	"kacho.cloud.iam.v1.AccessBindingService/ListByRole",
	"kacho.cloud.iam.v1.AccessBindingService/ListBySubject",
	"kacho.cloud.iam.v1.AccessBindingService/ListSubjectPrivileges",
	"kacho.cloud.iam.v1.AuthorizeService/Check",
	"kacho.cloud.iam.v1.AuthorizeService/ExpandRelations",
	"kacho.cloud.iam.v1.AuthorizeService/ListObjects",
	"kacho.cloud.iam.v1.AuthorizeService/ListSubjects",
}

// catalogRow — минимальная форма строки каталога, нужная этому файлу.
type catalogRow struct {
	FQN           string `json:"fqn"`
	ScopeFiltered bool   `json:"scope_filtered"`
}

// TestScopeFilteredCatalogRowsAreBackedByAServiceMap — обещание полосы
// `scope_filtered` обязано быть подтверждено картой прав владельца.
//
// Что делать, если гейт сработал, — ровно три исхода, четвёртого нет:
//
//  1. сервис действительно сужает страницу сам -> объявить это в его карте
//     (`ScopeFiltered: true`), чтобы обещание каталога было проверяемым;
//  2. сервис НЕ сужает -> строка каталога лжёт: край не спрашивает модель, а
//     сервис не спрашивает её за него. Убрать `scope_filtered` из аннотации proto
//     и перегенерировать каталог;
//  3. подтвердить нечем прямо сейчас (у владельца нет карты) -> внести сюда с
//     разбором, чем именно держится сужение.
//
// «Оставить необеспеченным молча» исходом не является: край перестаёт спрашивать
// модель на основании обещания, которого никто не давал.
func TestScopeFilteredCatalogRowsAreBackedByAServiceMap(t *testing.T) {
	root := repoRoot(t)

	raw, err := os.ReadFile(filepath.Join(root, catalogEmbedPath))
	if err != nil {
		t.Fatalf("не прочитан вшитый каталог %s: %v", catalogEmbedPath, err)
	}
	var rows []catalogRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("разбор каталога %s: %v", catalogEmbedPath, err)
	}

	declared := map[string]bool{}
	for _, r := range rows {
		if r.ScopeFiltered {
			declared[r.FQN] = true
		}
	}
	if len(declared) == 0 {
		t.Fatalf("в каталоге нет ни одной строки `scope_filtered` — гейт беспредметен. Либо "+
			"полоса снята (тогда удали и его, и %s), либо изменился ключ поля.", "unbackedScopeFilteredRows")
	}

	backed := map[string]bool{}
	fset := token.NewFileSet()
	for _, p := range discoverAuthzMapPackages(t, root) {
		f, perr := parser.ParseFile(fset, filepath.Join(root, p.MapFile), nil, 0)
		if perr != nil {
			t.Fatalf("разбор карты прав %s: %v", p.MapFile, perr)
		}
		for _, m := range scopeFilteredMethods(f) {
			backed[m] = true
		}
	}
	if len(backed) == 0 {
		t.Fatalf("ни одна карта прав не объявляет `ScopeFiltered: true` — либо разбор перестал "+
			"видеть эту форму, либо полосу больше никто не заявляет. В обоих случаях гейт ниже "+
			"объявил бы НЕОБЕСПЕЧЕННЫМИ все %d строк каталога, что было бы неверно.", len(declared))
	}

	var actual []string
	for fqn := range declared {
		if !backed[fqn] {
			actual = append(actual, fqn)
		}
	}
	sort.Strings(actual)

	pinned := slices.Clone(unbackedScopeFilteredRows)
	sort.Strings(pinned)

	for _, fqn := range actual {
		if !slices.Contains(pinned, fqn) {
			t.Errorf("каталог обещает `scope_filtered`, но карта прав владельца этого не "+
				"подтверждает:\n  %s\n\n"+
				"Край на этом обещании ПЕРЕСТАЁТ спрашивать модель. Исходы: объявить сужение в "+
				"карте сервиса / снять `scope_filtered` из аннотации proto и перегенерировать "+
				"каталог / внести сюда с разбором.", fqn)
		}
	}
	for _, fqn := range pinned {
		if !slices.Contains(actual, fqn) {
			t.Errorf("запись пережила свой предмет — эта строка каталога больше не является "+
				"необеспеченной:\n  %s\n\n"+
				"Удали её из unbackedScopeFilteredRows, иначе список перестаёт быть счётом и "+
				"становится слепой зоной, которую унаследует следующий.", fqn)
		}
	}

	// Перепись: «ноль необеспеченных» обязано быть отличимо от «ноль прочитанных
	// строк каталога» и от «ноль найденных карт прав». Обе предпосылки выше уже
	// роняют гейт на пустоте, но их прохождение молчаливо — а молчание неотличимо
	// от того, что осмотра не было.
	t.Logf("перепись: строк каталога %d, из них scope_filtered %d; карт прав осмотрено %d, "+
		"объявляют сужение %d; необеспеченных %d (внесено с разбором %d)",
		len(rows), len(declared), len(discoverAuthzMapPackages(t, root)), len(backed),
		len(actual), len(pinned))
}

// scopeFilteredMethods — ключи карты прав, значение которых объявляет
// `ScopeFiltered: true`.
//
// Разбор идёт по дереву, а не регуляркой: та же строка встречается в прозе
// комментариев рядом с каждым таким объявлением (они объясняют, почему полоса
// выбрана), и текстовый поиск зачёл бы объяснение за объявление.
func scopeFilteredMethods(f *ast.File) []string {
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, ok := kv.Key.(*ast.BasicLit)
		if !ok || key.Kind != token.STRING {
			return true
		}
		lit, ok := kv.Value.(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, el := range lit.Elts {
			field, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			name, ok := field.Key.(*ast.Ident)
			if !ok || name.Name != "ScopeFiltered" {
				continue
			}
			if v, ok := field.Value.(*ast.Ident); ok && v.Name == "true" {
				if method, uerr := strconv.Unquote(key.Value); uerr == nil {
					out = append(out, strings.TrimPrefix(method, "/"))
				}
			}
		}
		return true
	})
	return out
}

// authzMapPackage — пакет, объявляющий карту прав сервиса, и его тест чётности.
type authzMapPackage struct {
	// Dir — rel-путь каталога пакета.
	Dir string
	// MapFile — rel-путь файла с `func PermissionMap() authz.RPCMap`.
	MapFile string
	// ParityFiles — rel-пути тестов в том же пакете, вызывающих
	// catalogparity.Compare.
	ParityFiles []string
	// Divergences — строки, перечисленные в divergenceVarName этого пакета.
	Divergences []string
	// DivergenceVarDecls — сколько раз divergenceVarName объявлена в пакете.
	// Ровно одна — предпосылка разбора реестра.
	DivergenceVarDecls int
	// DiffCalls — сколько раз divergenceVarName передана в `.Diff(` — то есть
	// действительно работает как список исключений, а не лежит рядом.
	DiffCalls int
}

// TestEveryServiceAuthzMapIsComparedAgainstTheCatalog — карта прав, которую
// никто не сверяет с каталогом, находится вне гейта целиком.
//
// Что делать, если гейт сработал, — ровно три исхода, четвёртого нет:
//
//  1. пакет действительно несёт карту прав сервиса -> положить рядом
//     catalog_parity_test.go по образцу любого существующего (загрузить каталог
//     через catalogparity.LoadCatalog, сравнить catalogparity.Compare, отдиффить
//     против списка knownCatalogDivergences);
//  2. функция называется PermissionMap, но картой прав НЕ является -> переименовать,
//     чтобы имя не обещало того, чего нет;
//  3. карта устарела и больше не подключена -> удалить её вместе с пакетом.
//
// «Оставить как есть» исходом не является: карта без сверки — это второе решение
// о доступе, которое ничему не подотчётно.
func TestEveryServiceAuthzMapIsComparedAgainstTheCatalog(t *testing.T) {
	root := repoRoot(t)
	pkgs := discoverAuthzMapPackages(t, root)

	var missing []string
	for _, p := range pkgs {
		if len(p.ParityFiles) == 0 {
			missing = append(missing, p.MapFile)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d карт(а) прав сервиса не сверяется с каталогом шлюза:\n  %s\n\n"+
			"На вызов принимается два решения о доступе — шлюза и сервиса. Несверенная "+
			"карта делает второе из них ничему не подотчётным: она может требовать не то "+
			"отношение и не на том объекте, и узнается это только пробой.\n"+
			"Исходы: добавить catalog_parity_test.go в тот же пакет / переименовать функцию, "+
			"если это не карта прав / удалить неподключённую карту.",
			len(missing), strings.Join(missing, "\n  "))
	}

	// Перепись: «ни одна карта не осталась несверенной» значит что-то только если
	// карт вообще нашлось. Ноль найденных дал бы ровно тот же зелёный вердикт.
	pf := 0
	for _, p := range pkgs {
		pf += len(p.ParityFiles)
	}
	t.Logf("перепись: карт прав найдено %d, сверяющих проб при них %d, несверенных %d",
		len(pkgs), pf, len(missing))
}

// TestCatalogDivergenceRosterIsPinned — репо-широкий реестр расхождений обязан
// совпадать с объединением локальных списков.
//
// Локальный список видит только свой сервис, поэтому по нему нельзя ответить на
// вопрос «сколько сейчас мест, где выданное по каталогу недостаточно». Пин здесь
// делает ответ одним, и любое движение — в любую сторону — становится видимым
// изменением ЭТОГО файла.
//
// Что делать, если гейт сработал, — ровно три исхода, четвёртого нет:
//
//  1. каталог называет настоящее требование, а карта сервиса отстала -> отзеркалить
//     карту по каталогу (каталог — авторитет, он генерируется из аннотаций proto);
//  2. требование исполняет сервис, а каталог его ЗАНИЖАЕТ -> починить аннотацию
//     proto и перегенерировать каталог, чтобы выданное по каталогу было достаточно
//     (`make -C gateway permission-catalog-apply` + синхронизация копии iam);
//  3. свести стороны прямо сейчас нельзя -> внести расхождение И сюда, И в
//     knownCatalogDivergences своего сервиса, с письменным разбором по модели:
//     какие tuple выполняют каждое из двух отношений и кто из-за этого получает
//     отказ.
//
// «Оставить разное молча» исходом не является: фактическое требование становится
// пересечением двух отношений, а пересечение не записано ни в одном документе,
// по которому выдают права.
func TestCatalogDivergenceRosterIsPinned(t *testing.T) {
	root := repoRoot(t)
	pkgs := discoverAuthzMapPackages(t, root)

	seen := map[string]string{} // расхождение -> файл, где оно перечислено
	var actual []string
	for _, p := range pkgs {
		for _, d := range p.Divergences {
			if _, dup := seen[d]; !dup {
				actual = append(actual, d)
			}
			seen[d] = p.Dir
		}
	}
	sort.Strings(actual)

	pinned := slices.Clone(knownDivergenceRoster)
	sort.Strings(pinned)

	for _, d := range actual {
		if !slices.Contains(pinned, d) {
			t.Errorf("расхождение каталога и карты сервиса не внесено в репо-широкий реестр "+
				"(%s, перечислено в %s):\n  %s\n\n"+
				"Исходы: отзеркалить карту по каталогу / починить аннотацию proto и "+
				"перегенерировать каталог / внести сюда с разбором по модели.",
				divergenceVarName, seen[d], d)
		}
	}
	for _, d := range pinned {
		if !slices.Contains(actual, d) {
			t.Errorf("запись реестра пережила свой предмет — такого расхождения в дереве "+
				"больше нет:\n  %s\n\n"+
				"Удали её из knownDivergenceRoster, иначе реестр перестаёт быть счётом "+
				"расхождений и становится списком, которому никто не верит.", d)
		}
	}

	// Перепись: пустой обход дал бы «расхождений нет» — вердикт, неотличимый от
	// зелёного по существу. Числа отделяют одно от другого.
	t.Logf("перепись: пакетов с картой прав %d, расхождений в дереве %d, в реестре %d",
		len(pkgs), len(actual), len(pinned))
}

// TestCatalogParityRosterPremiseHolds — гейт выше опирается на факты, которые
// могут перестать быть верными, и тогда он замолчит, продолжая выглядеть
// работающим.
//
// Предпосылок три, и каждая проверяется отдельно:
//
//  1. обход вообще что-то находит. Ноль карт прав — это не «расхождений нет», это
//     «гейт ничего не утверждает»;
//  2. реестр собирается разбором переменной с фиксированным именем. Пакет,
//     объявивший её дважды или не объявивший вовсе, разбирается неверно ТИХО;
//     а переменная, объявленная, но не переданная в `.Diff(`, вообще не работает
//     как список исключений — тест рядом с ней сравнивает против пустоты;
//  3. «каталог» — одна вещь. Две вшитые копии обязаны совпадать побайтово, иначе
//     непонятно, с какой из них шла сверка (ту же проверку делает CI-таргет
//     `make -C gateway permission-catalog-check`, но он требует buf — здесь она
//     доступна обычным `go test`).
func TestCatalogParityRosterPremiseHolds(t *testing.T) {
	root := repoRoot(t)
	pkgs := discoverAuthzMapPackages(t, root)

	if len(pkgs) == 0 {
		t.Fatalf("обход не нашёл ни одной карты прав сервиса (`func PermissionMap() authz.RPCMap`). "+
			"Гейты в этом файле в таком состоянии не утверждают НИЧЕГО. Либо изменилась сигнатура/"+
			"имя карты — поправь discoverAuthzMapPackages, либо карт действительно не осталось — "+
			"тогда удали и гейт, и реестр %s.", divergenceVarName)
	}

	for _, p := range pkgs {
		if len(p.ParityFiles) == 0 {
			continue // об этом ругается TestEveryServiceAuthzMapIsComparedAgainstTheCatalog
		}
		if p.DivergenceVarDecls != 1 {
			t.Errorf("%s: переменная %s объявлена %d раз(а), ожидается ровно одна. Реестр "+
				"расхождений собирается разбором именно её — при другом количестве разбор "+
				"молча читает не то, и репо-широкий пин перестаёт что-либо ловить.",
				p.Dir, divergenceVarName, p.DivergenceVarDecls)
		}
		if p.DiffCalls == 0 {
			t.Errorf("%s: %s объявлена, но никуда не передана (ожидается `.Diff(%s)`). "+
				"Список исключений, который не участвует в сравнении, оставляет тест "+
				"сравнивающим против пустоты — расхождения он будет показывать как новые, "+
				"а реестр здесь — как пустой.",
				p.Dir, divergenceVarName, divergenceVarName)
		}
	}

	embedded, err := os.ReadFile(filepath.Join(root, catalogEmbedPath))
	if err != nil {
		t.Fatalf("не прочитан вшитый каталог %s: %v — реестр расхождений сверяется с ним, "+
			"без него гейт беспредметен", catalogEmbedPath, err)
	}
	mirror, err := os.ReadFile(filepath.Join(root, iamCatalogMirrorPath))
	if err != nil {
		t.Fatalf("не прочитано зеркало каталога %s: %v", iamCatalogMirrorPath, err)
	}
	if string(embedded) != string(mirror) {
		t.Errorf("две вшитые копии каталога разошлись:\n  %s\n  %s\n\n"+
			"Гейты выше говорят «каталог» в единственном числе; при разъезде копий неясно, "+
			"с какой из них сверялась карта сервиса и по какой выдаются права. "+
			"Пересинхронизируй обе из перегенерированной (`make -C gateway permission-catalog-apply`).",
			catalogEmbedPath, iamCatalogMirrorPath)
	}

	// Перепись предпосылок: сколько пакетов осмотрено и у скольких из них найдены обе
	// несущие конструкции. Проверка предпосылки, о результате которой не сказано,
	// сама становится тем, от чего защищает.
	withVar, withDiff := 0, 0
	for _, p := range pkgs {
		if p.DivergenceVarDecls == 1 {
			withVar++
		}
		if p.DiffCalls > 0 {
			withDiff++
		}
	}
	t.Logf("перепись предпосылок: пакетов с картой прав %d; из них объявляют реестр ровно "+
		"один раз %d, передают его в сравнение %d; копий каталога сверено 2, байт %d",
		len(pkgs), withVar, withDiff, len(embedded))
}

// discoverAuthzMapPackages — находит пакеты с `func PermissionMap() authz.RPCMap`
// и собирает по каждому тесты чётности + перечисленные исключения.
//
// Разбор идёт через go/ast, а не регулярками: предмет — объявления и вызовы, и
// строковый поиск по ним даёт ложные срабатывания на прозе и закомментированном
// коде.
func discoverAuthzMapPackages(t *testing.T, root string) []authzMapPackage {
	t.Helper()

	byDir := map[string]*authzMapPackage{}
	fset := token.NewFileSet()

	// Состав дерева — из индекса git, а не с диска: под корнем лежат
	// git-игнорируемые рабочие копии (`.claude/worktrees/`), и прочитанный
	// оттуда PermissionMap приписал бы пакет, которого в репозитории нет.
	// Отсев по ИМЕНИ каталога (`.claude`) здесь стоял, и он же был хрупок:
	// он закрывал ровно одно известное имя, а не класс.
	tt := newTrackedTree(t, root)
	rels := make([]string, 0, tt.count())
	for rel := range tt.files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	err := func() error {
		for _, rel := range rels {
			if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
				continue
			}
			if strings.HasPrefix(rel, "pkg/api/") || // сгенерированные стабы
				strings.HasPrefix(rel, "vendor/") ||
				strings.HasPrefix(rel, "node_modules/") ||
				strings.HasPrefix(rel, "ui-future/") ||
				strings.HasPrefix(rel, "docs-site/") {
				continue
			}
			path := filepath.Join(root, filepath.FromSlash(rel))
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				continue // не компилируемый/шаблонный файл — не предмет этого гейта
			}
			if !declaresPermissionMap(f) {
				continue
			}
			dir := filepath.Dir(rel)
			byDir[dir] = &authzMapPackage{Dir: dir, MapFile: rel}
		}
		return nil
	}()
	if err != nil {
		t.Fatalf("обход дерева: %v", err)
	}

	for dir, p := range byDir {
		entries, rerr := os.ReadDir(filepath.Join(root, dir))
		if rerr != nil {
			t.Fatalf("чтение каталога %s: %v", dir, rerr)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			rel := filepath.Join(dir, e.Name())
			f, perr := parser.ParseFile(fset, filepath.Join(root, rel), nil, 0)
			if perr != nil {
				continue
			}
			comparesAgainstCatalog, decls, diffCalls, divergences := inspectParityTest(f)
			p.DivergenceVarDecls += decls
			p.DiffCalls += diffCalls
			p.Divergences = append(p.Divergences, divergences...)
			if comparesAgainstCatalog {
				p.ParityFiles = append(p.ParityFiles, rel)
			}
		}
	}

	out := make([]authzMapPackage, 0, len(byDir))
	for _, p := range byDir {
		sort.Strings(p.ParityFiles)
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out
}

// declaresPermissionMap — файл объявляет `func PermissionMap() authz.RPCMap`.
func declaresPermissionMap(f *ast.File) bool {
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name == nil || fn.Name.Name != "PermissionMap" {
			continue
		}
		if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
			continue
		}
		sel, ok := fn.Type.Results.List[0].Type.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != "RPCMap" {
			continue
		}
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "authz" {
			return true
		}
	}
	return false
}

// inspectParityTest — читает один _test.go: сверяется ли он с каталогом, сколько
// раз объявляет divergenceVarName, сколько раз передаёт её в `.Diff(`, и какие
// строки она содержит.
func inspectParityTest(f *ast.File) (compares bool, decls int, diffCalls int, divergences []string) {
	ast.Inspect(f, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			sel, ok := node.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel == nil {
				return true
			}
			if sel.Sel.Name == "Compare" {
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "catalogparity" {
					compares = true
				}
			}
			if sel.Sel.Name == "Diff" {
				for _, arg := range node.Args {
					if id, ok := arg.(*ast.Ident); ok && id.Name == divergenceVarName {
						diffCalls++
					}
				}
			}
		case *ast.ValueSpec:
			for i, name := range node.Names {
				if name.Name != divergenceVarName {
					continue
				}
				// Объявление считается ВСЕГДА, в том числе `var x []string` без
				// значения: сервис без исключений записывает его именно так, и
				// пропустить его значило бы объявить работающий пакет
				// неразбираемым.
				decls++
				if i >= len(node.Values) {
					continue
				}
				lit, ok := node.Values[i].(*ast.CompositeLit)
				if !ok {
					continue
				}
				for _, el := range lit.Elts {
					bl, ok := el.(*ast.BasicLit)
					if !ok || bl.Kind != token.STRING {
						continue
					}
					s, uerr := strconv.Unquote(bl.Value)
					if uerr != nil {
						continue
					}
					divergences = append(divergences, s)
				}
			}
		}
		return true
	})
	return compares, decls, diffCalls, divergences
}

// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// catalogreachability_test.go — гейт на класс «каталог прав объявляет полосу
// авторизации для метода, которого не обслуживает ни один листенер», плюс
// доказательство того, что анализатор способен упасть.
//
// РАДИУС. Полоса монтирования (grpcmountparity) отвечает на вопрос «объявлен ли
// сервис, которого нет ни на одном листенере». Здесь — вопрос с другой стороны и о
// другом артефакте: не «сервис не поднят», а «в каталоге прав есть СТРОКА, которая
// решает о доступе к вызову, который не состоится». Один класс через две стороны
// не закрывается: каталог генерируется из proto и потому содержит запись на каждый
// объявленный метод независимо от того, монтируется ли его сервис, а анализатор
// монтирования про каталог не знает вовсе.
//
// ЧТО БЫЛО ИЗМЕРЕНО на ревизии bdafe2c4: 294 строки каталога, все до одной
// резолвятся в существующий метод контракта (то есть ни одна не пережила снятие
// метода), и ПЯТЬ из них называют метод, чей сервис не смонтирован ни в одном
// композиционном корне. Ни один гейт дерева на этом не падал.
//
// ЧТО ИЗМЕРЕНО СЕЙЧАС (после снятия): 289 строк каталога, инертных — НОЛЬ. Те пять
// строк не были «исправлены» переносом в список исключений: сняты сами объявления,
// потому что у всех четырёх сервисов не было ни сервера, ни клиента, ни одной
// неgenerated-ссылки — включая типы сообщений. Надгробие снятых имён и гейт на их
// возвращение — retiredrpcsurface.go: этот анализатор такое возвращение НЕ ловит по
// построению (имя, вернувшееся вместе с реализацией, смонтировано, и его строки
// резолвятся), а именно ради этого случая имена и резервируют.
package repohygiene

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// knownInertCatalogRows — строки каталога, объявляющие полосу для метода, который
// сегодня не обслуживается. Перечень — ПИН, а не разрешение: разрешение выдаётся
// на уровне СЕРВИСА и ровно одно на два гейта (mountAllow). Здесь зафиксировано,
// во что это решение обходится каталогу, чтобы цена была видна и чтобы она не
// росла молча.
//
// Каждая строка обязана быть следствием записи mountAllow — это проверяется, а не
// предполагается (TestCatalogReachability_InertRowsAreExactlyTheAllowedServices).
//
// СЕЙЧАС ПУСТ, и это исход, а не упущение. Пять строк, стоявших здесь прежде,
// снялись вместе со своими объявлениями — четыре сервиса без единой реализации
// (см. retiredRPCSurface в retiredrpcsurface_test.go). Шестая, про глагол
// подписки платформы, снялась ИНАЧЕ и по своему предикату: её метод СМОНТИРОВАН
// владельцами журналов, то есть исключать стало нечего. Пустой перечень означает,
// что каталог целиком состоит из строк, за которыми стоит обслуживаемый метод.
// Способность анализатора находить инертные строки от этой пустоты не зависит —
// она доказана инъекцией, см. TestCatalogReachability_RedOnAnUnmountedService.
var knownInertCatalogRows = []string{}

// catalogReachabilityOptions — вход на НАСТОЯЩЕМ дереве. Список исключений
// передаётся из mountAllow: сервис, намеренно не поднимаемый по gRPC, — это ОДНО
// решение, и его вторая копия здесь была бы поверхностью правки, за которой никто
// не смотрит.
func catalogReachabilityOptions(t *testing.T, allow []string) CatalogReachabilityOptions {
	t.Helper()
	return CatalogReachabilityOptions{
		Root:        repoRoot(t),
		CatalogPath: filepath.Join("gateway", "internal", "middleware", "embed", "permission_catalog.json"),
		APIRoot:     "pkg/api",
		ModulePath:  "github.com/PRO-Robotech/kacho",
		Roots:       []string{"services", "gateway"},
		Allow:       allow,
	}
}

// TestCatalogReachability_EveryRowResolvesToAServedMethod — положительная сторона
// на настоящем дереве.
func TestCatalogReachability_EveryRowResolvesToAServedMethod(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditCatalogReachability(catalogReachabilityOptions(t, mountAllow), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: анализатор действительно прочитал обе стороны. Ноль находок обязано
	// быть отличимо от нуля прочитанного — разбор, вернувший пустое множество
	// методов, прошёл бы «чисто» на любом каталоге.
	if census.CatalogRows < 100 {
		t.Fatalf("строк каталога прочитано %d (< 100) — это не тот каталог, и вердикт беспредметен", census.CatalogRows)
	}
	if census.DeclaredMethods < 100 || census.DeclaredSvcs < 10 {
		t.Fatalf("из стабов прочитано сервисов %d, методов %d — разбор дескрипторов не нашёл того, "+
			"что заведомо есть", census.DeclaredSvcs, census.DeclaredMethods)
	}
	if census.OwningBinaries < 5 {
		t.Fatalf("монтирующих композиционных корней найдено %d (< 5) — «ничего не смонтировано» "+
			"получено даром", census.OwningBinaries)
	}
	if census.RowsResolved == 0 {
		t.Fatalf("ни одна строка каталога не резолвится в смонтированный метод — предмета нет")
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("%d записей каталога не резолвятся в обслуживаемый метод:\n%s",
		len(findings), strings.Join(lines, "\n"))
}

// TestCatalogReachability_InertRowsAreExactlyTheAllowedServices — цена решения
// «сервис намеренно не поднят», выраженная в строках каталога.
//
// Гоняется с ПУСТЫМ списком исключений: инвентарь инертных строк обязан совпасть
// с переписью выше — ни больше, ни меньше. Появилась новая — она здесь и
// покажется, с именем.
//
// ОТКУДА БЕРЁТСЯ СПОСОБНОСТЬ АНАЛИЗАТОРА НАХОДИТЬ. Прежняя редакция доказывала
// её тем, что на НАСТОЯЩЕМ дереве инертные строки нашлись, и падала, когда их
// оказывалось ноль. Это делало проверку зависимой от наличия дефекта: снятие
// последней инертной строки — то, ради чего гейт и писался, — красило её в
// красный, и единственным способом вернуть зелёный было вернуть дефект.
// Способность доказывается инъекцией на синтетическом дереве
// (TestCatalogReachability_RedOnAnUnmountedService), а здесь остаётся то, что
// этот тест действительно измеряет: инвентарь. Пустой инвентарь — законный
// исход, и он обязан быть отличим от «ничего не прочитано», поэтому перепись
// проверяется отдельно.
func TestCatalogReachability_InertRowsAreExactlyTheAllowedServices(t *testing.T) {
	findings, census, err := AuditCatalogReachability(catalogReachabilityOptions(t, nil), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}

	// Премиса: «инертных строк ноль» получено на прочитанном дереве, а не на пустом.
	if census.CatalogRows < 100 || census.DeclaredMethods < 100 || census.OwningBinaries < 5 {
		t.Fatalf("прочитано строк каталога %d, методов контракта %d, монтирующих корней %d — "+
			"пустой инвентарь получен даром", census.CatalogRows, census.DeclaredMethods, census.OwningBinaries)
	}

	got := make([]string, 0, len(findings))
	for _, f := range findings {
		if f.Kind != "unmounted" {
			t.Errorf("на настоящем дереве найдена находка рода %q: %s", f.Kind, f.String())
			continue
		}
		got = append(got, f.FQN)
	}
	sort.Strings(got)
	want := append([]string(nil), knownInertCatalogRows...)
	sort.Strings(want)

	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("инвентарь инертных строк изменился.\nбыло:\n  %s\nстало:\n  %s\n"+
			"это не опечатка в списке: строка ушла — значит метод смонтировали (снимите запись), "+
			"строка появилась — значит контракт объявил полосу для метода, которого никто не обслуживает",
			strings.Join(want, "\n  "), strings.Join(got, "\n  "))
	}

	// Каждая инертная строка обязана быть следствием ЗАПИСИ mountAllow, а не
	// самостоятельным разрешением: иначе список выше стал бы вторым, независимым
	// разрешительным механизмом.
	allowed := map[string]struct{}{}
	for _, a := range mountAllow {
		allowed[a] = struct{}{}
	}
	for _, fqn := range got {
		svc := fqn[:strings.IndexByte(fqn, '/')]
		if _, ok := allowed[svc]; !ok {
			t.Errorf("инертная строка %s называет сервис %s, которого нет в mountAllow — "+
				"решение «не монтируем» принято здесь и нигде больше не записано", fqn, svc)
		}
	}

	// Перепись: проверка предпосылки выше роняет гейт на скудном инвентаре, но её
	// ПРОХОЖДЕНИЕ было молчаливым — а молчание неотличимо от того, что не смотрели.
	t.Logf("перепись: строк каталога %d, методов контракта %d, модулей-владельцев %d; "+
		"находок рода unmounted %d", census.CatalogRows, census.DeclaredMethods,
		census.OwningBinaries, len(got))
}

// ── доказательство того, что анализатор способен упасть ─────────────────────

// catalogTinyTree материализует минимальное дерево: стабы двух сервисов одного
// proto-пакета, композиционный корень, монтирующий ОДИН из них, и каталог,
// содержимое которого задаёт вызывающий.
func catalogTinyTree(t *testing.T, rows []map[string]any) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	// Дескрипторы в форме, которую эмитит protoc-gen-go-grpc: два сервиса в одном
	// файле — тот случай, в котором текстовый разбор приписал бы методы одного
	// другому.
	write("pkg/api/kacho/cloud/demo/v1/demo_grpc.pb.go", `package demov1

import grpc "google.golang.org/grpc"

var AlphaService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "kacho.cloud.demo.v1.AlphaService",
	Methods: []grpc.MethodDesc{
		{MethodName: "Get"},
	},
	Streams: []grpc.StreamDesc{
		{StreamName: "Follow"},
	},
}

var BetaService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "kacho.cloud.demo.v1.BetaService",
	Methods: []grpc.MethodDesc{
		{MethodName: "Ping"},
	},
}

func RegisterAlphaServiceServer(s any, i any) {}
func RegisterBetaServiceServer(s any, i any)  {}
`)
	// Смонтирован только Alpha. Регистрация Beta присутствует В КОММЕНТАРИИ —
	// монтированием это не является, и текстовый разбор ошибся бы здесь.
	write("services/demo/cmd/demo/main.go", `package main

import demov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/demo/v1"

func main() {
	var srv any
	demov1.RegisterAlphaServiceServer(srv, nil)
	// demov1.RegisterBetaServiceServer(srv, nil) — снят намеренно
}
`)
	b, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	write("catalog.json", string(b))
	return root
}

func row(fqn string) map[string]any {
	return map[string]any{"fqn": fqn, "permission": "<exempt>", "required_relation": ""}
}

func auditTiny(t *testing.T, root string, allow []string) []CatalogReachabilityFinding {
	t.Helper()
	findings, census, err := AuditCatalogReachability(CatalogReachabilityOptions{
		Root:        root,
		CatalogPath: "catalog.json",
		APIRoot:     "pkg/api",
		ModulePath:  "github.com/PRO-Robotech/kacho",
		Roots:       []string{"services"},
		Allow:       allow,
	}, nil)
	if err != nil {
		t.Fatalf("анализатор не отработал на синтетическом дереве: %v", err)
	}
	if census.DeclaredMethods != 3 {
		t.Fatalf("в синтетическом дереве прочитано %d методов, ожидалось 3 — разбор дескрипторов "+
			"сломан, и любой вердикт ниже беспредметен", census.DeclaredMethods)
	}
	if census.MountedSvcs != 1 {
		t.Fatalf("в синтетическом дереве смонтированным признан %d сервис(ов), ожидался 1 — "+
			"либо закомментированная регистрация посчитана монтированием, либо разбор корня сломан",
			census.MountedSvcs)
	}
	return findings
}

// TestCatalogReachability_SilentOnAServedMethod — законный близнец. Строка,
// называющая метод СМОНТИРОВАННОГО сервиса, обязана проходить молча, иначе гейт
// ловит форму, а не существо, и первый же ложный срабат его отключит.
func TestCatalogReachability_SilentOnAServedMethod(t *testing.T) {
	root := catalogTinyTree(t, []map[string]any{
		row("kacho.cloud.demo.v1.AlphaService/Get"),
		row("kacho.cloud.demo.v1.AlphaService/Follow"), // потоковый метод — тоже обслуживается
	})
	if f := auditTiny(t, root, nil); len(f) != 0 {
		t.Errorf("гейт сработал на строках, называющих смонтированные методы: %v", f)
	}
}

// TestCatalogReachability_RedOnAnUnmountedService — настоящий вход: сервис есть в
// контракте, но композиционный корень его не монтирует.
func TestCatalogReachability_RedOnAnUnmountedService(t *testing.T) {
	root := catalogTinyTree(t, []map[string]any{
		row("kacho.cloud.demo.v1.AlphaService/Get"),
		row("kacho.cloud.demo.v1.BetaService/Ping"),
	})
	f := auditTiny(t, root, nil)
	if len(f) != 1 || f[0].Kind != "unmounted" {
		t.Fatalf("ожидалась ровно одна находка рода unmounted, получено %v", f)
	}
	if !strings.Contains(f[0].FQN, "BetaService/Ping") {
		t.Errorf("находка не называет координату: %s", f[0].String())
	}
}

// TestCatalogReachability_RedOnAMethodTheContractDoesNotHave — строка пережила
// снятие метода: сервис смонтирован, а метода у него нет.
func TestCatalogReachability_RedOnAMethodTheContractDoesNotHave(t *testing.T) {
	root := catalogTinyTree(t, []map[string]any{
		row("kacho.cloud.demo.v1.AlphaService/Get"),
		row("kacho.cloud.demo.v1.AlphaService/Vanished"),
	})
	f := auditTiny(t, root, nil)
	if len(f) != 1 || f[0].Kind != "unknown-method" {
		t.Fatalf("ожидалась ровно одна находка рода unknown-method, получено %v", f)
	}
}

// TestCatalogReachability_RedOnAServiceTheContractDoesNotHave — строка пережила
// снятие сервиса целиком.
func TestCatalogReachability_RedOnAServiceTheContractDoesNotHave(t *testing.T) {
	root := catalogTinyTree(t, []map[string]any{
		row("kacho.cloud.demo.v1.AlphaService/Get"),
		row("kacho.cloud.demo.v1.GammaService/Get"),
	})
	f := auditTiny(t, root, nil)
	if len(f) != 1 || f[0].Kind != "unknown-service" {
		t.Fatalf("ожидалась ровно одна находка рода unknown-service, получено %v", f)
	}
}

// TestCatalogReachability_MethodsAreNotBorrowedBetweenDescriptors — метод одного
// сервиса не должен считаться методом другого. Именно здесь текстовый разбор
// («ищем MethodName в файле») дал бы ложно-зелёное: `Ping` лежит в том же файле.
func TestCatalogReachability_MethodsAreNotBorrowedBetweenDescriptors(t *testing.T) {
	root := catalogTinyTree(t, []map[string]any{
		row("kacho.cloud.demo.v1.AlphaService/Ping"), // Ping принадлежит Beta, не Alpha
	})
	f := auditTiny(t, root, nil)
	if len(f) != 1 || f[0].Kind != "unknown-method" {
		t.Fatalf("метод соседнего дескриптора зачтён своему сервису — разбор идёт по тексту, "+
			"а не по дескриптору: %v", f)
	}
}

// TestCatalogReachability_AllowExcusesAndThenExpires — послабление работает и
// само истекает.
func TestCatalogReachability_AllowExcusesAndThenExpires(t *testing.T) {
	const beta = "kacho.cloud.demo.v1.BetaService"

	// (а) есть что исключать — молчит.
	root := catalogTinyTree(t, []map[string]any{
		row("kacho.cloud.demo.v1.AlphaService/Get"),
		row(beta + "/Ping"),
	})
	if f := auditTiny(t, root, []string{beta}); len(f) != 0 {
		t.Errorf("послабление не применилось: %v", f)
	}

	// (б) исключать больше нечего — запись сама становится находкой. Без этого
	// слепое пятно унаследует следующий сервис, которому достанется это имя.
	root = catalogTinyTree(t, []map[string]any{
		row("kacho.cloud.demo.v1.AlphaService/Get"),
	})
	f := auditTiny(t, root, []string{beta})
	if len(f) != 1 || f[0].Kind != "stale-allow" || f[0].FQN != beta {
		t.Fatalf("послабление, которому нечего исключать, пережило свой предмет: %v", f)
	}
}

// TestCatalogReachability_EmptyCatalogIsAnError — предпосылка гейта. Пустой (или
// не тот) каталог обязан быть ошибкой, а не «ноль находок».
func TestCatalogReachability_EmptyCatalogIsAnError(t *testing.T) {
	root := catalogTinyTree(t, []map[string]any{})
	_, _, err := AuditCatalogReachability(CatalogReachabilityOptions{
		Root: root, CatalogPath: "catalog.json", APIRoot: "pkg/api",
		ModulePath: "github.com/PRO-Robotech/kacho", Roots: []string{"services"},
	}, nil)
	if err == nil {
		t.Fatal("пустой каталог принят как «ноль находок» — гейт зеленел бы на отсутствии предмета")
	}
}

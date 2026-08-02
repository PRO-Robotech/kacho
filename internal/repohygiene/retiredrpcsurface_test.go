// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// retiredRPCSurface — НАДГРОБИЕ: имена, снятые с контракта, которые не должны
// вернуться молча.
//
// Почему перепись, а не `reserved` в самом контракте, — см. шапку
// retiredrpcsurface.go: грамматика protobuf не принимает `reserved` внутри
// `service`, а у метода нет номера. Это единственная форма резервирования,
// выразимая для снятой RPC-поверхности в этом дереве.
//
// Запись НЕ истекает. Надгробие — не послабление: послабление живёт, пока у него
// есть предмет, а надгробие обязано пережить любое количество зелёных прогонов,
// иначе имя вернётся ровно тогда, когда про него забудут. Снять запись —
// осознанное решение владельца контракта, а не следствие того, что «давно
// ничего не находило».
var retiredRPCSurface = []RetiredRPC{
	{
		FQN: "kacho.cloud.compute.v1.InternalResourceLifecycleService/Subscribe",
		Reason: "фид жизненного цикла ресурсов был объявлен в трёх доменах, а реализован " +
			"ровно в одном (loadbalancer.v1). Объявление compute не несло ни сервера, ни клиента, " +
			"ни одной неgenerated-ссылки — включая типы сообщений",
	},
	{
		FQN: "kacho.cloud.vpc.v1.InternalResourceLifecycleService/Subscribe",
		Reason: "то же объявление в домене vpc — без сервера, клиента и неgenerated-ссылок. " +
			"Живым остаётся loadbalancer.v1.InternalResourceLifecycleService/Subscribe",
	},
	{
		FQN: "kacho.cloud.vpc.v1.InternalWatchService/Watch",
		Reason: "поток событий из outbox поднят у compute (compute.v1.InternalWatchService/Watch, " +
			"он живой); объявление vpc осталось без единой реализации и без неgenerated-ссылок",
	},
	{
		FQN: "kacho.cloud.iam.v1.InternalIamHooksService/TokenHook",
		Reason: "хуки Hydra обслуживаются по HTTP (services/iam/internal/handler/iamhooks), и обслуживаются " +
			"СВОИМИ структурами тела запроса под контракт Hydra — типы этого proto не читает ни одна строка " +
			"неgenerated-кода. gRPC-объявление описывало замысел, который не был реализован",
	},
	{
		FQN: "kacho.cloud.iam.v1.InternalIamHooksService/RefreshTokenHook",
		Reason: "вторая половина того же неreализованного gRPC-объявления хуков Hydra; живой путь — " +
			"HTTP-обработчик refresh_hook_handler.go со своей формой тела",
	},
}

func retiredRPCSurfaceOptions(t *testing.T) RetiredRPCSurfaceOptions {
	t.Helper()
	return RetiredRPCSurfaceOptions{
		Root:      repoRoot(t),
		APIRoot:   "pkg/api",
		ProtoRoot: "proto",
		CatalogPaths: []string{
			filepath.Join("gateway", "internal", "middleware", "embed", "permission_catalog.json"),
			filepath.Join("services", "iam", "internal", "apps", "kacho", "seed", "embedded", "permission_catalog.json"),
		},
		Retired: retiredRPCSurface,
	}
}

// TestRetiredRPCSurface_NoRetiredNameCameBack — положительная сторона на
// НАСТОЯЩЕМ дереве.
func TestRetiredRPCSurface_NoRetiredNameCameBack(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditRetiredRPCSurface(retiredRPCSurfaceOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: анализатор прочитал все три плеча. Ноль находок обязано быть
	// отличимо от нуля прочитанного.
	if census.DeclaredMethods < 100 || census.DeclaredSvcs < 10 {
		t.Fatalf("из стабов прочитано сервисов %d, методов %d — разбор не нашёл того, что заведомо есть",
			census.DeclaredSvcs, census.DeclaredMethods)
	}
	if census.ProtoFiles < 20 || census.ProtoSvcs < 10 {
		t.Fatalf("из контракта прочитано файлов %d, сервисов %d — разбор не нашёл того, что заведомо есть",
			census.ProtoFiles, census.ProtoSvcs)
	}
	if census.CatalogFiles != 2 || census.CatalogRows < 200 {
		t.Fatalf("копий каталога прочитано %d, строк суммарно %d — прочитаны не обе копии",
			census.CatalogFiles, census.CatalogRows)
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("%d снятых имён вернулись в дерево:\n%s", len(findings), strings.Join(lines, "\n"))
}

// ── доказательство того, что анализатор способен упасть ─────────────────────

// retiredTinyTree материализует минимальное дерево: стабы, контракт и каталог,
// содержимое которых задаёт вызывающий.
func retiredTinyTree(t *testing.T, protoBody, stubBody string, catalogFQNs []string) string {
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
	write("pkg/api/kacho/cloud/demo/v1/demo_grpc.pb.go", stubBody)
	write("proto/kacho/cloud/demo/v1/demo.proto", protoBody)
	rows := make([]string, 0, len(catalogFQNs))
	for _, f := range catalogFQNs {
		rows = append(rows, `{"fqn":"`+f+`","permission":"demo.p"}`)
	}
	write("catalog.json", "["+strings.Join(rows, ",")+"]")
	return root
}

func retiredTinyOptions(root string, retired ...RetiredRPC) RetiredRPCSurfaceOptions {
	return RetiredRPCSurfaceOptions{
		Root: root, APIRoot: "pkg/api", ProtoRoot: "proto",
		CatalogPaths: []string{"catalog.json"}, Retired: retired,
	}
}

// Стабы в форме, которую эмитит protoc-gen-go-grpc: два сервиса в одном файле —
// тот случай, в котором текстовый разбор приписал бы методы одного другому.
const retiredTinyStubs = `package demov1

var AlphaService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "kacho.cloud.demo.v1.AlphaService",
	Methods: []grpc.MethodDesc{{MethodName: "Ping"}},
}
var BetaService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "kacho.cloud.demo.v1.BetaService",
	Methods: []grpc.MethodDesc{{MethodName: "Pong"}},
}
`

const retiredTinyProto = `syntax = "proto3";
package kacho.cloud.demo.v1;
service AlphaService {
  rpc Ping (Req) returns (Res);
}
service BetaService {
  rpc Pong (Req) returns (Res);
}
`

// TestRetiredRPCSurface_CatchesEachReturnPath — инъекция НАСТОЯЩИМ входом:
// снятое имя, вернувшееся каждым из трёх путей, обязано быть найдено с
// координатой.
func TestRetiredRPCSurface_CatchesEachReturnPath(t *testing.T) {
	// Имя, объявленное всюду: и в контракте, и в стабах, и в каталоге.
	root := retiredTinyTree(t, retiredTinyProto, retiredTinyStubs,
		[]string{"kacho.cloud.demo.v1.AlphaService/Ping"})
	dead := RetiredRPC{FQN: "kacho.cloud.demo.v1.AlphaService/Ping", Reason: "снято в тесте"}

	findings, _, err := AuditRetiredRPCSurface(retiredTinyOptions(root, dead), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	kinds := map[string]bool{}
	for _, f := range findings {
		kinds[f.Kind] = true
		if !strings.Contains(f.String(), "AlphaService/Ping") {
			t.Errorf("находка не называет координату: %s", f.String())
		}
		if !strings.Contains(f.Reason, "снято в тесте") {
			t.Errorf("находка не несёт причину снятия: %s", f.String())
		}
	}
	for _, want := range []string{"redeclared-stub", "redeclared-proto", "catalog-row"} {
		if !kinds[want] {
			t.Errorf("путь возвращения %q не пойман (найдено: %v)", want, kinds)
		}
	}
}

// TestRetiredRPCSurface_SilentOnLegitimateTwin — вторая сторона контроля:
// ЗАКОННАЯ конструкция той же формы обязана проходить молча.
//
// Близнец подобран так, чтобы срабатывание могло случиться только по существу, а
// не по форме: живой сервис лежит в ТОМ ЖЕ файле контракта и в том же файле
// стабов, что и снятый, и его метод называется так же (`Ping`) — различаются
// только имена сервисов. Гейт, ключующийся на имени метода или на имени файла,
// здесь покраснеет.
func TestRetiredRPCSurface_SilentOnLegitimateTwin(t *testing.T) {
	root := retiredTinyTree(t, retiredTinyProto, retiredTinyStubs,
		[]string{"kacho.cloud.demo.v1.AlphaService/Ping", "kacho.cloud.demo.v1.BetaService/Pong"})

	// Снят метод, которого в дереве действительно нет: у BetaService есть Pong, но
	// нет Ping; у AlphaService есть Ping, но нет Pong.
	dead := RetiredRPC{FQN: "kacho.cloud.demo.v1.BetaService/Ping", Reason: "снято в тесте"}

	findings, census, err := AuditRetiredRPCSurface(retiredTinyOptions(root, dead), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("законная конструкция той же формы вызвала %d находок — гейт ловит форму, а не существо:\n  %s",
			len(findings), findings[0].String())
	}
	// Премиса самого контроля: молчание получено на прочитанном дереве, а не на пустом.
	if census.DeclaredMethods == 0 || census.ProtoSvcs == 0 || census.CatalogRows == 0 {
		t.Fatalf("контроль молчал на пустом входе (методов %d, сервисов контракта %d, строк каталога %d) — "+
			"он ничего не доказывает", census.DeclaredMethods, census.ProtoSvcs, census.CatalogRows)
	}
}

// TestRetiredRPCSurface_EmptyLedgerIsAnError — пустое надгробие не «ноль
// находок», а ошибка: инертный гейт зеленеет на любом дереве.
func TestRetiredRPCSurface_EmptyLedgerIsAnError(t *testing.T) {
	root := retiredTinyTree(t, retiredTinyProto, retiredTinyStubs, []string{"kacho.cloud.demo.v1.AlphaService/Ping"})
	if _, _, err := AuditRetiredRPCSurface(retiredTinyOptions(root), nil); err == nil {
		t.Fatal("пустая перепись прошла как «ноль находок» — гейт инертен и об этом не сообщает")
	}
}

// TestRetiredRPCSurface_ReadsEveryCatalogCopy — копия, которую забыли
// перегенерировать, обязана быть найдена. Именно ради этого случая читаются обе.
func TestRetiredRPCSurface_ReadsEveryCatalogCopy(t *testing.T) {
	root := retiredTinyTree(t, retiredTinyProto, retiredTinyStubs, nil)
	// Первая копия чистая, вторая — со снятым именем.
	if err := os.WriteFile(filepath.Join(root, "catalog.json"),
		[]byte(`[{"fqn":"kacho.cloud.demo.v1.BetaService/Pong","permission":"demo.p"}]`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "catalog2.json"),
		[]byte(`[{"fqn":"kacho.cloud.demo.v1.AlphaService/Ping","permission":"demo.p"}]`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	opts := retiredTinyOptions(root, RetiredRPC{FQN: "kacho.cloud.demo.v1.AlphaService/Ping", Reason: "снято в тесте"})
	opts.CatalogPaths = []string{"catalog.json", "catalog2.json"}

	findings, _, err := AuditRetiredRPCSurface(opts, nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	var found bool
	for _, f := range findings {
		if f.Kind == "catalog-row" && f.Where == "catalog2.json" {
			found = true
		}
	}
	if !found {
		t.Errorf("строка снятого имени во ВТОРОЙ копии каталога не найдена (находки: %v)", findings)
	}
}

// TestRetiredRPCSurface_LedgerNamesAreWellFormedAndUnique — сама перепись
// обязана быть переписью: форма имени и отсутствие дублей.
func TestRetiredRPCSurface_LedgerNamesAreWellFormedAndUnique(t *testing.T) {
	seen := map[string]struct{}{}
	for _, r := range retiredRPCSurface {
		if _, _, ok := splitFQN(r.FQN); !ok {
			t.Errorf("запись %q не имеет формы `<сервис>/<метод>`", r.FQN)
		}
		if strings.TrimSpace(r.Reason) == "" {
			t.Errorf("запись %q не несёт причины снятия — надгробие без надписи ничего не сообщает следующему", r.FQN)
		}
		if _, dup := seen[r.FQN]; dup {
			t.Errorf("запись %q задвоена", r.FQN)
		}
		seen[r.FQN] = struct{}{}
	}
	got := make([]string, 0, len(retiredRPCSurface))
	for _, r := range retiredRPCSurface {
		got = append(got, r.FQN)
	}
	if !sort.StringsAreSorted(got) {
		t.Log("перепись не отсортирована — это не ошибка, но затрудняет чтение диффа")
	}
}

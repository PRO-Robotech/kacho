// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_iam_requestbody_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что
// анализатор способен упасть по КАЖДОМУ из двух своих предикатов, называет
// координату и молчит на законном близнеце.
//
// Стенд синтетический наполовину и это осознанно: дескрипторы берутся НАСТОЯЩИЕ
// (подделать регистр протобуфа нельзя, да и незачем — предмет проверки не в
// нём), а страницы документации и прод-код use-case'ов — синтетические, в
// t.TempDir(). Инъекции вносятся ПО ОДНОЙ, к каждой приложен законный близнец
// той же формы, обязанный молчать.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type bodyStand struct{ root string }

func newBodyStand(t *testing.T) *bodyStand {
	t.Helper()
	s := &bodyStand{root: t.TempDir()}
	// Прод-код use-case'а: ОДНО поле помечено невходным, второе отвергается по
	// обычному поводу (формат). Второе в набор попасть НЕ должно — иначе гейт
	// запретил бы присылать всё, что вообще проверяется.
	s.write(t, "usecase/account/create.go", `package account

func run() error {
	// В КОММЕНТАРИИ стоит shared.InvalidArg("labels", "output-only") — и это не
	// вызов. Анализатор, читающий сырой текст, внёс бы labels в набор.
	if x != "" {
		return shared.InvalidArg("description", "Illegal argument description (derived from caller)")
	}
	if y != "" {
		return shared.InvalidArg("name", "Illegal argument name (must match ^[a-z])")
	}
	return nil
}
`)
	// Законная страница: оба ключа — настоящие поля CreateAccountRequest, и ни
	// один не помечен невходным.
	s.write(t, "docs/ok.mdx", curlDoc(`{ "name": "acme", "labels": { "любой": "ключ" } }`))
	return s
}

// curlDoc — страница с одной командой curl к настоящему пути домена.
func curlDoc(body string) string {
	return "# Страница\n\n<CodeBlock language=\"bash\">\n  {dedent`\n" +
		"    curl -X POST 'http://localhost:18080/iam/v1/accounts' \\\\\n" +
		"      -H 'Content-Type: application/json' \\\\\n" +
		"      -d '" + body + "'\n  `}\n</CodeBlock>\n"
}

func (s *bodyStand) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(s.root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (s *bodyStand) run(t *testing.T) ([]ClientTruthIAMRequestBodyFinding, ClientTruthIAMRequestBodyCensus) {
	t.Helper()
	var log strings.Builder
	f, c, err := AuditClientTruthIAMRequestBody(ClientTruthIAMRequestBodyOptions{
		Tree:         clientTruthSyntheticTree(t, s.root),
		ProtoPackage: "kacho.cloud.iam.v1",
		DocsDirs:     []string{"docs"},
		DocExts:      []string{".mdx"},
		UseCaseDirs:  []string{"usecase"},
	}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))
	return f, c
}

// TestBodyGate_SilentOnALegalPage — контроль. Без него любое «краснеет» ниже
// доказывало бы лишь то, что гейт краснеет всегда.
func TestBodyGate_SilentOnALegalPage(t *testing.T) {
	s := newBodyStand(t)
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("на законной странице findings=%d, ожидался 0: %v", len(findings), findings)
	}
	if census.BodiesMatched != 1 || census.KeysJudged < 2 {
		t.Fatalf("тел сопоставлено %d, ключей рассужено %d — сверка не состоялась",
			census.BodiesMatched, census.KeysJudged)
	}
	// Невходное поле выведено РОВНО одно: маркер отличает «поля здесь нет» от
	// «значение не то». Двойка означала бы, что в набор попала обычная проверка
	// формата и гейт запретил бы присылать `name`.
	if census.RejectedFields != 1 {
		t.Fatalf("невходных полей выведено %d, ожидалось 1 (только помеченное маркером)",
			census.RejectedFields)
	}
}

// TestBodyGate_RedOnKeyAbsentFromTheMessage — ПЕРВЫЙ предикат: ключа нет в
// сообщении вовсе (форма дефекта #1615 — снятое тумбстоуном поле).
func TestBodyGate_RedOnKeyAbsentFromTheMessage(t *testing.T) {
	s := newBodyStand(t)
	s.write(t, "docs/ok.mdx", curlDoc(`{ "name": "acme", "scopeRef": { "tier": "PROJECT" } }`))
	findings, _ := s.run(t)
	if len(findings) != 1 {
		t.Fatalf("findings=%d, ожидался ровно 1: %v", len(findings), findings)
	}
	got := findings[0].String()
	if findings[0].Rejected {
		t.Errorf("сработал не тот предикат: ключа в сообщении НЕТ, а находка говорит об отказе кода")
	}
	for _, want := range []string{"docs/ok.mdx", "scopeRef", "CreateAccountRequest", "нет в"} {
		if !strings.Contains(got, want) {
			t.Errorf("находка не называет %q: %s", want, got)
		}
	}
}

// TestBodyGate_RedOnKeyTheCodeRejects — ВТОРОЙ предикат: поле в сообщении ЕСТЬ,
// но код отвергает его присутствие (форма дефекта #1603). Первый предикат этот
// случай НЕ ловит — ради него второй и заведён.
func TestBodyGate_RedOnKeyTheCodeRejects(t *testing.T) {
	s := newBodyStand(t)
	s.write(t, "docs/ok.mdx", curlDoc(`{ "name": "acme", "description": "ACME" }`))
	findings, _ := s.run(t)
	if len(findings) != 1 {
		t.Fatalf("findings=%d, ожидался ровно 1: %v", len(findings), findings)
	}
	if !findings[0].Rejected {
		t.Errorf("сработал не тот предикат: поле в сообщении есть, находка обязана говорить об отказе кода")
	}
	got := findings[0].String()
	for _, want := range []string{"docs/ok.mdx", "description", "отвергает"} {
		if !strings.Contains(got, want) {
			t.Errorf("находка не называет %q: %s", want, got)
		}
	}
}

// TestBodyGate_SilentWhenTheRejectLosesItsMarker — законный близнец второго
// предиката: тот же вызов отказа, но по обычному поводу. Поле обязано остаться
// разрешённым, иначе гейт запрещает присылать всё, что вообще проверяется.
func TestBodyGate_SilentWhenTheRejectLosesItsMarker(t *testing.T) {
	s := newBodyStand(t)
	s.write(t, "usecase/account/create.go", `package account

func run() error {
	return shared.InvalidArg("description", "Illegal argument description (too long)")
}
`)
	s.write(t, "docs/ok.mdx", curlDoc(`{ "name": "acme", "description": "ACME" }`))
	var log strings.Builder
	_, _, err := AuditClientTruthIAMRequestBody(ClientTruthIAMRequestBodyOptions{
		Tree: clientTruthSyntheticTree(t, s.root), ProtoPackage: "kacho.cloud.iam.v1",
		DocsDirs: []string{"docs"}, DocExts: []string{".mdx"},
		UseCaseDirs: []string{"usecase"},
	}, &log)
	// Невходных полей стало ноль — и это ОТКАЗ, а не тихий зелёный: второй
	// предикат остался бы без предмета, а «находок ноль» получено даром.
	if err == nil {
		t.Fatal("набор невходных полей пуст, а анализатор вернул успех")
	}
}

// TestBodyGate_RedOnNestedKey — рекурсия: неизвестный ключ ВНУТРИ объекта.
// Без неё самое неугадываемое место (ветвь oneof) осталось бы вне наблюдения.
func TestBodyGate_RedOnNestedKey(t *testing.T) {
	s := newBodyStand(t)
	s.write(t, "docs/ok.mdx",
		"<CodeBlock language=\"bash\">\n  {dedent`\n"+
			"    curl -X POST 'http://localhost:18080/iam/v1/accessBindings' \\\\\n"+
			"      -d '{ \"roleId\": \"rol1\", \"target\": { \"вседоступно\": {} } }'\n  `}\n</CodeBlock>\n")
	findings, _ := s.run(t)
	if len(findings) != 1 {
		t.Fatalf("findings=%d, ожидался ровно 1: %v", len(findings), findings)
	}
	if !strings.Contains(findings[0].KeyPath, "target.") {
		t.Errorf("находка не называет ПУТЬ до вложенного ключа: %s", findings[0].KeyPath)
	}
}

// TestBodyGate_SilentOnMapKeys — законный близнец рекурсии: у карты ключи
// произвольны by construction, и углубляться в неё значило бы краснеть на
// законном.
func TestBodyGate_SilentOnMapKeys(t *testing.T) {
	s := newBodyStand(t)
	s.write(t, "docs/ok.mdx", curlDoc(`{ "name": "acme", "labels": { "env": "prod", "любой": "ключ" } }`))
	findings, _ := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("ключи карты рассужены как поля: %v", findings)
	}
}

// TestBodyGate_ForeignDomainIsNotAFinding — путь соседнего домена находкой не
// является, но СЧИТАЕТСЯ переписью: «сопоставилось ноль» не должно выглядеть
// как «нарушений ноль».
func TestBodyGate_ForeignDomainIsNotAFinding(t *testing.T) {
	s := newBodyStand(t)
	s.write(t, "docs/ok.mdx",
		"<CodeBlock language=\"bash\">\n  {dedent`\n"+
			"    curl -X POST 'http://localhost:18080/vpc/v1/networks' \\\\\n"+
			"      -d '{ \"чужоеПоле\": 1 }'\n  `}\n</CodeBlock>\n")
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("чужой домен объявлен находкой: %v", findings)
	}
	if census.BodiesUnmatched != 1 {
		t.Fatalf("несопоставленных тел %d, ожидалось 1 — слепая зона не посчитана",
			census.BodiesUnmatched)
	}
}

// TestBodyGate_ResponseBlockIsNotJudged — блок JSON, показывающий ОТВЕТ,
// инструкцией не является: в ответе законны выходные поля, которых на входе нет.
func TestBodyGate_ResponseBlockIsNotJudged(t *testing.T) {
	s := newBodyStand(t)
	s.write(t, "docs/resp.mdx",
		"<CodeBlock language=\"json\">\n  {dedent`\n"+
			"    { \"id\": \"acc1\", \"ownerUserId\": \"usr1\", \"чегоНетНигде\": 1 }\n  `}\n</CodeBlock>\n")
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("блок ответа рассужен как тело запроса: %v", findings)
	}
	if census.CurlBlocks != 1 {
		t.Fatalf("команд curl %d, ожидалась 1 (только законная страница стенда)", census.CurlBlocks)
	}
}

// TestBodyGate_FailsWhenPackageYieldsNoMethods — «ноль находок» обязано быть
// отличимо от «прочитано ноль»: пакета с таким именем нет.
func TestBodyGate_FailsWhenPackageYieldsNoMethods(t *testing.T) {
	s := newBodyStand(t)
	var log strings.Builder
	_, _, err := AuditClientTruthIAMRequestBody(ClientTruthIAMRequestBodyOptions{
		Tree: clientTruthSyntheticTree(t, s.root), ProtoPackage: "kacho.cloud.несуществующий.v1",
		DocsDirs: []string{"docs"}, DocExts: []string{".mdx"},
		UseCaseDirs: []string{"usecase"},
	}, &log)
	if err == nil {
		t.Fatal("методов не выведено ни одного, а анализатор вернул успех")
	}
}

// TestBodyGate_RedOnGrpcurlBody — ВТОРАЯ форма команды. Заведена не для полноты:
// без неё тринадцать блоков `grpcurl` дерева не наблюдались ничем, и в них жили
// настоящие дефекты — включая тело метода `Internal*`, у которого HTTP-привязки
// нет by construction и которого первый распознаватель не увидел бы никогда.
func TestBodyGate_RedOnGrpcurlBody(t *testing.T) {
	s := newBodyStand(t)
	s.write(t, "docs/grpc.mdx",
		"```bash\ngrpcurl -plaintext -d '{\"subject_id\":\"user:u\",\"чегоНет\":1}' \\\\\n"+
			"  localhost:9091 kacho.cloud.iam.v1.InternalIAMService/Check\n```\n")
	findings, census := s.run(t)
	if len(findings) != 1 {
		t.Fatalf("findings=%d, ожидался ровно 1: %v", len(findings), findings)
	}
	got := findings[0].String()
	for _, want := range []string{"docs/grpc.mdx", "чегоНет", "CheckRequest", "gRPC"} {
		if !strings.Contains(got, want) {
			t.Errorf("находка не называет %q: %s", want, got)
		}
	}
	// Законное поле того же тела рассужено и НЕ объявлено находкой.
	if census.KeysJudged < 4 {
		t.Fatalf("ключей рассужено %d — законное поле рядом не рассуждено", census.KeysJudged)
	}
}

// TestBodyGate_GrpcurlUnknownServiceIsNotAFinding — законный близнец: служба вне
// регистра (соседний домен) находкой не является и уходит в перепись.
func TestBodyGate_GrpcurlUnknownServiceIsNotAFinding(t *testing.T) {
	s := newBodyStand(t)
	s.write(t, "docs/grpc.mdx",
		"```bash\ngrpcurl -plaintext -d '{\"чужое\":1}' \\\\\n"+
			"  localhost:9091 kacho.cloud.чужойдомен.v1.SomeService/Do\n```\n")
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("чужая служба объявлена находкой: %v", findings)
	}
	if census.BodiesUnmatched != 1 {
		t.Fatalf("несопоставленных тел %d, ожидалось 1 — слепая зона не посчитана",
			census.BodiesUnmatched)
	}
}

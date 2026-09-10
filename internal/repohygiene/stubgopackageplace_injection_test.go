// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// stubgopackageplace_injection_test.go — доказательство способности гейта падать
// И молчать.
//
// # Инъекций больше одной, потому что исходов текстовой правки ДВА
//
// Оба измерены опытом и разобраны на stubgopackageplace.go. Гейт обязан называть
// оба, и называть их РАЗНЫМИ находками: правка, изменившая длину, ломает
// дескриптор — это громкий исход, и его лечит регенерация; правка, которой не
// было вовсе, оставляет дескриптор целым и путь прежним — молчаливый исход, и
// его не видит больше никто.
//
// Порядок: контроль (всё цело — молчит) · инъекция расхождения пути · инъекция
// неразбираемого дескриптора · пустой обход · вход без дескрипторов. Законный
// близнец подан по каждой оси: стаб без дескриптора находкой быть НЕ должен, и
// путь модуля службы обязан судиться своим объявлением, а не корневым.

// descriptorBytes — настоящий сериализованный дескриптор с заданным
// `go_package`. Вход строится ПРОИЗВОДИТЕЛЕМ, а не выписанной строкой: гейт
// разбирает дескриптор, поэтому подделка обязана быть дескриптором.
func descriptorBytes(t *testing.T, name, goPackage string) []byte {
	t.Helper()
	fd := &descriptorpb.FileDescriptorProto{
		Name:    proto.String(name),
		Syntax:  proto.String("proto3"),
		Options: &descriptorpb.FileOptions{GoPackage: proto.String(goPackage)},
	}
	raw, err := proto.Marshal(fd)
	if err != nil {
		t.Fatalf("сборка дескриптора: %v", err)
	}
	return raw
}

func lawfulSites() []stubGoPackageSite {
	return []stubGoPackageSite{
		{
			File:     "pkg/api/corelib/authz/v1/authz_options.pb.go",
			Baked:    "github.com/PRO-Robotech/kacho/pkg/api/corelib/authz/v1",
			Expected: "github.com/PRO-Robotech/kacho/pkg/api/corelib/authz/v1",
			Module:   "github.com/PRO-Robotech/kacho",
		},
		{
			File:     "pkg/api/kacho/cloud/vpc/v1/network.pb.go",
			Baked:    "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1",
			Expected: "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1",
			Module:   "github.com/PRO-Robotech/kacho",
		},
	}
}

func TestStubGoPackageJudgeIsSilentWhenBakedPathMatchesPlace(t *testing.T) {
	t.Parallel()

	faults, census := judgeStubGoPackagePlace(lawfulSites(), nil, 3, 1)

	if len(faults) != 0 {
		t.Fatalf("контроль покраснел на согласованном входе (%d):\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	if census.WithDescriptor != 2 || census.WithoutDescriptor != 1 || census.FilesRead != 3 {
		t.Fatalf("перепись не назвала объём: %s", census.String())
	}
	if census.Modules != 1 {
		t.Fatalf("перепись не назвала модули: %s", census.String())
	}
}

// Инъекция ОДНОГО факта: стаб перенесён в другой модуль, дескриптор не
// перегенерирован. Остальное у входа цело.
func TestStubGoPackageJudgeCatchesStubRelocatedWithoutRegeneration(t *testing.T) {
	t.Parallel()

	sites := lawfulSites()
	sites[0].Expected = "github.com/PRO-Robotech/corelib/authz/v1"
	sites[0].Module = "github.com/PRO-Robotech/corelib"

	faults, census := judgeStubGoPackagePlace(sites, nil, 3, 1)

	if len(faults) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d:\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	if !strings.Contains(faults[0], "authz_options.pb.go") {
		t.Fatalf("находка не назвала файл: %s", faults[0])
	}
	if !strings.Contains(faults[0], "corelib/authz/v1") {
		t.Fatalf("находка не назвала ожидаемый путь: %s", faults[0])
	}
	if census.Modules != 2 {
		t.Fatalf("перепись не заметила второй модуль: %s", census.String())
	}
}

// Инъекция ОДНОГО факта: дескриптор не разбирается — ровно то, что производит
// текстовая правка `go_package` внутри байтов.
func TestStubGoPackageJudgeCatchesUnparsableDescriptor(t *testing.T) {
	t.Parallel()

	faults, census := judgeStubGoPackagePlace(lawfulSites(),
		[]string{"pkg/api/kacho/cloud/operation/operation.pb.go"}, 4, 1)

	if len(faults) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d:\n  %s",
			len(faults), strings.Join(faults, "\n  "))
	}
	if !strings.Contains(faults[0], "operation.pb.go") ||
		!strings.Contains(faults[0], "РЕГЕНЕРАЦИЕЙ") {
		t.Fatalf("находка не назвала ни файл, ни противоядие: %s", faults[0])
	}
	if census.Unparsable != 1 {
		t.Fatalf("перепись не назвала неразбираемые: %s", census.String())
	}
}

// Пустой обход — находка, а не зелёное.
func TestStubGoPackageJudgeRefusesAnEmptyWalk(t *testing.T) {
	t.Parallel()

	faults, census := judgeStubGoPackagePlace(nil, nil, 0, 0)

	if len(faults) == 0 {
		t.Fatal("пустой обход прошёл зелёным — «ноль расхождений» стало неотличимо " +
			"от «ноль прочитанного»")
	}
	if !strings.Contains(faults[0], "обход пуст") {
		t.Fatalf("находка не назвала предмет: %s", faults[0])
	}
	if census.FilesRead != 0 {
		t.Fatalf("перепись соврала об объёме: %s", census.String())
	}
}

// Стабы прочитаны, дескриптор не разобран НИ У ОДНОГО — извлечение литерала
// перестало работать. Это находка, иначе зелёное было бы вакуумным.
func TestStubGoPackageJudgeRefusesWhenNoDescriptorWasEverRead(t *testing.T) {
	t.Parallel()

	faults, census := judgeStubGoPackagePlace(nil, nil, 67, 67)

	if len(faults) == 0 {
		t.Fatal("вход из 67 стабов без единого дескриптора прошёл зелёным")
	}
	if !strings.Contains(faults[0], "НИ У ОДНОГО") {
		t.Fatalf("находка не назвала предмет: %s", faults[0])
	}
	if census.WithoutDescriptor != 67 {
		t.Fatalf("перепись не назвала бездескрипторные: %s", census.String())
	}
}

// Законный близнец добычи входа: настоящий литерал `rawDesc` в форме, которую
// эмитит protoc-gen-go, читается узлом разбора и разбирается как дескриптор.
func TestRawDescriptorLiteralReadsTheProtocEmittedForm(t *testing.T) {
	t.Parallel()

	raw := descriptorBytes(t, "corelib/authz/v1/authz_options.proto",
		"github.com/PRO-Robotech/kacho/pkg/api/corelib/authz/v1;authzv1")

	// Форма protoc-gen-go: `const … _rawDesc = "" + "…"`, плюс соседние
	// объявления, чьи имена суффиксу НЕ удовлетворяют.
	src := "package authzv1\n\nconst file_x_proto_rawDesc = \"\" +\n\t" +
		quoteGoString(string(raw[:len(raw)/2])) + " +\n\t" +
		quoteGoString(string(raw[len(raw)/2:])) + "\n\n" +
		"var file_x_proto_rawDescData []byte\n"

	got, ok, err := rawDescriptorLiteral([]byte(src), "x.pb.go")
	if err != nil {
		t.Fatalf("разбор синтетического стаба: %v", err)
	}
	if !ok {
		t.Fatal("литерал rawDesc не найден в форме, которую эмитит protoc-gen-go — " +
			"гейт объявил бы бездескрипторными ВСЕ стабы и остался зелёным")
	}
	gp, err := goPackageOfDescriptor(got)
	if err != nil {
		t.Fatalf("дескриптор собранного литерала не разбирается: %v", err)
	}
	if want := "github.com/PRO-Robotech/kacho/pkg/api/corelib/authz/v1"; gp != want {
		t.Fatalf("go_package прочитан как %q, ожидалось %q (суффикс `;alias` обязан быть снят)", gp, want)
	}
}

// Транспортный стаб дескриптора не несёт — и находкой быть не должен.
func TestRawDescriptorLiteralIsSilentOnATransportStub(t *testing.T) {
	t.Parallel()

	src := "package vpcv1\n\ntype NetworkServiceClient interface{}\n"

	_, ok, err := rawDescriptorLiteral([]byte(src), "network_service_grpc.pb.go")
	if err != nil {
		t.Fatalf("разбор транспортного стаба: %v", err)
	}
	if ok {
		t.Fatal("у транспортного стаба найден дескриптор, которого он не несёт")
	}
}

// Вложенное объявление модуля перебивает корневое.
func TestOwningModulePrefersTheNestedDeclaration(t *testing.T) {
	t.Parallel()

	modules := map[string]string{
		"":             "github.com/PRO-Robotech/kacho",
		"services/iam": "github.com/PRO-Robotech/kaname",
	}

	dir, mod := owningModule(modules, "services/iam/internal/x/y.pb.go")
	if mod != "github.com/PRO-Robotech/kaname" || dir != "services/iam" {
		t.Fatalf("файл службы приписан модулю %q (каталог %q) — вложенное объявление "+
			"не перебило корневое", mod, dir)
	}

	dir, mod = owningModule(modules, "pkg/api/corelib/authz/v1/x.pb.go")
	if mod != "github.com/PRO-Robotech/kacho" || dir != "" {
		t.Fatalf("файл платформы приписан модулю %q (каталог %q)", mod, dir)
	}

	if got := expectedImportPath("github.com/PRO-Robotech/kaname", "services/iam",
		"services/iam/internal/x/y.pb.go"); got != "github.com/PRO-Robotech/kaname/internal/x" {
		t.Fatalf("путь импорта выведен как %q", got)
	}
}

// quoteGoString — строковый литерал Go для произвольных байтов.
func quoteGoString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			b.WriteString("\\\"")
		case c == '\\':
			b.WriteString("\\\\")
		case c >= 0x20 && c < 0x7f:
			b.WriteByte(c)
		default:
			const hex = "0123456789abcdef"
			b.WriteString("\\x")
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0xf])
		}
	}
	b.WriteByte('"')
	return b.String()
}

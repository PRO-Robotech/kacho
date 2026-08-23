// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
)

// Гейт КЛАССА «принято-и-проигнорировано» (api-conventions.md): поле публичного
// запроса обязано иметь читателя в прод-коде своего сервиса. Три законных исхода,
// четвёртого нет — реализовать / отвергать явно / снять с контракта; молча
// принять и выбросить исходом не является.
//
// Гейт закрывает КЛАСС, а не экземпляры: новое поле в proto обязано попасть в
// одну из трёх полос, иначе сборка красная в тот же момент, когда поле появилось.
//
//	читается    — на поле есть ссылка в непроверочном дереве services/compute;
//	отвергается — handler отвечает INVALID_ARGUMENT и называет поле (ПРОВЕРЯЕТСЯ
//	              поведением, а не декларацией);
//	недостижимо — RPC, принимающий это сообщение, отдаёт Unimplemented, поэтому
//	              не принимает вообще ничего (ПРОВЕРЯЕТСЯ вызовом).
//
// ЧЕСТНАЯ ГРАНИЦА полосы «читается»: ссылка доказывает УПОМИНАНИЕ, а не влияние
// на поведение. Ровно на этом и держался разбираемый дефект — sshPublicKeys
// читался хендлером, ехал в DTO use-case и там исчезал, поэтому любая проверка
// «есть ли читатель» его пропускала. Значит эта полоса ловит только полностью
// неупомянутое поле; поля, чьё влияние оспаривается, переводятся в полосу
// «отвергается», где утверждение делается ПОВЕДЕНИЕМ. Гейт заявляет это
// ограничение вслух, вместо того чтобы выглядеть сильнее, чем он есть.

// rejectedRequestFields — поля, которые compute принимать отказывается. Каждая
// запись проверяется поведением (TestRequestFields_RejectedOnesReallyRefuse),
// поэтому список не может «протухнуть в зелёном»: исчезнет поле из proto — запись
// станет находкой (исключение обязано истекать само).
var rejectedRequestFields = map[protoreflect.FullName][]string{
	"kacho.cloud.compute.v1.CreateInstanceRequest": {
		"network_settings", "filesystem_specs", "local_disk_specs",
		"maintenance_policy", "maintenance_grace_period", "serial_port_settings",
		"ssh_public_keys",
	},
	"kacho.cloud.compute.v1.UpdateInstanceRequest": {
		"ssh_public_keys", "network_settings",
		"maintenance_policy", "maintenance_grace_period", "serial_port_settings",
	},
	"kacho.cloud.compute.v1.GetInstanceSerialPortOutputRequest": {"port"},
}

// classifiedServices — сервисы, чью входную поверхность обходит гейт. Совпадает с
// тем, что composition root поднимает на листенерах.
var classifiedServices = []grpc.ServiceDesc{
	computev1.InstanceService_ServiceDesc,
	computev1.MachineTypeService_ServiceDesc,
	computev1.InternalMachineTypeService_ServiceDesc,
}

// rejectionHelperPrefix — префикс функций, которые ОТВЕРГАЮТ поле. Их тела
// вырезаются из просматриваемого кода перед поиском ссылок, иначе гейт
// самообманывается: `req.GetSshPublicKeys()` внутри отказа выглядит как чтение,
// и поле, снятое из полосы «отвергается», молча переезжает в полосу «читается».
// Проверено инъекцией — без вырезания гейт оставался зелёным на реальном дефекте.
const rejectionHelperPrefix = "RejectUnsupported"

func moduleRootForFields(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	require.NoError(t, err)
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqualf(t, parent, dir, "go.mod not found above %s", dir)
		dir = parent
	}
}

// computeNonTestSources — непроверочный Go-код сервиса, склеенный для поиска
// ссылок, БЕЗ тел функций-отказов. Пустой результат роняет гейт: «ноль
// нарушений» обязано быть отличимо от «ноль прочитанного».
func computeNonTestSources(t *testing.T) string {
	t.Helper()
	root := filepath.Join(moduleRootForFields(t), "services", "compute")
	var b strings.Builder
	visited := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, readErr := os.ReadFile(path) // #nosec G304 -- test walks its own module tree
		if readErr != nil {
			return readErr
		}
		visited++
		b.Write(stripRejectionHelpers(t, path, body))
		b.WriteByte('\n')
		return nil
	})
	require.NoError(t, err)
	require.NotZerof(t, visited, "walked %s and read no non-test Go file — the scan read nothing", root)
	return b.String()
}

// stripRejectionHelpers вырезает тела функций `RejectUnsupported*` из исходника.
// Разбор AST, а не текста: границы тела берутся из синтаксиса, поэтому строковый
// литерал или комментарий со словом «func» их не сдвинет.
func stripRejectionHelpers(t *testing.T, path string, src []byte) []byte {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	require.NoErrorf(t, err, "parse %s", path)

	out := append([]byte(nil), src...)
	base := fset.File(f.Pos()).Base()
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, rejectionHelperPrefix) {
			continue
		}
		from, to := int(fn.Body.Pos())-base, int(fn.Body.End())-base
		if from < 0 || to > len(out) || from >= to {
			continue
		}
		for i := from; i < to; i++ {
			if out[i] != '\n' {
				out[i] = ' '
			}
		}
	}
	return out
}

// goFieldName — имя поля в сгенерированном Go-типе (protoc-gen-go camel-case).
func goFieldName(protoName protoreflect.Name) string {
	parts := strings.Split(string(protoName), "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

// fieldIsReferenced — упоминается ли поле в непроверочном коде сервиса: через
// сгенерированный геттер либо через прямой доступ к полю proto-переменной (`req.`
// / `r.` — единственные имена, которыми хендлеры compute держат запрос). Сужение
// до этих двух форм намеренное: `.Field` без квалификатора совпал бы с
// одноимённым полем доменной структуры и объявил бы прочитанным то, что никто не
// читает.
func fieldIsReferenced(blob string, f protoreflect.FieldDescriptor) bool {
	g := regexp.QuoteMeta(goFieldName(f.Name()))
	for _, pat := range []string{`\.Get` + g + `\(\)`, `\breq\.` + g + `\b`, `\br\.` + g + `\b`} {
		if regexp.MustCompile(pat).MatchString(blob) {
			return true
		}
	}
	return false
}

type requestEntry struct {
	md  protoreflect.MessageDescriptor
	rpc string
}

// requestMessages — входные сообщения всех RPC обойдённых сервисов.
func requestMessages(t *testing.T) map[protoreflect.FullName]requestEntry {
	t.Helper()
	out := map[protoreflect.FullName]requestEntry{}
	for _, sd := range classifiedServices {
		d, derr := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(sd.ServiceName))
		require.NoErrorf(t, derr, "service descriptor for %s", sd.ServiceName)
		desc, ok := d.(protoreflect.ServiceDescriptor)
		require.Truef(t, ok, "%s is not a service descriptor", sd.ServiceName)
		for i := 0; i < desc.Methods().Len(); i++ {
			m := desc.Methods().Get(i)
			out[m.Input().FullName()] = requestEntry{md: m.Input(), rpc: string(m.Name())}
		}
	}
	require.NotEmpty(t, out, "no request message resolved — the walk found nothing")
	return out
}

// TestRequestFields_EveryFieldIsClassified — исчерпывающая классификация: каждое
// поле каждого входного сообщения выставленной поверхности лежит ровно в одной из
// трёх полос. Поле, не попавшее никуда, роняет гейт: именно так «принято и
// выброшено» и появляется, ведь добавить поле в proto дешевле, чем реализовать.
func TestRequestFields_EveryFieldIsClassified(t *testing.T) {
	blob := computeNonTestSources(t)
	msgs := requestMessages(t)

	var unclassified []string
	for name, entry := range msgs {
		rejected := map[string]struct{}{}
		for _, f := range rejectedRequestFields[name] {
			rejected[f] = struct{}{}
		}
		fields := entry.md.Fields()
		for i := 0; i < fields.Len(); i++ {
			f := fields.Get(i)
			if _, ok := rejected[string(f.Name())]; ok {
				continue
			}
			if fieldIsReferenced(blob, f) {
				continue
			}
			if rpcIsUnimplemented(t, entry.rpc, name) {
				continue
			}
			unclassified = append(unclassified,
				string(name.Name())+"."+string(f.Name())+" (rpc "+entry.rpc+")")
		}
	}
	sort.Strings(unclassified)

	require.Emptyf(t, unclassified,
		"%d request field(s) are accepted by a live RPC and read by nothing: %v\n"+
			"Accepted-and-ignored is not one of the three lawful outcomes (api-conventions.md).\n"+
			"Implement the field, add it to rejectedRequestFields (and make the handler refuse it "+
			"naming the field), or take it off the contract with reserved number AND name.",
		len(unclassified), unclassified)
}

// TestRequestFields_RejectedOnesReallyRefuse — вторая половина: заявление
// «отвергается» подтверждается ПОВЕДЕНИЕМ. Иначе список стал бы декларацией, а не
// гейтом, — той самой формой без содержания. Сверяется с наборами поведенческих
// кейсов, каждый из которых поднимает хендлер и утверждает INVALID_ARGUMENT +
// имя поля в деталях, — чтобы новая запись не могла попасть в объявление, минуя
// поведенческую проверку.
func TestRequestFields_RejectedOnesReallyRefuse(t *testing.T) {
	covered := map[protoreflect.FullName]map[string]struct{}{
		"kacho.cloud.compute.v1.CreateInstanceRequest":              {"ssh_public_keys": {}},
		"kacho.cloud.compute.v1.UpdateInstanceRequest":              {},
		"kacho.cloud.compute.v1.GetInstanceSerialPortOutputRequest": {"port": {}},
	}
	for _, tc := range unsupportedCreateFieldCases {
		covered["kacho.cloud.compute.v1.CreateInstanceRequest"][tc.field] = struct{}{}
	}
	for _, tc := range updateDroppedFieldCases {
		covered["kacho.cloud.compute.v1.UpdateInstanceRequest"][tc.field] = struct{}{}
	}

	for msg, declared := range rejectedRequestFields {
		for _, f := range declared {
			require.Containsf(t, covered[msg], f,
				"%s.%s is declared rejected but no behavioural case proves the handler refuses it "+
					"and names the field", msg, f)
		}
	}
}

// TestRequestFields_NoStaleRejectionEntry — исключение живёт, пока у него есть
// предмет: запись про поле, которого в контракте больше нет, роняет гейт, а не
// доживает безобидным остатком (её унаследует следующая слепая зона).
func TestRequestFields_NoStaleRejectionEntry(t *testing.T) {
	msgs := requestMessages(t)
	for msg, declared := range rejectedRequestFields {
		entry, ok := msgs[msg]
		require.Truef(t, ok, "rejectedRequestFields names %s, which no served RPC accepts", msg)
		for _, f := range declared {
			require.NotNilf(t, entry.md.Fields().ByName(protoreflect.Name(f)),
				"rejectedRequestFields still excludes %s.%s, but the field is gone from the contract", msg, f)
		}
	}
}

// rpcIsUnimplemented — отвечает ли RPC codes.Unimplemented. Утверждение делается
// ВЫЗОВОМ: «этот RPC не реализован» — свойство поведения, а не комментария рядом
// с ним. Имена RPC и Go-методов совпадают by construction протогенератора.
func rpcIsUnimplemented(t *testing.T, rpc string, msg protoreflect.FullName) bool {
	t.Helper()
	m := reflect.ValueOf(&InstanceHandler{}).MethodByName(rpc)
	if !m.IsValid() {
		return false
	}
	mt, err := protoregistry.GlobalTypes.FindMessageByName(msg)
	if err != nil {
		return false
	}
	res := m.Call([]reflect.Value{reflect.ValueOf(context.Background()), reflect.ValueOf(mt.New().Interface())})
	if len(res) != 2 {
		return false
	}
	callErr, _ := res[1].Interface().(error)
	return status.Code(callErr) == codes.Unimplemented
}

// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// parity_test.go — состав AllowedMethods ВЫЧИСЛЯЕТСЯ из дескрипторов, а не
// перечисляется руками.
//
// ЗАЧЕМ. AllowedMethods — рукописная карта, и до этого файла КАЖДОЕ утверждение
// о ней тоже было рукописным: пять тестовых файлов пакета перечисляли методы
// поимённо и ни один не считал разность «публичное в дескрипторах» минус
// «есть в списке». Поэтому список молча отставал от контракта: новый публичный
// RPC появлялся в proto, получал REST-биндинг — и оставался недостижим по
// нативному gRPC, потому что добавить его в этот файл забыли.
//
// ОГОВОРКА ПРО REST, КОТОРАЯ ЗДЕСЬ СТОЯЛА. Прежняя редакция объясняла, что
// отставание жило только на gRPC-полосе, «потому что REST выводится из
// дескрипторов и отстать не может». Это верно ПОМЕТОДНО и неверно ПОСЕРВИСНО:
// из дескрипторов берутся методы ВНУТРИ сервиса, а сама регистрация сервиса —
// рукописная строка в restmux/mux.go, по одной на сервис, часть из них ещё и
// условная по адресу домена. Класс на той полосе не гипотетический: iam
// ConditionsService был в дескрипторах, в таблице маршрутов и в каталоге прав,
// и не был смонтирован — все шесть его REST-путей отвечали 404, пока это не
// починили руками (a96dbe1a). Теперь обе полосы вычисляются:
// restmux/public_binding_routability_test.go считает ту же разность по REST.
//
// ЧЕГО ЭТОТ ФАЙЛ НЕ ДОКАЗЫВАЕТ. «Путь в списке» и «путь маршрутизируется» —
// разные факты: резолвер отказывает ещё и когда в карте открытых соединений нет
// ключа домена, а карту строит composition root из отдельного рукописного
// набора ключей. Эту разность считает cmd/api-gateway/route_wiring_parity_test.go.
//
// ПОЧЕМУ ОТСТАВАНИЕ НЕ БЫЛО ВИДНО. Отказ резолвера (proxy/server.go) —
// NotFound «unknown method», БАЙТ-В-БАЙТ тот же, которым внешний листенер
// отвечает за Internal*-методы (proxy/route_refusal.go, и неразличимость там
// заявлена целью). То есть забытая регистрация выглядит снаружи ровно как
// намеренно скрытая admin-поверхность: клиент их не различает, и отставание
// не проявляется ни в одном наблюдаемом симптоме, кроме «RPC не работает».
//
// ЧТО ЗДЕСЬ ПРОВЕРЯЕТСЯ — три разности, каждая в обе стороны:
//
//  1. публичное-в-дескрипторах \ AllowedMethods = ∅ — ничего не забыто;
//  2. AllowedMethods \ все-методы-в-дескрипторах = ∅ — в списке нет записей,
//     за которыми не стоит ни одного RPC (это же утверждение делает
//     ИЗЛИШНИМИ рукописные негативы про снятые с контракта пути: любой такой
//     путь, попав в список, краснеет здесь);
//  3. AllowedMethods ∩ Internal* = ∅ — запрет #6 на всей реальной популяции
//     Internal-методов, а не на выборке имён.
//
// ПРЕДПОСЫЛКА ГЕЙТА И ОБЪЁМ ОСМОТРЕННОГО. Дескрипторы видны только для тех
// пакетов, что слинкованы в тестовый бинарь (blank-импорты ниже). Значит
// «ноль находок» здесь неотличимо от «ноль прочитанного» — если кто-то уберёт
// импорт, гейт замолчит. Поэтому население сверяется с ВНЕШНИМ фактом:
// множество .proto-файлов на диске под proto/kacho/cloud. Пропавший импорт и
// новый домен без импорта дают красный с именем файла. Перепись печатается
// числом отдельным утверждением.
package allowlist_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/PRO-Robotech/kacho/gateway/internal/allowlist"

	// Население гейта. Каждый blank-импорт регистрирует дескрипторы своего
	// домена в protoregistry.GlobalFiles. Список сверяется с диском в
	// TestAllowlist_CensusCoversEveryProtoFile — новый домен без импорта тут
	// краснеет там.
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/api"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/reference"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
)

// routableDescriptorPrefix — резолвер маршрутизирует ТОЛЬКО этот префикс
// (proxy/server.go: `strings.HasPrefix(method, "/kacho.cloud.")`), поэтому и
// население гейта ограничено им же. Файл дескриптора несёт путь без ведущего
// "proto/": "kacho/cloud/vpc/v1/network_service.proto".
const (
	routableFilePrefix = "kacho/cloud/"
	routablePkgPrefix  = "/kacho.cloud."
	protoDirOnDisk     = "../../../proto/" + routableFilePrefix
)

// descriptorSurface — то, что дескрипторы говорят о маршрутизируемой
// поверхности: пути файлов (для переписи) и полные пути методов, разделённые
// по признаку Internal.
type descriptorSurface struct {
	files    map[string]struct{}
	public   map[string]struct{}
	internal map[string]struct{}
	services int
}

// readDescriptorSurface собирает поверхность из глобального реестра.
func readDescriptorSurface(t *testing.T) descriptorSurface {
	t.Helper()
	s := descriptorSurface{
		files:    map[string]struct{}{},
		public:   map[string]struct{}{},
		internal: map[string]struct{}{},
	}
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		path := string(fd.Path())
		if !strings.HasPrefix(path, routableFilePrefix) {
			return true
		}
		s.files[path] = struct{}{}
		svcs := fd.Services()
		for i := 0; i < svcs.Len(); i++ {
			svc := svcs.Get(i)
			s.services++
			methods := svc.Methods()
			for j := 0; j < methods.Len(); j++ {
				full := "/" + string(svc.FullName()) + "/" + string(methods.Get(j).Name())
				if !strings.HasPrefix(full, routablePkgPrefix) {
					continue
				}
				if allowlist.HasInternalSuffix(full) {
					s.internal[full] = struct{}{}
				} else {
					s.public[full] = struct{}{}
				}
			}
		}
		return true
	})
	return s
}

// protoFilesOnDisk — внешний факт, которым проверяется предпосылка гейта.
// Он не зависит ни от одного импорта: если blank-импорт убрали, здесь файл
// всё равно есть, а в реестре его уже нет.
func protoFilesOnDisk(t *testing.T) map[string]struct{} {
	t.Helper()
	out := map[string]struct{}{}
	root := filepath.Clean(protoDirOnDisk)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".proto") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		out[routableFilePrefix+filepath.ToSlash(rel)] = struct{}{}
		return nil
	})
	if err != nil {
		t.Fatalf("обход %s: %v — гейт не может утверждать объём осмотренного", root, err)
	}
	return out
}

func sortedDiff(a, b map[string]struct{}) []string {
	var out []string
	for k := range a {
		if _, ok := b[k]; !ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// Минимальные полы переписи. Они не «примерно про размер контракта» — они про
// то, что гейт вообще что-то прочитал: пустой реестр или несуществующая
// директория дают ноль и обязаны краснеть, а не сходиться в «ноль находок».
const (
	minProtoFiles = 80
	minServices   = 40
	minMethods    = 200
)

// TestAllowlist_CensusCoversEveryProtoFile — предпосылка гейта: population,
// которую видят проверки ниже, совпадает с .proto-файлами на диске.
//
// Это утверждение об ОБЪЁМЕ ОСМОТРЕННОГО. Без него «разность пуста» ниже
// означало бы «мы ничего не читали» ровно так же убедительно, как «всё
// сошлось».
func TestAllowlist_CensusCoversEveryProtoFile(t *testing.T) {
	surface := readDescriptorSurface(t)
	disk := protoFilesOnDisk(t)

	t.Logf("перепись: %d .proto на диске, %d в дескрипторах, %d сервисов, "+
		"%d публичных методов, %d Internal-методов, %d записей в AllowedMethods",
		len(disk), len(surface.files), surface.services,
		len(surface.public), len(surface.internal), len(allowlist.AllowedMethods))

	if len(disk) < minProtoFiles {
		t.Fatalf("на диске под %s найдено %d .proto (< %d) — гейт читает не то дерево; "+
			"пока это не починено, любой его зелёный вердикт беспредметен",
			protoDirOnDisk, len(disk), minProtoFiles)
	}
	if surface.services < minServices {
		t.Fatalf("в дескрипторах %d сервисов (< %d) — реестр пуст или домены не слинкованы",
			surface.services, minServices)
	}
	if n := len(surface.public) + len(surface.internal); n < minMethods {
		t.Fatalf("в дескрипторах %d методов (< %d) — реестр пуст или домены не слинкованы",
			n, minMethods)
	}

	if missing := sortedDiff(disk, surface.files); len(missing) > 0 {
		t.Errorf("%d .proto лежат на диске, но их дескрипторов нет в тестовом бинаре — "+
			"добавь blank-импорт сгенерированного пакета в parity_test.go, иначе "+
			"эти сервисы не проверяются НИЧЕМ:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
	if extra := sortedDiff(surface.files, disk); len(extra) > 0 {
		t.Errorf("%d дескрипторов не имеют .proto на диске под %s — перепись читает не то дерево:\n  %s",
			len(extra), protoDirOnDisk, strings.Join(extra, "\n  "))
	}
}

// TestAllowlist_EveryPublicRPCIsRoutable — положительная сторона: каждый
// публичный RPC дескрипторов маршрутизируется нативным gRPC.
//
// Отсутствие записи НЕ выглядит как ошибка конфигурации: резолвер отвечает тем
// же NotFound, что и за скрытую admin-поверхность, поэтому недостающая
// регистрация неотличима от намеренного сокрытия и живёт сколько угодно долго.
func TestAllowlist_EveryPublicRPCIsRoutable(t *testing.T) {
	surface := readDescriptorSurface(t)
	if len(surface.public) < minMethods/2 {
		t.Fatalf("публичных методов в дескрипторах %d — слишком мало, чтобы вердикт что-то значил",
			len(surface.public))
	}
	missing := sortedDiff(surface.public, allowlist.AllowedMethods)
	if len(missing) > 0 {
		t.Errorf("%d публичных RPC из %d не маршрутизируются нативным gRPC (нет в AllowedMethods) "+
			"при том, что их REST-биндинг выводится из тех же дескрипторов и работает:\n  %s",
			len(missing), len(surface.public), strings.Join(missing, "\n  "))
	}
}

// TestAllowlist_NoEntryWithoutAnRPC — обратная сторона: за каждой записью
// списка стоит настоящий RPC.
//
// Это утверждение заменяет рукописные негативы про снятые с контракта пути
// (старый resourcemanager.v1, operation.v1, 0.x-глаголы): перечислять их
// поимённо больше не нужно — здесь краснеет ЛЮБОЙ путь, за которым нет RPC,
// включая те, которых никто не предвидел.
func TestAllowlist_NoEntryWithoutAnRPC(t *testing.T) {
	surface := readDescriptorSurface(t)
	all := map[string]struct{}{}
	for k := range surface.public {
		all[k] = struct{}{}
	}
	for k := range surface.internal {
		all[k] = struct{}{}
	}
	if len(all) < minMethods {
		t.Fatalf("в дескрипторах %d методов — вердикт беспредметен", len(all))
	}
	if extra := sortedDiff(allowlist.AllowedMethods, all); len(extra) > 0 {
		t.Errorf("%d записей AllowedMethods не соответствуют ни одному RPC в дескрипторах "+
			"(опечатка в пути, снятый с контракта метод или переехавший пакет — "+
			"такая запись ничего не разрешает и ничего не запрещает):\n  %s",
			len(extra), strings.Join(extra, "\n  "))
	}
}

// TestAllowlist_NoInternalRPCIsRoutable — запрет #6 на ВСЕЙ популяции
// Internal-методов, а не на выборке имён.
func TestAllowlist_NoInternalRPCIsRoutable(t *testing.T) {
	surface := readDescriptorSurface(t)
	if len(surface.internal) == 0 {
		t.Fatal("в дескрипторах ноль Internal-методов — предикат HasInternalSuffix перестал их " +
			"опознавать, и запрет #6 больше ничем не проверяется")
	}
	var leaked []string
	for m := range surface.internal {
		if allowlist.IsAllowed(m) {
			leaked = append(leaked, m)
		}
	}
	sort.Strings(leaked)
	if len(leaked) > 0 {
		t.Errorf("%d Internal-методов из %d попали в AllowedMethods (запрет #6):\n  %s",
			len(leaked), len(surface.internal), strings.Join(leaked, "\n  "))
	}
}

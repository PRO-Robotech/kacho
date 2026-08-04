// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// rootedwalk_test.go — гейты этого пакета читают дерево репозитория обходом
// диска. Здесь заперто свойство: файл, лежащий ЗА пределами осматриваемого
// каталога, в вердикт не попадает.
//
// # Почему проба про симлинк, а не про гонку
//
// Предмет — обращение к файлу по пути, полученному от обхода: между тем, как
// обход снял с записи тип, и тем, как колбэк открыл её по имени, запись может
// перестать быть тем, чем была. Саму гонку детерминированно не воспроизвести,
// а её наблюдаемое следствие — да: путь, ведущий наружу, читается вместо того,
// чтобы быть отвергнутым. Корневой обход (`os.Root`) закрывает и то и другое
// одним механизмом — разрешение имени не выходит за корень, — поэтому проба
// утверждает именно отказ на выход за корень.
//
// # Почему отказ, а не тихий пропуск
//
// Гейт, встретивший запись, которую он не может честно прочесть, обязан
// остановиться. Пропустить её молча значило бы отдать «ноль находок» на
// «ноль прочитанного» — то, ради чего в этом дереве заведена перепись
// осмотренного.
//
// Отслеживаемых симлинков в репозитории НЕТ (`git ls-files -s`, режим 120000 —
// ноль записей на 9b4dac0c), поэтому отказ не может сработать на законном
// содержимом дерева.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// outsideFile кладёт файл в каталог, ЗАВЕДОМО лежащий вне осматриваемого корня,
// и возвращает его путь.
func outsideFile(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("подготовка файла вне корня: %v", err)
	}
	return p
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("файловая система не поддерживает симлинки: %v", err)
	}
}

// TestRetiredRPCSurface_DoesNotReadThroughASymlinkOutOfTheProtoRoot — контракт
// снят с ДЕРЕВА КОНТРАКТА: имя, объявленное в файле за пределами корня, не может
// ни породить находку, ни быть засчитанным в перепись.
//
// Положительный контроль стоит рядом и в том же тесте: без симлинка тот же
// анализатор на том же дереве отрабатывает и находит подложенное имя. Без него
// отрицание зеленело бы на анализаторе, который не работает вовсе.
func TestRetiredRPCSurface_DoesNotReadThroughASymlinkOutOfTheProtoRoot(t *testing.T) {
	const ghost = "kacho.cloud.demo.v1.AlphaService/Vanish"
	dead := RetiredRPC{FQN: ghost, Reason: "снято в тесте"}

	// Каталог обязан быть непуст (иначе третье плечо гейта беспредметно и он
	// отказывает), но перечисленное в нём имя — НЕ снятое: иначе находку давало бы
	// плечо каталога, и проба про обход дерева перестала бы что-либо различать.
	catalog := []string{"kacho.cloud.demo.v1.BetaService/Pong"}

	// ── положительный контроль: анализатор способен увидеть это имя ──────────
	live := retiredTinyTree(t, retiredTinyProto, retiredTinyStubs, catalog)
	// Имя ghost принадлежит AlphaService — объявим его прямо в контракте дерева.
	inTree := filepath.Join(live, "proto", "kacho", "cloud", "demo", "v1", "extra.proto")
	if err := os.WriteFile(inTree, []byte(`syntax = "proto3";
package kacho.cloud.demo.v1;
service AlphaService {
  rpc Vanish (Req) returns (Res);
}
`), 0o600); err != nil {
		t.Fatalf("подготовка контроля: %v", err)
	}
	got, _, err := AuditRetiredRPCSurface(retiredTinyOptions(live, dead), nil)
	if err != nil {
		t.Fatalf("положительный контроль: анализатор не отработал: %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("положительный контроль: снятое имя, объявленное ВНУТРИ дерева, не найдено — "+
			"проба ниже зеленела бы на неработающем анализаторе (перечень: %v)", got)
	}

	// ── предмет: то же имя, но объявленное ЗА корнем и втянутое симлинком ────
	root := retiredTinyTree(t, retiredTinyProto, retiredTinyStubs, catalog)
	leak := outsideFile(t, "leaked.proto", `syntax = "proto3";
package kacho.cloud.demo.v1;
service AlphaService {
  rpc Vanish (Req) returns (Res);
}
`)
	symlink(t, leak, filepath.Join(root, "proto", "kacho", "cloud", "demo", "v1", "leaked.proto"))

	findings, census, err := AuditRetiredRPCSurface(retiredTinyOptions(root, dead), nil)
	for _, f := range findings {
		if f.FQN == ghost {
			t.Errorf("находка получена из файла ЗА пределами %q: %s\n"+
				"Обход прочитал содержимое по симлинку, ведущему наружу, — вердикт гейта "+
				"стал свойством постороннего файла, а не осматриваемого дерева.",
				"proto", f.String())
		}
	}
	if err == nil {
		t.Errorf("запись, ведущая за пределы осматриваемого каталога, прочитана молча "+
			"(ошибки нет, файлов в переписи %d). Гейт обязан ОТКАЗАТЬ: запись, которую он "+
			"не может честно прочесть, нельзя ни засчитать, ни пропустить — иначе «ноль "+
			"находок» становится неотличимо от «ноль прочитанного».", census.ProtoFiles)
	} else if !strings.Contains(err.Error(), "leaked.proto") {
		t.Errorf("отказ не называет запись, на которой он произошёл: %v", err)
	}
}

// TestGRPCMountParity_DoesNotReadThroughASymlinkOutOfTheAPIRoot — то же свойство
// на втором обходе пакета: дерево СТАБОВ.
//
// Здесь следствие чтения наружу нагляднее: подложенный стаб объявляет сервис,
// которого в дереве нет, и гейт паритета сообщает о нём как о неподнятом — то
// есть требует «починить» то, чего в репозитории не существует.
func TestGRPCMountParity_DoesNotReadThroughASymlinkOutOfTheAPIRoot(t *testing.T) {
	// ── положительный контроль: на дереве без симлинка гейт молчит ───────────
	if f, c, err := AuditGRPCMountParity(tinyOptions(tinyTree(t, true, "")), nil); err != nil || len(f) != 0 {
		t.Fatalf("положительный контроль: ожидались ноль находок и отсутствие ошибки, "+
			"получено findings=%v err=%v (прочитано стабов %d)", f, err, c.StubFiles)
	}

	root := tinyTree(t, true, "")
	leak := outsideFile(t, "ghost_grpc.pb.go", `package demov1

var GhostService_ServiceDesc = struct{ ServiceName string }{ServiceName: "kacho.cloud.demo.v1.GhostService"}
`)
	symlink(t, leak, filepath.Join(root, "pkg", "api", "kacho", "cloud", "demo", "v1", "ghost_grpc.pb.go"))

	findings, census, err := AuditGRPCMountParity(tinyOptions(root), nil)
	for _, f := range findings {
		if strings.Contains(f.FQN, "GhostService") {
			t.Errorf("гейт сообщает о сервисе, объявленном ЗА пределами дерева стабов: %s\n"+
				"Обход прочитал файл по симлинку наружу — находка требует поднять сервис, "+
				"которого в репозитории нет.", f.String())
		}
	}
	if err == nil {
		t.Errorf("запись, ведущая за пределы дерева стабов, прочитана молча (ошибки нет, "+
			"стабов в переписи %d). Гейт обязан ОТКАЗАТЬ, а не читать наружу.", census.StubFiles)
	} else if !strings.Contains(err.Error(), "ghost_grpc.pb.go") {
		t.Errorf("отказ не называет запись, на которой он произошёл: %v", err)
	}
}

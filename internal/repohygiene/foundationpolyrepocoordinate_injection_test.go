// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// foundationpolyrepocoordinate_injection_test.go — доказательство того, что
// проверка прозы фундамента СПОСОБНА упасть, и того, что она молчит на законных
// близнецах.
//
// # Почему синтетический корень, а не правка дерева
//
// Проверка читает дерево, которое читают и соседние сессии. Внести в него дефект
// ради доказательства значило бы править общее состояние. Разбор вынесен в
// чистую функцию над ПРОИЗВОЛЬНЫМ корнем, и сюда подаётся корень, собранный в
// каталоге прогона.
//
// # Каждая инъекция меняет ровно один факт против контроля
//
// Контроль стоит первым и обязан МОЛЧАТЬ. Ось «краснеет» одна — она и есть
// предмет. Осей «молчит» пять, и у каждой полосы стоит ПАРА: «в полосе — молчит»
// и «та же строка вне полосы — находка». Без второй половины пропуск был бы
// маской, а не полосой.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// fpcRootWith собирает корень из перечисленных файлов. Файлов ровно столько,
// сколько подано: перепись тогда прямо называет, что прочитано ровно то, что
// подано.
func fpcRootWith(t *testing.T, files map[string]string) *treecorpus.Tree {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("каталог %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("файл %s: %v", rel, err)
		}
	}
	tree, err := treecorpus.SyntheticTree(root)
	if err != nil {
		t.Fatalf("состав синтетического корня не собран — инъекция беспредметна: %v", err)
	}
	return tree
}

// fpcSoundTree — законный близнец: проза фундамента называет координаты ЭТОГО
// дерева. Проверка обязана молчать.
func fpcSoundTree() map[string]string {
	return map[string]string{
		"pkg/authz/doc.go": "" +
			"// Реализация порта живёт в `pkg/authz/authziam`.\n" +
			"package authz\n",
		"pkg/authz/authziam/check.go": "package authziam\n",
	}
}

// ── Контроль: годный корень молчит ──────────────────────────────────────────

func TestFpcInjectionControl_SoundFoundationIsSilent(t *testing.T) {
	t.Parallel()
	census, findings, err := scanFoundationProse(fpcRootWith(t, fpcSoundTree()))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("годный фундамент объявлен нарушением: проверка ловит форму, а не существо: %v", findings)
	}
	if census.filesRead != 2 {
		t.Fatalf("контроль беспредметен: прочитано %d файлов вместо 2", census.filesRead)
	}
	if census.coordinates != 0 || census.bareNames != 0 {
		t.Fatalf("контроль беспредметен: распознано лишнее (%s)", census)
	}
}

// ── Ось «краснеет»: мёртвая полирепо-координата — НАХОДКА с координатой ─────

func TestFpcInjection_DeadPolyrepoCoordinateIsFound(t *testing.T) {
	t.Parallel()
	files := fpcSoundTree()
	files["pkg/authz/check_client.go"] = "" +
		"// Реализация — клиентский adapter `kacho-vpc/internal/clients/iam_authz_client.go`.\n" +
		"package authz\n"
	census, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("мёртвая координата не найдена — проверка вакуумна: %v (%s)", findings, census)
	}
	got := findings[0].String()
	for _, want := range []string{"pkg/authz/check_client.go:1", "kacho-vpc/internal/clients/iam_authz_client.go"} {
		if !strings.Contains(got, want) {
			t.Fatalf("находка не называет %q — читатель ищет координату глазами: %s", want, got)
		}
	}
}

// ── Молчит: та же форма, но координата РЕЗОЛВИТСЯ ───────────────────────────
//
// Проза пишет координату от корня СЛУЖБЫ, а не репозитория, поэтому совпадение
// ищется суффиксом. Без этой оси гейт краснел бы на живом адресе.

func TestFpcInjection_ResolvingCoordinateStaysSilent(t *testing.T) {
	t.Parallel()
	files := fpcSoundTree()
	files["pkg/authz/edge.go"] = "// страж посадки объявлен в `kacho-geo/serve.go`\npackage authz\n"
	files["services/geo/cmd/kacho-geo/serve.go"] = "package main\n"
	census, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("живая координата объявлена мёртвой: %v", findings)
	}
	if census.coordinates != 1 || census.resolved != 1 {
		t.Fatalf("резолв не сосчитан: %s", census)
	}
}

func TestFpcInjection_TheSameCoordinateWithoutItsFileIsFound(t *testing.T) {
	t.Parallel()
	files := fpcSoundTree()
	files["pkg/authz/edge.go"] = "// страж посадки объявлен в `kacho-geo/serve.go`\npackage authz\n"
	_, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("без своего файла та же координата обязана быть находкой — иначе резолв есть маска: %v", findings)
	}
}

// ── Полоса КОНТРАКТА: судится `.proto` под `proto/`, и только он ───────────
//
// Пара обязательна. Первая половина доказывает, что расширение популяции не
// вакуумно: дефект в контракте краснеет и называет координату. Вторая — что
// расширен ВИД файла, а не каталог целиком: соседний файл того же каталога, но
// другого вида, остаётся вне популяции, и его молчание не есть молчание
// сломанного обхода (оно подтверждено числом прочитанных).

func TestFpcInjection_DeadCoordinateInTheContractIsFound(t *testing.T) {
	t.Parallel()
	files := fpcSoundTree()
	files["proto/kacho/cloud/registry/v1/registry.proto"] = "" +
		"// ID реестра. Prefix \"reg\" (kacho-corelib/ids.NewID).\n" +
		"message Registry {}\n"
	census, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("мёртвая координата контракта не найдена — расширение популяции вакуумно: "+
			"%v (%s)", findings, census)
	}
	got := findings[0].String()
	for _, want := range []string{"proto/kacho/cloud/registry/v1/registry.proto:1", "kacho-corelib/ids.NewID"} {
		if !strings.Contains(got, want) {
			t.Fatalf("находка не называет %q: %s", want, got)
		}
	}
}

func TestFpcInjection_TheSameLineInANonContractFileOfTheContractDirStaysSilent(t *testing.T) {
	t.Parallel()
	files := fpcSoundTree()
	files["proto/README.md"] = "ID реестра. Prefix `reg` (kacho-corelib/ids.NewID).\n"
	census, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("файл вне вида популяции объявлен находкой — расширен каталог, а не вид: %v", findings)
	}
	if census.filesRead != 2 {
		t.Fatalf("молчание не объяснено: прочитано %d файлов вместо 2 контроля, то есть "+
			"обход мог не состояться вовсе: %s", census.filesRead, census)
	}
}

func TestFpcInjection_TheSameContractLineNamingTheCurrentPlaceStaysSilent(t *testing.T) {
	t.Parallel()
	files := fpcSoundTree()
	files["proto/kacho/cloud/registry/v1/registry.proto"] = "" +
		"// ID реестра. Prefix \"reg\" (pkg/ids.NewID).\n" +
		"message Registry {}\n"
	files["pkg/ids/ids.go"] = "package ids\n"
	_, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("контракт, назвавший нынешнее место, объявлен находкой: %v", findings)
	}
}

// ── Молчит: голое имя без пути — это НАЗВАНИЕ службы, а не адрес ────────────

func TestFpcInjection_BareServiceNameStaysSilent(t *testing.T) {
	t.Parallel()
	files := fpcSoundTree()
	files["pkg/authz/proxytuple.go"] = "// публичность выражает сам кортеж: kacho-registry пишет его.\npackage authz\n"
	census, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("голое имя службы объявлено координатой: %v", findings)
	}
	if census.bareNames != 1 {
		t.Fatalf("полоса голых имён не сосчитана — «ноль находок» неотличимо от «не искали»: %s", census)
	}
}

// ── Молчит: заголовок запроса, а не путь ────────────────────────────────────

func TestFpcInjection_RequestHeaderStaysSilent(t *testing.T) {
	t.Parallel()
	files := fpcSoundTree()
	files["pkg/authz/subject.go"] = "// личность приходит парой x-kacho-principal-id/type\npackage authz\n"
	census, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("заголовок запроса объявлен полирепо-координатой: левая граница не проверяется: %v", findings)
	}
	if census.coordinates != 0 {
		t.Fatalf("заголовок сосчитан координатой: %s", census)
	}
}

// ── Полоса порождённого: молчит С клеймом, находка БЕЗ него ────────────────

func TestFpcInjection_GeneratedFileStaysSilent(t *testing.T) {
	t.Parallel()
	files := fpcSoundTree()
	files["pkg/api/kacho/cloud/registry/v1/registry.pb.go"] = "" +
		"// Code generated by protoc-gen-go. DO NOT EDIT.\n" +
		"// ID реестра. Prefix \"reg\" (kacho-corelib/ids.NewID).\n" +
		"package registryv1\n"
	census, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("порождённый файл объявлен нарушением: его текст приходит из контракта: %v", findings)
	}
	if census.filesGenerated != 1 {
		t.Fatalf("полоса порождённых не сосчитана: %s", census)
	}
}

func TestFpcInjection_TheSameLineWithoutTheGeneratedMarkIsFound(t *testing.T) {
	t.Parallel()
	files := fpcSoundTree()
	files["pkg/api/kacho/cloud/registry/v1/registry.pb.go"] = "" +
		"// ID реестра. Prefix \"reg\" (kacho-corelib/ids.NewID).\n" +
		"package registryv1\n"
	_, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("без клейма порождённого та же строка обязана быть находкой — иначе полоса есть маска: %v", findings)
	}
}

// ── Полоса популяции: молчит ВНЕ четырёх корней, находка ВНУТРИ них ───────
//
// Здесь стояло «дерево служб судится проверкой фундамента» с ожиданием
// МОЛЧАНИЯ: популяция была `pkg/` и `proto/`, и утверждение было верно. Оно
// пережило свой предмет — популяция расширена решением (`#2198`, см. шапку
// проверки), и служба теперь судится намеренно.
//
// Утверждение не снято, а ПЕРЕВЕДЕНО на признак, который дерево производит:
// граница популяции есть по-прежнему, она просто проходит в другом месте.
// Корень вне четырёх объявленных обязан молчать — иначе «расширили популяцию»
// значило бы «сняли границу», а перечень `foundationProseRoots` перестал бы
// что-либо решать.

func TestFpcInjection_OutsideTheDeclaredRootsStaysSilent(t *testing.T) {
	t.Parallel()
	files := fpcSoundTree()
	files["deploy/tools/mirror.go"] = "// Зеркалит kacho-vpc/internal/repo/jsonb.go.\npackage tools\n"
	census, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("корень вне объявленных судится проверкой: популяция шире объявленной: %v", findings)
	}
	if census.filesRead != 2 {
		t.Fatalf("файл вне популяции сосчитан прочитанным: %s", census)
	}
}

func TestFpcInjection_TheSameLineInsideTheFoundationIsFound(t *testing.T) {
	t.Parallel()
	files := fpcSoundTree()
	files["pkg/db/jsonb.go"] = "// Зеркалит kacho-vpc/internal/repo/jsonb.go.\npackage db\n"
	_, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("внутри фундамента та же строка обязана быть находкой: %v", findings)
	}
}

// ── Пустой обход отличим от нуля находок ───────────────────────────────────

func TestFpcInjection_EmptyWalkIsDistinguishableFromZeroFindings(t *testing.T) {
	t.Parallel()
	_, _, err := scanFoundationProse(fpcRootWith(t, map[string]string{"README.md": "нет кода\n"}))
	if err == nil {
		t.Fatal("пустой обход выдан за зелёный прогон")
	}
	if !strings.Contains(err.Error(), "обход пуст") {
		t.Fatalf("отказ не называет причину: %v", err)
	}
}

// ── Полоса СЛУЖБ и КРАЯ: популяция расширена, и расширение НЕ вакуумно ─────
//
// Пара обязательна по той же причине, что у контракта: первая половина
// доказывает, что дефект в новом корне краснеет и называет координату; вторая —
// что расширен ВИД файла, а не каталог целиком.

func TestFpcInjection_DeadCoordinateInAServiceIsFound(t *testing.T) {
	t.Parallel()
	files := fpcSoundTree()
	files["services/compute/internal/repo/jsonb.go"] = "" +
		"// Зеркалит kacho-vpc/internal/repo/jsonb.go.\n" +
		"package repo\n"
	census, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("мёртвая координата в дереве служб не найдена — расширение популяции "+
			"вакуумно: %v (%s)", findings, census)
	}
	got := findings[0].String()
	for _, want := range []string{"services/compute/internal/repo/jsonb.go:1", "kacho-vpc/internal/repo/jsonb.go"} {
		if !strings.Contains(got, want) {
			t.Fatalf("находка не называет %q: %s", want, got)
		}
	}
}

func TestFpcInjection_DeadCoordinateAtTheEdgeIsFound(t *testing.T) {
	t.Parallel()
	files := fpcSoundTree()
	files["gateway/internal/opsproxy/proxy.go"] = "" +
		"// Префикс идентификатора берётся у kacho-corelib/ids.\n" +
		"package opsproxy\n"
	_, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("мёртвая координата у края не найдена — край в популяции не участвует: %v", findings)
	}
}

func TestFpcInjection_TheSameLineInANonGoFileOfAServiceStaysSilent(t *testing.T) {
	t.Parallel()
	files := fpcSoundTree()
	files["services/compute/internal/repo/README.md"] = "Зеркалит kacho-vpc/internal/repo/jsonb.go.\n"
	census, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("файл вне вида популяции объявлен находкой — расширен каталог, а не вид: %v", findings)
	}
	if census.filesRead != 2 {
		t.Fatalf("молчание не объяснено: прочитано %d файлов вместо 2 контроля: %s",
			census.filesRead, census)
	}
}

// ── Полоса ДЕРЕВА ГЕЙТА: синтетика инъекции обязана уцелеть ────────────────
//
// Дефект в фикстуре гейта ЗАКОННЫЙ: без него инъекция беспредметна. Пара
// доказывает, что пропускается дерево гейта, а не строка: та же строка вне его
// остаётся находкой.

func TestFpcInjection_TheGateOwnTreeIsSkipped(t *testing.T) {
	t.Parallel()
	files := fpcSoundTree()
	files["services/iam/internal/supplyhygiene/directory_name_injection_test.go"] = "" +
		"// Фикстура: kacho-vpc/internal/apps/kacho/shared\n" +
		"package supplyhygiene\n"
	census, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("синтетика гейта объявлена находкой — инъекция соседа стала бы "+
			"беспредметной: %v", findings)
	}
	if census.filesGateOwn != 1 {
		t.Fatalf("полоса не названа числом: пропущено %d файлов дерева гейта вместо 1: %s",
			census.filesGateOwn, census)
	}
}

func TestFpcInjection_TheSameFixtureLineOutsideTheGateTreeIsFound(t *testing.T) {
	t.Parallel()
	files := fpcSoundTree()
	files["services/iam/internal/domain/constants.go"] = "" +
		"// Фикстура: kacho-vpc/internal/apps/kacho/shared\n" +
		"package domain\n"
	_, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("вне дерева гейта та же строка обязана быть находкой — иначе полоса есть "+
			"маска: %v", findings)
	}
}

// ── Полоса ЛИЧНОСТИ НАГРУЗКИ: SPIFFE-имя путём не является ────────────────

func TestFpcInjection_WorkloadIdentityStaysSilent(t *testing.T) {
	t.Parallel()
	files := fpcSoundTree()
	files["services/iam/internal/authzguard/fgaproxy_test.go"] = "" +
		"// Круг доверенных: spiffe://kacho.cloud/ns/kacho-storage/sa/kacho-storage\n" +
		"package authzguard\n"
	census, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("имя нагрузки объявлено мёртвой координатой — слепая замена сломала бы "+
			"объявленный круг: %v", findings)
	}
	if census.exWorkload != 1 {
		t.Fatalf("полоса не названа числом: личностей нагрузки %d вместо 1: %s",
			census.exWorkload, census)
	}
}

func TestFpcInjection_TheSameNameWithoutTheWorkloadSegmentIsFound(t *testing.T) {
	t.Parallel()
	files := fpcSoundTree()
	files["services/iam/internal/authzguard/fgaproxy_test.go"] = "" +
		"// Реализация — kacho-storage/internal/clients/iam.go\n" +
		"package authzguard\n"
	_, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("та же приставка без сегмента нагрузки обязана быть находкой — иначе "+
			"полоса накрывает адреса: %v", findings)
	}
}

// ── Полоса ПУТИ МОНТИРОВАНИЯ: координата в контейнере, а не в дереве ──────

func TestFpcInjection_MountPathStaysSilent(t *testing.T) {
	t.Parallel()
	files := fpcSoundTree()
	files["services/nlb/cmd/migrator/main.go"] = "" +
		"//\t--config  /etc/kacho-nlb/config.yaml\n" +
		"package main\n"
	census, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("путь монтирования объявлен мёртвой координатой дерева: %v", findings)
	}
	if census.exMountPath != 1 {
		t.Fatalf("полоса не названа числом: путей монтирования %d вместо 1: %s",
			census.exMountPath, census)
	}
}

func TestFpcInjection_TheSamePathWithoutTheMountPrefixIsFound(t *testing.T) {
	t.Parallel()
	files := fpcSoundTree()
	files["services/nlb/cmd/migrator/main.go"] = "" +
		"//\tУмолчание читается из kacho-nlb/config.yaml\n" +
		"package main\n"
	_, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("тот же путь без приставки монтирования обязан быть находкой — иначе "+
			"полоса накрывает адреса дерева: %v", findings)
	}
}

// ── Полоса ПЕРЕЧИСЛЕНИЯ ВЛАДЕЛЬЦЕВ: за косой чертой вторая служба ─────────

func TestFpcInjection_OwnerEnumerationStaysSilent(t *testing.T) {
	t.Parallel()
	files := fpcSoundTree()
	files["services/compute/internal/apps/kacho/api/instance/instance.go"] = "" +
		"// Зеркало output-only (source of truth = kacho-vpc/kacho-storage).\n" +
		"package instance\n"
	census, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("перечисление двух владельцев объявлено адресом: %v", findings)
	}
	if census.exOwnerEnum != 1 {
		t.Fatalf("полоса не названа числом: перечислений %d вместо 1: %s",
			census.exOwnerEnum, census)
	}
}

func TestFpcInjection_TheSameFormWithAPathTailIsFound(t *testing.T) {
	t.Parallel()
	files := fpcSoundTree()
	files["services/compute/internal/apps/kacho/api/instance/instance.go"] = "" +
		"// Зеркало читает kacho-vpc/internal/dto/mirror.go.\n" +
		"package instance\n"
	_, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("та же приставка с путём за ней обязана быть находкой — иначе полоса "+
			"накрывает адреса: %v", findings)
	}
}

// ── Полоса КАТАЛОГА СБОРКИ: совпало имя двоичного файла, а не топология ───

func TestFpcInjection_LiveBuildDirStaysSilent(t *testing.T) {
	t.Parallel()
	files := fpcSoundTree()
	files["services/registry/internal/apps/kacho/config/config.go"] = "" +
		"// см. cmd/kacho-registry/describe\n" +
		"package config\n"
	files["services/registry/cmd/kacho-registry/main.go"] = "package main\n"
	census, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("ссылка внутрь службы объявлена полирепо-координатой: %v", findings)
	}
	if census.exBuildDir != 1 {
		t.Fatalf("полоса не названа числом: каталогов сборки %d вместо 1: %s",
			census.exBuildDir, census)
	}
}

func TestFpcInjection_TheSameCoordinateWithoutItsBuildDirIsFound(t *testing.T) {
	t.Parallel()
	files := fpcSoundTree()
	files["services/registry/internal/apps/kacho/config/config.go"] = "" +
		"// см. cmd/kacho-registry/describe\n" +
		"package config\n"
	_, findings, err := scanFoundationProse(fpcRootWith(t, files))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("без живого каталога сборки та же координата обязана быть находкой — "+
			"иначе полоса есть маска: %v", findings)
	}
}

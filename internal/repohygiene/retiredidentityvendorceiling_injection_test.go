// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Инъекция в обе стороны. ПАРЫ ОДНОФАКТНЫЕ: дефект и законный близнец
// различаются РОВНО ОДНИМ фактом, и полярность утверждения в этот факт не
// входит — пара с двумя различиями законна по двум независимым причинам и не
// показывает, на что гейт смотрит.
//
// Каждая пара названа своим фактом.

// vendorCorpusOf — корпус дерева с согласованной переписью обхода: всякий путь
// лежит ровно в одной категории, и Walked равен их сумме.
func vendorCorpusOf(bodies, blobs map[string]string, archives []vendorArchive, prose []string, lines int) vendorTreeCorpus {
	if bodies == nil {
		bodies = map[string]string{}
	}
	if blobs == nil {
		blobs = map[string]string{}
	}
	c := vendorTreeCorpus{Bodies: bodies, Blobs: blobs, Archives: archives, Prose: prose, LinesRead: lines}
	c.Walked = len(bodies) + len(blobs) + len(archives) + len(prose)
	return c
}

// vendorFixture — корпус трёх деревьев: предмет кладётся в платформу, а пустые
// (но НЕ отсутствующие) фундамент и служба доступа остаются под судом.
func vendorFixture(platform map[string]string, archives []vendorArchive) map[string]vendorTreeCorpus {
	base := map[string]string{"deploy/helm/umbrella/values.x.yaml": "a: 1\nb: 2\n"}
	for k, v := range platform {
		base[k] = v
	}
	return map[string]vendorTreeCorpus{
		vendorTreePlatform:   vendorCorpusOf(base, nil, archives, nil, 3),
		vendorTreeAccess:     vendorCorpusOf(map[string]string{"go.mod": "module x\n"}, nil, nil, nil, 2),
		vendorTreeFoundation: vendorCorpusOf(map[string]string{"go.mod": "module y\n"}, nil, nil, nil, 2),
	}
}

var vendorZeroCeilings = map[string]int{
	vendorTreePlatform:   0,
	vendorTreeAccess:     0,
	vendorTreeFoundation: 0,
}

// judgeOne — сколько привязок нашлось в платформе и какие именно.
func judgeOne(t *testing.T, corpora map[string]vendorTreeCorpus) (int, []vendorBinding) {
	t.Helper()
	_, census, bindings, err := judgeRetiredVendorCeiling(corpora, vendorZeroCeilings)
	if err != nil {
		t.Fatalf("фикстура обязана судиться: %v", err)
	}
	return census[vendorTreePlatform].Bindings, bindings
}

// vendorTarGz — настоящий gzip+tar с одной записью. Байты, а не подделка: то же,
// что лежит в дереве.
func vendorTarGz(t *testing.T, member, body string) []byte {
	t.Helper()
	var raw bytes.Buffer
	gz := gzip.NewWriter(&raw)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: member, Mode: 0o600, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return raw.Bytes()
}

// vendorZip — настоящий zip с одной записью.
func vendorZip(t *testing.T, member, body string) []byte {
	t.Helper()
	var raw bytes.Buffer
	zw := zip.NewWriter(&raw)
	w, err := zw.Create(member)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return raw.Bytes()
}

// vendorReadTree — платформа, прочитанная ЧИТАТЕЛЕМ с диска, и два пустых, но
// присутствующих дерева. Инъекция, проверяющая читателя, обязана звать читателя.
func vendorReadTree(t *testing.T, root string, rels []string) map[string]vendorTreeCorpus {
	t.Helper()
	return map[string]vendorTreeCorpus{
		vendorTreePlatform:   vendorCorpusFromPaths(root, rels),
		vendorTreeAccess:     vendorCorpusOf(map[string]string{"go.mod": "module x\n"}, nil, nil, nil, 2),
		vendorTreeFoundation: vendorCorpusOf(map[string]string{"go.mod": "module y\n"}, nil, nil, nil, 2),
	}
}

// vendorWriteAt — файл под корнем временного дерева.
func vendorWriteAt(t *testing.T, root, rel string, data []byte) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestRetiredVendorCeiling_NameAxisInjection — ОДИН ФАКТ: чьё имя в строке.
//
// Обе половины пары — утверждения одной формы и одного знака; меняется только
// имя: издателя на своё. Формы взяты те самые, которые источник перечислял
// одиннадцатью плечами: образ, переменная окружения, имя службы, алиас подчарта,
// столбец, печенье. Ни одно из них в распознавателе не выписано — все шесть
// ловятся ОДНИМ признаком класса.
func TestRetiredVendorCeiling_NameAxisInjection(t *testing.T) {
	t.Parallel()

	pairs := []struct{ fact, defect, twin string }{
		{"образ", `    image: oryd/hydra:v2.2.0`, `    image: prorobotech/kaname:v2.2.0`},
		{"переменная окружения", `  KACHO_HYDRA_ADMIN_URL: https://admin:4445`, `  KACHO_KANAME_ADMIN_URL: https://admin:4445`},
		{"имя службы", `  host: kacho-umbrella-kratos-public.kacho.svc`, `  host: kacho-umbrella-kaname-public.kacho.svc`},
		{"алиас подчарта", `  - alias: pg-hydra`, `  - alias: pg-kaname`},
		{"столбец", `	hydra_client_id TEXT NOT NULL,`, `	kaname_client_id TEXT NOT NULL,`},
		{"печенье", `const sessionCookie = "ory_kratos_session"`, `const sessionCookie = "kacho_session"`},
	}
	for _, p := range pairs {
		t.Run("факт: "+p.fact, func(t *testing.T) {
			t.Parallel()

			n, bindings := judgeOne(t, vendorFixture(map[string]string{
				"deploy/helm/umbrella/values.y.yaml": "head: 1\n" + p.defect + "\ntail: 2\n"}, nil))
			if n != 1 {
				t.Fatalf("дефект обязан краснеть: привязок %d, ждали 1 (%q)", n, p.defect)
			}
			got := bindings[0]
			if got.File != "deploy/helm/umbrella/values.y.yaml" || got.Line != 2 {
				t.Fatalf("находка обязана называть координату: %s:%d", got.File, got.Line)
			}
			if got.Axis != vendorAxisName {
				t.Fatalf("ось распознана не та: %q", got.Axis)
			}

			m, _ := judgeOne(t, vendorFixture(map[string]string{
				"deploy/helm/umbrella/values.y.yaml": "head: 1\n" + p.twin + "\ntail: 2\n"}, nil))
			if m != 0 {
				t.Fatalf("законный близнец обязан молчать: привязок %d при строке %q", m, p.twin)
			}
		})
	}
}

// TestRetiredVendorCeiling_PathAxisInjection — ОДИН ФАКТ: чьё имя в ПУТИ файла.
//
// Тело у обеих половин одно и то же и нейтрально; меняется только имя в пути —
// издателя на наше. Прежде судилось одно тело, а шапка гейта уже перечисляла
// путь среди накрываемых форм: гейт молчал там, где заявлял, что смотрит.
func TestRetiredVendorCeiling_PathAxisInjection(t *testing.T) {
	t.Parallel()

	const neutral = "image: prorobotech/platform:v1\n"
	const defect = "deploy/helm/umbrella/charts/kratos-selfservice-ui/values.yaml"
	const twin = "deploy/helm/umbrella/charts/kaname-selfservice-ui/values.yaml"

	n, bindings := judgeOne(t, vendorFixture(map[string]string{defect: neutral}, nil))
	if n != 1 {
		t.Fatalf("имя издателя В ПУТИ обязано краснеть: привязок %d (%v)", n, bindings)
	}
	if bindings[0].File != defect || bindings[0].Axis != vendorAxisPath {
		t.Fatalf("находка обязана называть путь и свою ось: %s (%q)", bindings[0].File, bindings[0].Axis)
	}
	if bindings[0].Line != 0 {
		t.Fatalf("путь судится целиком, номер строки у него 0, получено %d", bindings[0].Line)
	}

	if m, _ := judgeOne(t, vendorFixture(map[string]string{twin: neutral}, nil)); m != 0 {
		t.Fatalf("наше имя в том же месте пути обязано молчать: привязок %d", m)
	}
}

// TestRetiredVendorCeiling_SurfaceAxisInjection — ОДИН ФАКТ: путь API издателя.
//
// Строка одна и та же, вызов один и тот же, знак один и тот же; меняется путь —
// его на наш. Перечень путей у гейта СВОЙ не заводится: он берётся у
// `ProviderSurfaces`, единственного дома этого словаря.
func TestRetiredVendorCeiling_SurfaceAxisInjection(t *testing.T) {
	t.Parallel()

	const file = "gateway/internal/clients/reacher.go"
	defect := "func reach(b string) string { return b + \"" + ProviderSurfaces[0].Path + "\" }"
	twin := `func reach(b string) string { return b + "/admin/tenants" }`

	n, bindings := judgeOne(t, vendorFixture(map[string]string{file: "package clients\n" + defect + "\n"}, nil))
	if n != 1 || bindings[0].Axis != vendorAxisSurface {
		t.Fatalf("дефект обязан краснеть осью пути: привязок %d, ось %q", n, axisOf(bindings))
	}
	if m, _ := judgeOne(t, vendorFixture(map[string]string{file: "package clients\n" + twin + "\n"}, nil)); m != 0 {
		t.Fatalf("законный близнец обязан молчать: привязок %d при строке %q", m, twin)
	}
}

// TestRetiredVendorCeiling_GluedNameInjection — ОДИН ФАКТ: чьё имя собрано из
// соседних литералов в одной строке.
//
// Форма записи, которой не знает ни одно плечо перечня: имя издателя не лежит в
// строке целиком. Склейка ЧЕРЕЗ ПЕРЕМЕННУЮ и через перенос строки объявлена
// границей (шапка гейта, границы, п. 3) — эта ось закрывает только однострочную.
func TestRetiredVendorCeiling_GluedNameInjection(t *testing.T) {
	t.Parallel()

	const file = "gateway/internal/clients/addr.go"
	defect := `const host = "hy" + "dra-admin"`
	twin := `const host = "kan" + "ame-admin"`

	n, bindings := judgeOne(t, vendorFixture(map[string]string{file: "package clients\n" + defect + "\n"}, nil))
	if n != 1 || bindings[0].Axis != vendorAxisGlued {
		t.Fatalf("склеенное имя издателя обязано краснеть своей осью: привязок %d, ось %q",
			n, axisOf(bindings))
	}
	if bindings[0].Line != 2 {
		t.Fatalf("находка обязана называть строку: %d", bindings[0].Line)
	}
	if m, _ := judgeOne(t, vendorFixture(map[string]string{file: "package clients\n" + twin + "\n"}, nil)); m != 0 {
		t.Fatalf("наше имя, склеенное так же, обязано молчать: привязок %d", m)
	}
}

// TestRetiredVendorCeiling_RetainedDependencyIsSilent — ОДИН ФАКТ: какой пакет
// издателя импортирован.
//
// Обе строки — импорт пакета `github.com/ory/...` одной формы и одного знака.
// Снятый провайдер — находка; БИБЛИОТЕКА того же издателя, законно остающаяся у
// собственной чеканки (`fosite`), молчит: класс построен на именах ПРОДУКТОВ
// издателя и на пространстве его образов, а не на слове «ory».
//
// Фикстура СИНТЕТИЧЕСКАЯ намеренно: в трёх деревьях сегодня ноль строк с этой
// зависимостью (измерено), и проба, привязанная к живой строке, истекла бы
// вместе с ней. Здесь она ждёт дня, когда библиотека приедет.
func TestRetiredVendorCeiling_RetainedDependencyIsSilent(t *testing.T) {
	t.Parallel()

	const file = "services/iam/internal/minting/deps.go"
	defect := `	_ "github.com/ory/hydra-client-go/v2"`
	twin := `	_ "github.com/ory/fosite"`

	if n, _ := judgeOne(t, vendorFixture(map[string]string{file: "package minting\nimport (\n" + defect + "\n)\n"}, nil)); n != 1 {
		t.Fatalf("клиент снятого провайдера обязан краснеть: привязок %d", n)
	}
	if m, _ := judgeOne(t, vendorFixture(map[string]string{file: "package minting\nimport (\n" + twin + "\n)\n"}, nil)); m != 0 {
		t.Fatalf("сохранённая зависимость чеканки обязана молчать: привязок %d при строке %q", m, twin)
	}
}

// TestRetiredVendorCeiling_ProseIsSilent — ТРИ пары, в каждой ровно один факт.
//
//	факт 1: категория файла — одна и та же строка в судимом файле и в прозе;
//	факт 2: маркер комментария в начале строки — одна и та же строка с ним и без;
//	факт 3: категория файла при имени издателя В ПУТИ — проза не судится и путём.
//
// Проза не привязывает дерево к издателю, а утверждение о нём как о действующем
// судит соседнее надгробие `retiredissuerclaim.go`. Давить числом на надгробия
// значило бы требовать стереть историю.
func TestRetiredVendorCeiling_ProseIsSilent(t *testing.T) {
	t.Parallel()

	const line = "hydraAdminURL: https://provider-admin.kacho.svc"

	if n, _ := judgeOne(t, vendorFixture(map[string]string{"deploy/notes.yaml": line + "\n"}, nil)); n != 1 {
		t.Fatalf("строка судимого файла обязана краснеть: привязок %d", n)
	}
	if m, _ := judgeOne(t, vendorFixture(map[string]string{"deploy/notes.md": line + "\n"}, nil)); m != 0 {
		t.Fatalf("та же строка в прозе обязана молчать: привязок %d", m)
	}
	if m, _ := judgeOne(t, vendorFixture(map[string]string{"deploy/notes.yaml": "# " + line + "\n"}, nil)); m != 0 {
		t.Fatalf("та же строка под маркером комментария обязана молчать: привязок %d", m)
	}

	// Проза не судится и ПУТЁМ: надгробие в `docs/` остаётся историей.
	if n, _ := judgeOne(t, vendorFixture(map[string]string{"docs/hydra-retirement.yaml": "a: 1\n"}, nil)); n != 1 {
		t.Fatalf("имя издателя в пути судимого файла обязано краснеть: привязок %d", n)
	}
	if m, _ := judgeOne(t, vendorFixture(map[string]string{"docs/hydra-retirement.md": "a: 1\n"}, nil)); m != 0 {
		t.Fatalf("тот же путь у прозы обязан молчать: привязок %d", m)
	}

	// Хвост-подпорка: `--` без пробела — не комментарий, а флаг, и привязку он несёт.
	flag := `    --env-var "providerPublicBaseUrl=http://localhost:${HYDRA_PUBLIC_PORT}" \`
	if n, _ := judgeOne(t, vendorFixture(map[string]string{"deploy/scripts/run.sh": flag + "\n"}, nil)); n != 1 {
		t.Fatalf("флаг командной строки — не комментарий и обязан краснеть: привязок %d", n)
	}
	if m, _ := judgeOne(t, vendorFixture(map[string]string{"deploy/scripts/x.sql": "-- " + line + "\n"}, nil)); m != 0 {
		t.Fatalf("`-- ` с пробелом — комментарий и обязан молчать: привязок %d", m)
	}
}

// TestRetiredVendorCeiling_ArchiveSuffixDoesNotDecide — ОДИН ФАКТ: суффикс имени
// архива. Байты у трёх половин ОДНИ И ТЕ ЖЕ, и все три обязаны краснеть: архив
// не перестаёт быть архивом от переименования.
//
// Прежде архивность решал перечень суффиксов: те же байты под именем `.zip`
// уходили в счётчик пропущенного — зелёное при физически присутствующем
// предмете. Здесь корпус читается ЧИТАТЕЛЕМ с диска: проверяется то, что
// исполняется на дереве, а не пересказ.
func TestRetiredVendorCeiling_ArchiveSuffixDoesNotDecide(t *testing.T) {
	t.Parallel()

	data := vendorTarGz(t, "idp/values.yaml", "image: oryd/kratos:v1.3.1\n")
	dir := t.TempDir()
	for _, name := range []string{"charts/idp-0.62.1.tgz", "charts/idp-0.62.1.zip", "charts/idp-0.62.1.bin"} {
		vendorWriteAt(t, dir, name, data)
		_, census, bindings, err := judgeRetiredVendorCeiling(vendorReadTree(t, dir, []string{name}), vendorZeroCeilings)
		if err != nil {
			t.Fatalf("%s: фикстура обязана судиться: %v", name, err)
		}
		c := census[vendorTreePlatform]
		if c.Bindings != 1 || c.Archives != 1 || bindings[0].Axis != vendorAxisArchiveBody {
			t.Fatalf("те же байты архива под именем %s обязаны краснеть содержимым: "+
				"привязок %d · архивов %d · ось %q", name, c.Bindings, c.Archives, axisOf(bindings))
		}
		if c.Prose != 0 {
			t.Fatalf("%s: архив не имеет права уходить в пропущенное: прозы %d", name, c.Prose)
		}
	}

	// Законный близнец: те же байты в том же формате, чужое вендоренное внутри.
	twin := vendorTarGz(t, "db/values.yaml", "image: bitnami/postgresql:16\n")
	vendorWriteAt(t, dir, "charts/db-13.4.4.zip", twin)
	_, census, _, err := judgeRetiredVendorCeiling(vendorReadTree(t, dir, []string{"charts/db-13.4.4.zip"}), vendorZeroCeilings)
	if err != nil {
		t.Fatalf("фикстура обязана судиться: %v", err)
	}
	if census[vendorTreePlatform].Bindings != 0 {
		t.Fatalf("чужое вендоренное обязано молчать: привязок %d", census[vendorTreePlatform].Bindings)
	}
}

// TestRetiredVendorCeiling_ArchiveFormatIsReadFromTheBytes — ОДИН ФАКТ: чьё имя
// внутри архива. Формат один и тот же настоящий `zip`, имя файла одно и то же и
// нейтральное; меняется только содержимое.
//
// Пара доказывает, что признак архивности снят с ПЕРВЫХ БАЙТОВ, а содержимое
// разворачивается по формату, а не по имени.
func TestRetiredVendorCeiling_ArchiveFormatIsReadFromTheBytes(t *testing.T) {
	t.Parallel()

	const name = "charts/idp-0.62.1.pkg"
	dir := t.TempDir()

	vendorWriteAt(t, dir, name, vendorZip(t, "idp/values.yaml", "image: oryd/kratos:v1.3.1\n"))
	_, census, bindings, err := judgeRetiredVendorCeiling(vendorReadTree(t, dir, []string{name}), vendorZeroCeilings)
	if err != nil {
		t.Fatalf("фикстура обязана судиться: %v", err)
	}
	if census[vendorTreePlatform].Bindings != 1 || bindings[0].Axis != vendorAxisArchiveBody {
		t.Fatalf("zip с предметом внутри обязан краснеть содержимым: привязок %d · ось %q",
			census[vendorTreePlatform].Bindings, axisOf(bindings))
	}
	if census[vendorTreePlatform].Sealed != 0 {
		t.Fatalf("zip разворачивается стандартной библиотекой: нераспакованных %d",
			census[vendorTreePlatform].Sealed)
	}

	vendorWriteAt(t, dir, name, vendorZip(t, "db/values.yaml", "image: bitnami/postgresql:16\n"))
	_, census, _, err = judgeRetiredVendorCeiling(vendorReadTree(t, dir, []string{name}), vendorZeroCeilings)
	if err != nil {
		t.Fatalf("фикстура обязана судиться: %v", err)
	}
	if census[vendorTreePlatform].Bindings != 0 {
		t.Fatalf("тот же zip без предмета обязан молчать: привязок %d", census[vendorTreePlatform].Bindings)
	}
}

// TestRetiredVendorCeiling_ArchiveNameInjection — ОДИН ФАКТ: имя архива.
//
// Содержимое у обеих половин ОДНО И ТО ЖЕ и имени издателя не несёт — так
// выглядит переименование у издателя внутри чарта, уводившее прежнее плечо в
// ноль при физически присутствующем архиве. Меняется только имя файла.
//
// Здесь же видно, почему предикат «архивов ноль» негоден: чужое вендоренное
// (`postgresql-13.4.4.tgz`) — законный близнец, он остаётся.
func TestRetiredVendorCeiling_ArchiveNameInjection(t *testing.T) {
	t.Parallel()

	const renamedInside = "idp/Chart.yaml\nname: idp\nversion: 0.62.1\n"

	defect := []vendorArchive{{Name: "deploy/helm/umbrella/charts/hydra-0.62.1.tgz",
		Format: "gzip", Opened: true, Text: renamedInside}}
	n, bindings := judgeOne(t, vendorFixture(nil, defect))
	if n != 1 || bindings[0].Axis != vendorAxisArchiveName {
		t.Fatalf("архив издателя обязан краснеть по ИМЕНИ при переименованном содержимом: "+
			"привязок %d, ось %q", n, axisOf(bindings))
	}

	twin := []vendorArchive{{Name: "deploy/helm/umbrella/charts/postgresql-13.4.4.tgz",
		Format: "gzip", Opened: true, Text: renamedInside}}
	if m, _ := judgeOne(t, vendorFixture(nil, twin)); m != 0 {
		t.Fatalf("чужое вендоренное обязано молчать: привязок %d", m)
	}
}

// TestRetiredVendorCeiling_ArchiveBodyInjection — ОДИН ФАКТ: содержимое архива.
//
// Имя у обеих половин одно и то же и имени издателя не несёт — так выглядит
// переименование САМОГО чарта. Меняется только содержимое.
func TestRetiredVendorCeiling_ArchiveBodyInjection(t *testing.T) {
	t.Parallel()

	const name = "deploy/helm/umbrella/charts/idp-0.62.1.tgz"

	defect := []vendorArchive{{Name: name, Format: "gzip", Opened: true,
		Text: "idp/values.yaml\nimage: oryd/kratos:v1.3.1\n"}}
	n, bindings := judgeOne(t, vendorFixture(nil, defect))
	if n != 1 || bindings[0].Axis != vendorAxisArchiveBody {
		t.Fatalf("архив издателя обязан краснеть по СОДЕРЖИМОМУ при нейтральном имени: "+
			"привязок %d, ось %q", n, axisOf(bindings))
	}

	twin := []vendorArchive{{Name: name, Format: "gzip", Opened: true,
		Text: "idp/values.yaml\nimage: bitnami/postgresql:16\n"}}
	if m, _ := judgeOne(t, vendorFixture(nil, twin)); m != 0 {
		t.Fatalf("архив без предмета обязан молчать: привязок %d", m)
	}
}

// TestRetiredVendorCeiling_BinaryCarrierInjection — ОДИН ФАКТ: чьё имя внутри
// двоичного файла. Имя файла и сама двоичность у обеих половин одни и те же.
//
// Читается ЧИТАТЕЛЕМ с диска: двоичное прежде уходило в пропущенное целиком, и
// собранный двоичник издателя лежал бы в дереве беззвучно.
func TestRetiredVendorCeiling_BinaryCarrierInjection(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const name = "deploy/bin/payload.dat"
	bindingsFor := func(mark string) (vendorTreeCensus, []vendorBinding) {
		t.Helper()
		vendorWriteAt(t, dir, name, append([]byte{0x00, 0x01, 0x02}, []byte(mark+"\x00")...))
		_, census, bindings, err := judgeRetiredVendorCeiling(vendorReadTree(t, dir, []string{name}), vendorZeroCeilings)
		if err != nil {
			t.Fatalf("фикстура обязана судиться: %v", err)
		}
		return census[vendorTreePlatform], bindings
	}

	c, bindings := bindingsFor("ory_kratos_session")
	if c.Bindings != 1 || bindings[0].Axis != vendorAxisBinary {
		t.Fatalf("двоичный файл с именем издателя внутри обязан краснеть своей осью: "+
			"привязок %d, ось %q", c.Bindings, axisOf(bindings))
	}
	if c.Blobs != 1 || c.Files != 0 {
		t.Fatalf("двоичное обязано лежать в своей категории: двоичных %d · судимо построчно %d",
			c.Blobs, c.Files)
	}

	if c, _ := bindingsFor("kacho_session"); c.Bindings != 0 {
		t.Fatalf("тот же двоичный файл с нашим именем обязан молчать: привязок %d", c.Bindings)
	}
}

// TestRetiredVendorCeiling_EmptiedTreeIsNotZero — ГЛАВНОЕ: сценарий отказа.
//
// ОДИН ФАКТ: присутствует ли путь издателя в дереве. Тела очищены у ОБЕИХ
// половин — так выглядит последний шаг снятия. Пока каталог стоит, ноль
// печатать нельзя: по этому нулю подпишут «предмет снят целиком».
func TestRetiredVendorCeiling_EmptiedTreeIsNotZero(t *testing.T) {
	t.Parallel()

	emptied := map[string]string{
		"deploy/helm/umbrella/charts/kratos-selfservice-ui/values.yaml": "",
		"deploy/helm/umbrella/templates/hydra-admin-certificate.yaml":   "",
		"gateway/internal/middleware/kratos_session.go":                 "",
	}
	n, bindings := judgeOne(t, vendorFixture(emptied, nil))
	if n != 3 {
		t.Fatalf("очищенные тела при ОСТАВШЕМСЯ каталоге не равны снятому предмету: "+
			"привязок %d, ждали 3 (%v)", n, bindings)
	}
	for _, b := range bindings {
		if b.Axis != vendorAxisPath {
			t.Fatalf("осью обязан быть путь: %q у %s", b.Axis, b.File)
		}
	}

	// Законный близнец: те же тела, путей издателя в дереве больше нет.
	renamed := map[string]string{
		"deploy/helm/umbrella/charts/kaname-selfservice-ui/values.yaml": "",
		"deploy/helm/umbrella/templates/kaname-admin-certificate.yaml":  "",
		"gateway/internal/middleware/kaname_session.go":                 "",
	}
	if m, _ := judgeOne(t, vendorFixture(renamed, nil)); m != 0 {
		t.Fatalf("дерево без путей издателя обязано давать ноль: привязок %d", m)
	}
}

// TestRetiredVendorCeiling_FoundationIsWalked — ОДИН ФАКТ: имя издателя в строке
// ФУНДАМЕНТА.
//
// Дефект, введённый в третье дерево, обязан краснеть с координатой `corelib` —
// иначе фундамент не обходится вовсе, и дыра откроется в день, когда движок
// приедет туда. Близнец — та же строка той же формы со своим именем.
func TestRetiredVendorCeiling_FoundationIsWalked(t *testing.T) {
	t.Parallel()

	defect := vendorFixture(nil, nil)
	defect[vendorTreeFoundation] = vendorCorpusOf(map[string]string{
		"identity/session.go": "package identity\n\nconst cookie = \"ory_kratos_session\"\n"}, nil, nil, nil, 4)
	f, census, bindings, err := judgeRetiredVendorCeiling(defect, vendorZeroCeilings)
	if err != nil {
		t.Fatalf("фикстура обязана судиться: %v", err)
	}
	if census[vendorTreeFoundation].Bindings != 1 {
		t.Fatalf("фундамент обязан обходиться: привязок в нём %d", census[vendorTreeFoundation].Bindings)
	}
	if len(f) != 1 || f[0].Tree != vendorTreeFoundation {
		t.Fatalf("находка обязана называть дерево фундамента: %+v", f)
	}
	if bindings[0].File != "identity/session.go" {
		t.Fatalf("находка обязана называть координату внутри фундамента: %s", bindings[0].File)
	}

	twin := vendorFixture(nil, nil)
	twin[vendorTreeFoundation] = vendorCorpusOf(map[string]string{
		"identity/session.go": "package identity\n\nconst cookie = \"kacho_session\"\n"}, nil, nil, nil, 4)
	if _, c2, _, err := judgeRetiredVendorCeiling(twin, vendorZeroCeilings); err != nil || c2[vendorTreeFoundation].Bindings != 0 {
		t.Fatalf("законный близнец в фундаменте обязан молчать: привязок %d (%v)",
			c2[vendorTreeFoundation].Bindings, err)
	}
}

// TestRetiredVendorCeiling_ShrunkLedgerIsAFinding — ОДИН ФАКТ: записанное число.
//
// Дерево у обеих половин одно и то же; меняется только запись потолка. Потолок
// УБЫВАЮЩИЙ: число под записью — находка «перепишите запись», иначе потолок
// прощал бы возврат ровно настолько, насколько успели снять.
func TestRetiredVendorCeiling_ShrunkLedgerIsAFinding(t *testing.T) {
	t.Parallel()

	corpora := vendorFixture(map[string]string{
		"deploy/helm/umbrella/values.y.yaml": "image: oryd/hydra:v2.2.0\n"}, nil)

	stale := map[string]int{vendorTreePlatform: 2, vendorTreeAccess: 0, vendorTreeFoundation: 0}
	f, _, _, err := judgeRetiredVendorCeiling(corpora, stale)
	if err != nil {
		t.Fatalf("фикстура обязана судиться: %v", err)
	}
	if len(f) != 1 || f[0].Kind != vendorFindingShrunk {
		t.Fatalf("убывание обязано быть находкой: %+v", f)
	}
	if !strings.Contains(f[0].String(), "Перепишите потолок на 1") {
		t.Fatalf("находка обязана называть новое число: %s", f[0])
	}

	exact := map[string]int{vendorTreePlatform: 1, vendorTreeAccess: 0, vendorTreeFoundation: 0}
	if g, _, _, err := judgeRetiredVendorCeiling(corpora, exact); err != nil || len(g) != 0 {
		t.Fatalf("точная запись обязана молчать: %+v (%v)", g, err)
	}
}

// TestRetiredVendorCeiling_FindingNamesItsAddresses — ОДИН ФАКТ: назвала ли
// находка координату, на которую выросла.
//
// Перечень адресов обязан быть ПОЛНЫМ и идти ПО ПУТИ. Порядок здесь — часть
// свойства: фикстура нарочно ставит одинокую строку в файл, который по числу
// строк оказался бы последним, а по пути стоит первым. По убыванию числа
// выросшая на единицу координата всегда внизу — там, где перечень и обрезают.
func TestRetiredVendorCeiling_FindingNamesItsAddresses(t *testing.T) {
	t.Parallel()

	corpora := vendorFixture(map[string]string{
		"deploy/helm/umbrella/charts/hydra-injected/values.yaml": "image: prorobotech/platform:v1\n",
		"gateway/internal/clients/one.go":                        "package clients\nconst h = \"kratos-public\"\nconst a = \"hydra-admin\"\n",
	}, nil)
	f, _, _, err := judgeRetiredVendorCeiling(corpora, vendorZeroCeilings)
	if err != nil {
		t.Fatalf("фикстура обязана судиться: %v", err)
	}
	if len(f) != 1 || f[0].Kind != vendorFindingGrown {
		t.Fatalf("рост обязан быть находкой: %+v", f)
	}
	text := f[0].String()
	for _, want := range []string{
		"    1 · deploy/helm/umbrella/charts/hydra-injected/values.yaml",
		"    2 · gateway/internal/clients/one.go",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("находка обязана называть адрес %q, напечатано:\n%s", want, text)
		}
	}
	if strings.Index(text, "hydra-injected") > strings.Index(text, "one.go") {
		t.Fatalf("адреса обязаны идти ПО ПУТИ, а не по числу строк:\n%s", text)
	}
	if len(f[0].Coords) != 2 || !strings.Contains(text, "все 2, по пути") {
		t.Fatalf("перечень обязан быть полным и назвать своё число:\n%s", text)
	}
}

// TestRetiredVendorCeiling_NarrowedLedgerIsARefusal — ОДИН ФАКТ: наличие строки
// ведомости для фундамента.
//
// Снятие одной строки уводило бы ЦЕЛОЕ ДЕРЕВО с суда молча: перепись печатала бы
// «деревьев два», а нули необойдённого дерева были бы неотличимы от законного
// нуля. Это тот же класс, что арифметическая проверка, слепая к отсутствующей
// строке, и лечится он одинаково: ОТКАЗ, а не «находок ноль».
func TestRetiredVendorCeiling_NarrowedLedgerIsARefusal(t *testing.T) {
	t.Parallel()

	corpora := vendorFixture(nil, nil)

	full := map[string]int{vendorTreePlatform: 0, vendorTreeAccess: 0, vendorTreeFoundation: 0}
	_, census, _, err := judgeRetiredVendorCeiling(corpora, full)
	if err != nil || len(census) != len(retiredVendorTrees) {
		t.Fatalf("полная ведомость обязана судить три дерева: деревьев %d (%v)", len(census), err)
	}

	narrowed := map[string]int{vendorTreePlatform: 0, vendorTreeAccess: 0}
	if _, _, _, err := judgeRetiredVendorCeiling(corpora, narrowed); !errors.Is(err, errVendorLedger) {
		t.Fatalf("ведомость без строки фундамента обязана быть ОТКАЗОМ, получено: %v", err)
	}

	swapped := map[string]int{vendorTreePlatform: 0, vendorTreeAccess: 0, "kanaris": 0}
	if _, _, _, err := judgeRetiredVendorCeiling(corpora, swapped); !errors.Is(err, errVendorLedger) {
		t.Fatalf("подмена строки чужим деревом обязана быть ОТКАЗОМ, получено: %v", err)
	}

	extra := map[string]int{vendorTreePlatform: 0, vendorTreeAccess: 0,
		vendorTreeFoundation: 0, "kanaris": 0}
	if _, _, _, err := judgeRetiredVendorCeiling(corpora, extra); !errors.Is(err, errVendorLedger) {
		t.Fatalf("лишняя строка ведомости обязана быть ОТКАЗОМ, получено: %v", err)
	}
}

// TestRetiredVendorCeiling_PartitionMismatchIsARefusal — ОДИН ФАКТ: сошлось ли
// разбиение обхода с числом обойдённых путей.
//
// Этим и доказана полнота списка границ ПО КАТЕГОРИЯМ ФАЙЛОВ: категория, о
// которой забыли, обрушит сверку, а не замолчит.
func TestRetiredVendorCeiling_PartitionMismatchIsARefusal(t *testing.T) {
	t.Parallel()

	ok := vendorFixture(nil, nil)
	if _, _, _, err := judgeRetiredVendorCeiling(ok, vendorZeroCeilings); err != nil {
		t.Fatalf("сошедшееся разбиение обязано судиться: %v", err)
	}

	lost := vendorFixture(nil, nil)
	c := lost[vendorTreePlatform]
	c.Walked++ // путь обойден, но ни в одну категорию не положен
	lost[vendorTreePlatform] = c
	if _, _, _, err := judgeRetiredVendorCeiling(lost, vendorZeroCeilings); !errors.Is(err, errVendorPartition) {
		t.Fatalf("путь, выпавший из разбиения, обязан быть ОТКАЗОМ, получено: %v", err)
	}
}

// TestRetiredVendorCeiling_EmptyWalkIsNotAVerdict — проверка ПРЕДПОСЫЛКИ.
//
// Обход, не принёсший файлов, и дерево, не обойдённое вовсе, дают ОТКАЗ, а не
// «находок ноль»: третья категория не растворяется в зелёном.
func TestRetiredVendorCeiling_EmptyWalkIsNotAVerdict(t *testing.T) {
	t.Parallel()

	empty := vendorFixture(nil, nil)
	empty[vendorTreeFoundation] = vendorCorpusOf(nil, nil, nil, nil, 0)
	if _, _, _, err := judgeRetiredVendorCeiling(empty, vendorZeroCeilings); !errors.Is(err, errVendorEmptyWalk) {
		t.Fatalf("пустой обход дерева обязан быть отказом, получено: %v", err)
	}

	missing := vendorFixture(nil, nil)
	delete(missing, vendorTreeAccess)
	if _, _, _, err := judgeRetiredVendorCeiling(missing, vendorZeroCeilings); !errors.Is(err, errVendorEmptyWalk) {
		t.Fatalf("необойдённое дерево обязано быть отказом, получено: %v", err)
	}

	if _, _, _, err := judgeRetiredVendorCeiling(vendorFixture(nil, nil), map[string]int{}); !errors.Is(err, errVendorLedger) {
		t.Fatalf("ноль объявленных потолков обязан быть отказом, получено: %v", err)
	}
}

func axisOf(b []vendorBinding) string {
	if len(b) == 0 {
		return "(находок нет)"
	}
	return b[0].Axis
}

// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА СЛОВА. Пары однофактные: слева и справа одна и та же строка, и
// различает их ровно то, СТРОЧНАЯ ли буква продолжает имя издателя.

// TestRetiredVendorCeiling_WordBoundaryInjection_EnglishWordIsNotABinding —
// английское слово семейства `hydrate` привязкой НЕ является.
func TestRetiredVendorCeiling_WordBoundaryInjection_EnglishWordIsNotABinding(t *testing.T) {
	t.Parallel()
	for _, line := range []string{
		"const hydrate = (s) => s;",
		"export function hydrated(x) { return x; }",
		"  setHydrated(true);",
		"func hydrateStringListFields(v any) {}",
		"def dehydrate(obj): pass",
		"  await rehydration(store);",
		"if (!isHydrated) return null;",
	} {
		got, bindings := judgeOne(t, vendorFixture(map[string]string{
			"ui-future/shared/src/lib/store.ts": line + "\n"}, nil))
		if got != 0 {
			t.Errorf("строка %q засчитана привязкой (%d): %v.\nАнглийское слово, в "+
				"которое втекло имя издателя, снимается не снятием издателя, а чужим "+
				"рефактором — пока оно считается, предикат «ноль» невыполним",
				line, got, bindings)
		}
	}
}

// TestRetiredVendorCeiling_WordBoundaryTwin_NameAtABoundaryIsABinding — ЗАКОННЫЙ
// БЛИЗНЕЦ: то же место, но имя стоит отдельным словом — привязка.
//
// Пара к предыдущей пробе неделима: без неё «ноль находок» было бы неотличимо
// от распознавателя, ослепшего на всё имя сразу.
func TestRetiredVendorCeiling_WordBoundaryTwin_NameAtABoundaryIsABinding(t *testing.T) {
	t.Parallel()
	for _, line := range []string{
		"const hydraClaims = decode(token);",   // шов camelCase
		"  kratosURL: string;",                 // шов camelCase, прописные
		"KACHO_HYDRA_ADMIN_URL: https://x",     // SNAKE_CASE
		"image: oryd/hydra:v2",                 // пространство образов
		"  host: hydra-admin.svc",              // kebab-case
		"stub := stubHydra(t)",                 // имя вторым корнем, конец слова
		"  - /etc/config/hydra.yaml",           // точка
		"select hydra_client_id from clients;", // подчёркивание
		"cookie := \"ory_kratos_session\"",     // подчёркивание с обеих сторон
		"const c = kratos2Client;",             // цифра
	} {
		got, _ := judgeOne(t, vendorFixture(map[string]string{
			"deploy/helm/umbrella/values.y.yaml": line + "\n"}, nil))
		if got != 1 {
			t.Errorf("строка %q дала привязок %d, ожидалась 1 — граница слова снесла "+
				"настоящую привязку", line, got)
		}
	}
}

// TestRetiredVendorCeiling_WordBoundaryInjection_ForeignLockEntryIsNotABinding —
// запись чужого пакета в файле блокировок консоли.
//
// ПРЕДМЕТ: пока она считалась, «ноль» был недостижим никакой нашей работой —
// её снимает обновление чужой зависимости, а не снятие издателя.
func TestRetiredVendorCeiling_WordBoundaryInjection_ForeignLockEntryIsNotABinding(t *testing.T) {
	t.Parallel()
	lock := "    \"@radix-ui/react-use-is-hydrated\": \"0.1.3\",\n" +
		"    \"node_modules/@radix-ui/react-use-is-hydrated\": {\n" +
		"      \"resolved\": \"https://registry.npmjs.org/@radix-ui/react-use-is-hydrated/-/" +
		"react-use-is-hydrated-0.1.3.tgz\",\n"
	got, bindings := judgeOne(t, vendorFixture(map[string]string{
		"ui-future/package-lock.json": lock}, nil))
	if got != 0 {
		t.Errorf("записи чужого пакета засчитаны привязками (%d): %v", got, bindings)
	}
}

// TestRetiredVendorCeiling_WordBoundaryTwin_LockEntryNamingTheIssuerIsABinding —
// ЗАКОННЫЙ БЛИЗНЕЦ: тот же файл блокировок, но пакет НАЗЫВАЕТ издателя.
func TestRetiredVendorCeiling_WordBoundaryTwin_LockEntryNamingTheIssuerIsABinding(t *testing.T) {
	t.Parallel()
	got, _ := judgeOne(t, vendorFixture(map[string]string{
		"ui-future/package-lock.json": "    \"@ory/hydra-client\": \"2.2.0\",\n"}, nil))
	if got != 1 {
		t.Errorf("запись пакета издателя дала привязок %d, ожидалась 1 — граница слова "+
			"вывела из-под суда настоящую зависимость от издателя", got)
	}
}

// TestRetiredVendorCeiling_WordBoundaryInjection_PathOfAnEnglishWordIsSilent —
// та же граница на оси ПУТИ: каталог чужого пакета не привязка.
func TestRetiredVendorCeiling_WordBoundaryInjection_PathOfAnEnglishWordIsSilent(t *testing.T) {
	t.Parallel()
	got, bindings := judgeOne(t, vendorFixture(map[string]string{
		"ui-future/vendor/react-use-is-hydrated/index.js": "export default 1;\n"}, nil))
	if got != 0 {
		t.Errorf("путь чужого пакета засчитан привязкой (%d): %v", got, bindings)
	}
}

// TestRetiredVendorCeiling_WordBoundaryTwin_PathNamingTheIssuerIsABinding —
// ЗАКОННЫЙ БЛИЗНЕЦ оси пути.
func TestRetiredVendorCeiling_WordBoundaryTwin_PathNamingTheIssuerIsABinding(t *testing.T) {
	t.Parallel()
	got, bindings := judgeOne(t, vendorFixture(map[string]string{
		"deploy/helm/umbrella/charts/hydra-2.0.0/values.yaml": "x: 1\n"}, nil))
	if got != 1 {
		t.Errorf("путь чарта издателя дал привязок %d, ожидалась 1: %v", got, bindings)
	}
}

// TestRetiredVendorCeiling_WordBoundaryCostIsNamedByTheCensus — ЦЕНА границы
// названа числом и СЛОВАМИ.
//
// Слитное написание строчными — настоящая привязка, которую граница отбрасывает.
// Замолчать это нельзя: перепись печатает счётчик и разные слова, и завтрашнее
// такое слово видно сразу.
func TestRetiredVendorCeiling_WordBoundaryCostIsNamedByTheCensus(t *testing.T) {
	t.Parallel()
	corpora := vendorFixture(map[string]string{
		"ui-future/shared/src/lib/store.ts": "const hydrate = 1;\nconst hydraadminurl = 2;\n",
	}, nil)
	_, census, _, err := judgeRetiredVendorCeiling(corpora, vendorZeroCeilings)
	if err != nil {
		t.Fatalf("судья: %v", err)
	}
	c := census[vendorTreePlatform]
	if c.BoundaryDropped != 2 {
		t.Errorf("отброшено границей %d строк, ожидалось 2 — цена границы не считается, "+
			"и слитное написание пропало бы молча", c.BoundaryDropped)
	}
	want := map[string]bool{"hydrate": true, "hydraadminurl": true}
	if len(c.BoundaryWords) != 2 || !want[c.BoundaryWords[0]] || !want[c.BoundaryWords[1]] {
		t.Errorf("слова границы %v, ожидались hydrate и hydraadminurl — без перечня слов "+
			"число ничего не объясняет", c.BoundaryWords)
	}
}

// TestRetiredVendorCeiling_BoundaryCensusIsSilentOnACleanTree — ЗАКОННЫЙ
// БЛИЗНЕЦ переписи границы: дерево без английской родни не даёт ни одного
// отброшенного.
func TestRetiredVendorCeiling_BoundaryCensusIsSilentOnACleanTree(t *testing.T) {
	t.Parallel()
	corpora := vendorFixture(map[string]string{
		"deploy/helm/umbrella/values.y.yaml": "image: oryd/hydra:v2\n",
	}, nil)
	_, census, _, err := judgeRetiredVendorCeiling(corpora, map[string]int{
		vendorTreePlatform: 1, vendorTreeAccess: 0, vendorTreeFoundation: 0})
	if err != nil {
		t.Fatalf("судья: %v", err)
	}
	if c := census[vendorTreePlatform]; c.BoundaryDropped != 0 || len(c.BoundaryWords) != 0 {
		t.Errorf("на дереве без английской родни отброшено %d строк (%v) — счётчик "+
			"границы срабатывает на чём угодно", c.BoundaryDropped, c.BoundaryWords)
	}
}

// TestRetiredVendorCeiling_ZeroIsReachableInPrinciple — ПРЕДИКАТ «НОЛЬ»
// ВЫПОЛНИМ.
//
// Дерево, где издателя нет ни в одном виде, но ЕСТЬ его английская родня и
// чужое вендоренное, обязано дать ноль привязок при нулевом потолке — и это
// зелёный без находок. Пока родня считалась, такого дерева не существовало:
// ноль был недостижим никакой работой по снятию.
func TestRetiredVendorCeiling_ZeroIsReachableInPrinciple(t *testing.T) {
	t.Parallel()
	corpora := vendorFixture(map[string]string{
		"ui-future/package-lock.json":       "    \"@radix-ui/react-use-is-hydrated\": \"0.1.3\",\n",
		"ui-future/shared/src/lib/store.ts": "const hydrate = (s) => s;\nsetHydrated(true);\n",
		"vendor/cert-manager/values.yaml":   "replicas: 2\n",
		"services/iam/internal/token.go":    "package token\n\nconst issuer = \"kacho\"\n",
	}, nil)
	findings, census, bindings, err := judgeRetiredVendorCeiling(corpora, vendorZeroCeilings)
	if err != nil {
		t.Fatalf("судья: %v", err)
	}
	if len(bindings) != 0 {
		t.Fatalf("привязок %d при нуле ожидаемых: %v", len(bindings), bindings)
	}
	if len(findings) != 0 {
		t.Fatalf("находок %d при нулевом потолке и нуле привязок: %v", len(findings), findings)
	}
	c := census[vendorTreePlatform]
	if c.Files == 0 {
		t.Fatal("ноль судимых файлов — «ноль привязок» здесь означало бы «ноль прочитанного»")
	}
	t.Logf("ноль достижим: судимо файлов %d · привязок 0 · отброшено границей %d строк (%v)",
		c.Files, c.BoundaryDropped, c.BoundaryWords)
}

// ─────────────────────────────────────────────────────────────────────────────
// СМЕЩЕНИЕ ИЩЕТСЯ В ОДНОМ ТЕКСТЕ, БАЙТ ГРАНИЦЫ ЧИТАЕТСЯ В ДРУГОМ

// TestRetiredVendorCeiling_LoweringPreservesLength — приведение, которым ищется
// смещение, обязано СОХРАНЯТЬ ДЛИНУ.
//
// Пара к ней — положительный контроль на библиотечном приведении: он показывает,
// что расхождение длин не выдумано, а существует на том же входе.
func TestRetiredVendorCeiling_LoweringPreservesLength(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"\xffHYDRAte",     // невалидный байт: библиотечное растит его втрое
		"\xff\xffKRATOSx", // два невалидных байта подряд
		"İHYDRA",          // прописная I с точкой: две руны в нижнем регистре
		"обычная строка с hydra-admin", // кириллица без смещения — контроль
	} {
		if got, want := len(vendorASCIILower(in)), len(in); got != want {
			t.Errorf("приведение по байтам изменило длину %q: %d против %d — смещение, "+
				"найденное в приведённом тексте, указывает в исходном мимо", in, got, want)
		}
	}
	// Положительный контроль: библиотечное приведение длину НЕ сохраняет, иначе
	// эта проба зеленела бы и на сломанном коде.
	drifted := 0
	for _, in := range []string{"\xffHYDRAte", "\xff\xffKRATOSx", "İHYDRA"} {
		if len(strings.ToLower(in)) != len(in) {
			drifted++
		}
	}
	if drifted == 0 {
		t.Fatal("библиотечное приведение на всех трёх входах длину сохранило — " +
			"положительный контроль беспредметен, и зелёное выше ничего не значит")
	}
	t.Logf("контроль: библиотечное приведение сместило длину на %d входах из 3", drifted)
}

// TestRetiredVendorCeiling_OffsetInjection_InvalidByteDoesNotMoveTheVerdict —
// вердикт не зависит от невалидного байта ПЕРЕД именем.
//
// Пара однофактная: те же две строки без ведущего байта и с ним.
func TestRetiredVendorCeiling_OffsetInjection_InvalidByteDoesNotMoveTheVerdict(t *testing.T) {
	t.Parallel()
	pairs := []struct {
		clean, dirty string
		want         bool
		why          string
	}{
		{"HYDRAte", "\xffHYDRAte", false, "английское слово прописными продолжено строчной"},
		{"hydra-admin", "\xffhydra-admin", true, "имя отдельным словом"},
		{"hydrate", "\xff\xffhydrate", false, "английское слово"},
	}
	for _, p := range pairs {
		if got := vendorMarkBoundedIn(p.clean); got != p.want {
			t.Errorf("чистый вход %q дал %v, ожидалось %v (%s)", p.clean, got, p.want, p.why)
		}
		if got := vendorMarkBoundedIn(p.dirty); got != p.want {
			t.Errorf("тот же вход с невалидным байтом впереди (%q) дал %v, ожидалось %v — "+
				"смещение уехало, и граница прочитана по чужому байту", p.dirty, got, p.want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ВТОРАЯ ЦЕНА ГРАНИЦЫ

// TestRetiredVendorCeiling_UpperCostIsNamedByTheCensus — ложное СРАБАТЫВАНИЕ
// названо числом и словами, как и ложное отрицание.
func TestRetiredVendorCeiling_UpperCostIsNamedByTheCensus(t *testing.T) {
	t.Parallel()
	corpora := vendorFixture(map[string]string{
		"deploy/helm/umbrella/values.y.yaml": "X_HYDRATE_Y: 1\nimage: oryd/hydra:v2\n",
	}, nil)
	_, census, _, err := judgeRetiredVendorCeiling(corpora, map[string]int{
		vendorTreePlatform: 2, vendorTreeAccess: 0, vendorTreeFoundation: 0})
	if err != nil {
		t.Fatalf("судья: %v", err)
	}
	c := census[vendorTreePlatform]
	if c.UpperKept != 1 {
		t.Errorf("засчитано именем прописными с прописной следом %d строк, ожидалась 1 — "+
			"вторая цена границы не считается, и перепись, честная в одну сторону, "+
			"читается как честная целиком", c.UpperKept)
	}
	if len(c.UpperWords) != 1 || c.UpperWords[0] != "X_HYDRATE_Y" {
		t.Errorf("слова второй цены %v, ожидалось X_HYDRATE_Y — без перечня слов число "+
			"ничего не объясняет", c.UpperWords)
	}
}

// TestRetiredVendorCeiling_CamelCaseIsNotTheAmbiguousForm — ЗАКОННЫЙ БЛИЗНЕЦ
// второй цены: шов camelCase спорным случаем НЕ является.
//
// Без этой пары счётчик второй цены считал бы каждую настоящую привязку и стал
// бы шумом, в котором единственный спорный случай не виден.
func TestRetiredVendorCeiling_CamelCaseIsNotTheAmbiguousForm(t *testing.T) {
	t.Parallel()
	corpora := vendorFixture(map[string]string{
		"ui-future/shared/src/lib/a.ts": "const hydraClaims = 1;\nconst x = HydraIssuer;\n" +
			"const y = kratosURL;\n",
	}, nil)
	_, census, _, err := judgeRetiredVendorCeiling(corpora, map[string]int{
		vendorTreePlatform: 3, vendorTreeAccess: 0, vendorTreeFoundation: 0})
	if err != nil {
		t.Fatalf("судья: %v", err)
	}
	if c := census[vendorTreePlatform]; c.UpperKept != 0 || len(c.UpperWords) != 0 {
		t.Errorf("шов camelCase назван спорным случаем: %d строк, слова %v — счётчик "+
			"второй цены стал бы шумом", c.UpperKept, c.UpperWords)
	}
}

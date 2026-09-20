// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"errors"
	"strings"
	"testing"
)

// Инъекция в обе стороны. ПАРЫ ОДНОФАКТНЫЕ: дефект и законный близнец
// различаются РОВНО ОДНИМ фактом, и полярность утверждения в этот факт не
// входит — пара с двумя различиями законна по двум независимым причинам и не
// показывает, на что гейт смотрит.
//
// Каждая пара названа своим фактом.

// vendorFixture — корпус трёх деревьев: одна строка предмета в платформе, пустые
// (но НЕ отсутствующие) фундамент и служба доступа.
func vendorFixture(platform map[string]string, archives []vendorArchive) map[string]vendorTreeCorpus {
	base := map[string]string{"deploy/helm/umbrella/values.x.yaml": "a: 1\nb: 2\n"}
	for k, v := range platform {
		base[k] = v
	}
	return map[string]vendorTreeCorpus{
		vendorTreePlatform:   {Bodies: base, Archives: archives, LinesRead: 3},
		vendorTreeAccess:     {Bodies: map[string]string{"go.mod": "module x\n"}, LinesRead: 2},
		vendorTreeFoundation: {Bodies: map[string]string{"go.mod": "module y\n"}, LinesRead: 2},
	}
}

var vendorZeroCeilings = map[string]int{
	vendorTreePlatform:   0,
	vendorTreeAccess:     0,
	vendorTreeFoundation: 0,
}

// judgeOne — сколько привязок нашлось в платформе и первая из них.
func judgeOne(t *testing.T, corpora map[string]vendorTreeCorpus) (int, []vendorBinding) {
	t.Helper()
	_, census, bindings, err := judgeRetiredVendorCeiling(corpora, vendorZeroCeilings)
	if err != nil {
		t.Fatalf("фикстура обязана судиться: %v", err)
	}
	return census[vendorTreePlatform].Bindings, bindings
}

// TestRetiredVendorCeiling_NameAxisInjection — ОДИН ФАКТ: имя издателя в строке.
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

// TestRetiredVendorCeiling_ProseIsSilent — ДВЕ пары, в каждой ровно один факт.
//
//	факт 1: расширение файла — одна и та же строка в `.yaml` и в `.md`;
//	факт 2: маркер комментария в начале строки — одна и та же строка с ним и без.
//
// Проза не привязывает дерево к издателю, а утверждение о нём как о действующем
// судит соседнее надгробие `retiredissuerclaim.go`. Давить числом на надгробия
// значило бы требовать стереть историю.
func TestRetiredVendorCeiling_ProseIsSilent(t *testing.T) {
	t.Parallel()

	const line = "hydraAdminURL: https://provider-admin.kacho.svc"

	if n, _ := judgeOne(t, vendorFixture(map[string]string{"deploy/notes.yaml": line + "\n"}, nil)); n != 1 {
		t.Fatalf("строка исполняемого файла обязана краснеть: привязок %d", n)
	}
	if m, _ := judgeOne(t, vendorFixture(map[string]string{"deploy/notes.md": line + "\n"}, nil)); m != 0 {
		t.Fatalf("та же строка в прозе обязана молчать: привязок %d", m)
	}
	if m, _ := judgeOne(t, vendorFixture(map[string]string{"deploy/notes.yaml": "# " + line + "\n"}, nil)); m != 0 {
		t.Fatalf("та же строка под маркером комментария обязана молчать: привязок %d", m)
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

	defect := []vendorArchive{{Name: "deploy/helm/umbrella/charts/hydra-0.62.1.tgz", Text: renamedInside}}
	n, bindings := judgeOne(t, vendorFixture(nil, defect))
	if n != 1 || bindings[0].Axis != vendorAxisArchiveName {
		t.Fatalf("архив издателя обязан краснеть по ИМЕНИ при переименованном содержимом: "+
			"привязок %d, ось %q", n, axisOf(bindings))
	}

	twin := []vendorArchive{{Name: "deploy/helm/umbrella/charts/postgresql-13.4.4.tgz", Text: renamedInside}}
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

	defect := []vendorArchive{{Name: name, Text: "idp/values.yaml\nimage: oryd/kratos:v1.3.1\n"}}
	n, bindings := judgeOne(t, vendorFixture(nil, defect))
	if n != 1 || bindings[0].Axis != vendorAxisArchiveBody {
		t.Fatalf("архив издателя обязан краснеть по СОДЕРЖИМОМУ при нейтральном имени: "+
			"привязок %d, ось %q", n, axisOf(bindings))
	}

	twin := []vendorArchive{{Name: name, Text: "idp/values.yaml\nimage: bitnami/postgresql:16\n"}}
	if m, _ := judgeOne(t, vendorFixture(nil, twin)); m != 0 {
		t.Fatalf("архив без предмета обязан молчать: привязок %d", m)
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
	defect[vendorTreeFoundation] = vendorTreeCorpus{
		Bodies:    map[string]string{"identity/session.go": "package identity\n\nconst cookie = \"ory_kratos_session\"\n"},
		LinesRead: 4,
	}
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
	twin[vendorTreeFoundation] = vendorTreeCorpus{
		Bodies:    map[string]string{"identity/session.go": "package identity\n\nconst cookie = \"kacho_session\"\n"},
		LinesRead: 4,
	}
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

// TestRetiredVendorCeiling_EmptyWalkIsNotAVerdict — проверка ПРЕДПОСЫЛКИ.
//
// Обход, не принёсший файлов, и дерево, не обойдённое вовсе, дают ОТКАЗ, а не
// «находок ноль»: третья категория не растворяется в зелёном.
func TestRetiredVendorCeiling_EmptyWalkIsNotAVerdict(t *testing.T) {
	t.Parallel()

	empty := vendorFixture(nil, nil)
	empty[vendorTreeFoundation] = vendorTreeCorpus{Bodies: map[string]string{}}
	if _, _, _, err := judgeRetiredVendorCeiling(empty, vendorZeroCeilings); !errors.Is(err, errVendorEmptyWalk) {
		t.Fatalf("пустой обход дерева обязан быть отказом, получено: %v", err)
	}

	missing := vendorFixture(nil, nil)
	delete(missing, vendorTreeAccess)
	if _, _, _, err := judgeRetiredVendorCeiling(missing, vendorZeroCeilings); !errors.Is(err, errVendorEmptyWalk) {
		t.Fatalf("необойдённое дерево обязано быть отказом, получено: %v", err)
	}

	if _, _, _, err := judgeRetiredVendorCeiling(vendorFixture(nil, nil), map[string]int{}); !errors.Is(err, errVendorEmptyWalk) {
		t.Fatalf("ноль объявленных потолков обязан быть отказом, получено: %v", err)
	}
}

func axisOf(b []vendorBinding) string {
	if len(b) == 0 {
		return "(находок нет)"
	}
	return b[0].Axis
}

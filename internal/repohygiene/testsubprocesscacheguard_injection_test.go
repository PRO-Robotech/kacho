// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// testsubprocesscacheguard_injection_test.go — доказательство гейта инъекцией В
// ОБЕ СТОРОНЫ на синтетике.
//
// Инъекция ставится на синтетическом корпусе, а не на живом дереве: живое
// дерево меняется чужими полосами, и проба, привязанная к нему, краснела бы от
// чужой правки. Каждый случай меняет РОВНО ОДИН факт против базового корпуса —
// иначе красное приходило бы от соседа.
//
// Базовый корпус несёт по одному месту на КАЖДУЮ запись словаря: иначе
// самоистечение словаря давало бы находки в каждом случае и заглушало предмет.
package repohygiene_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/repohygiene"
)

// baseSynthCorpus — законный близнец: страж стоит у самого места запуска, все
// записи словаря представлены. Находок обязано быть НОЛЬ.
func baseSynthCorpus() map[string]string {
	return map[string]string{
		"synth/render_test.go": `package probe

import (
	"os/exec"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/cachedverdict"
)

func render(t *testing.T) string {
	if msg := cachedverdict.SubprocessRefusal("helm"); msg != "" {
		t.Fatal(msg)
	}
	out, _ := exec.Command("helm", "template", ".").CombinedOutput()
	return string(out)
}

func TestRenderCarriesTheHost(t *testing.T) {
	if !strings.Contains(render(t), "kacho-geo") {
		t.Fatal("нет")
	}
}

func TestEveryOtherToolOfTheCanon(t *testing.T) {
	_, _ = exec.Command("go", "env", "GOMODCACHE").Output()
	_, _ = exec.Command("bash", "x.sh").Output()
	_, _ = exec.Command("python3", "gen.py").Output()
	_, _ = exec.Command("gh", "issue", "list").Output()
	_, _ = exec.Command("make", "target").Output()
	_, _ = exec.Command("jq", "-r", ".").Output()
}
`,
	}
}

func auditSynth(t *testing.T, corpus map[string]string) []string {
	t.Helper()
	findings, cen := repohygiene.AuditTestSubprocessCacheGuards(corpus)
	if cen.Sites == 0 {
		t.Fatalf("синтетика не дала ни одного места запуска — инъекция ничего не проверила бы; перепись: %s", cen.Line())
	}
	return findings
}

// TestSubprocessGuardInjection_LawfulTwinIsSilent — КОНТРОЛЬ. Без него всякое
// красное ниже означало бы только «гейт краснеет на чём угодно».
func TestSubprocessGuardInjection_LawfulTwinIsSilent(t *testing.T) {
	t.Parallel()
	if f := auditSynth(t, baseSynthCorpus()); len(f) != 0 {
		t.Fatalf("законный близнец дал находки:\n%s", strings.Join(f, "\n"))
	}
}

func mutate(t *testing.T, old, new string) map[string]string {
	t.Helper()
	c := baseSynthCorpus()
	src := c["synth/render_test.go"]
	if !strings.Contains(src, old) {
		t.Fatalf("инъекция не нашла своего места: %q отсутствует в базовом корпусе", old)
	}
	c["synth/render_test.go"] = strings.Replace(src, old, new, 1)
	return c
}

const guardBlock = `	if msg := cachedverdict.SubprocessRefusal("helm"); msg != "" {
		t.Fatal(msg)
	}
`

// TestSubprocessGuardInjection_GuardRemovedIsFoundWithItsCoordinate — настоящий
// дефект: страж снят, всё остальное на месте.
func TestSubprocessGuardInjection_GuardRemovedIsFoundWithItsCoordinate(t *testing.T) {
	t.Parallel()
	f := auditSynth(t, mutate(t, guardBlock, ""))
	if len(f) == 0 {
		t.Fatal("страж снят, а гейт промолчал")
	}
	joined := strings.Join(f, "\n")
	for _, want := range []string{"synth/render_test.go:", "TestRenderCarriesTheHost", "render", `"helm"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("находка не называет %q:\n%s", want, joined)
		}
	}
}

// TestSubprocessGuardInjection_GuardAfterTheLaunchIsFound — порядок несущий:
// страж ПОСЛЕ запуска отказывает позже вердикта, то есть не отказывает.
func TestSubprocessGuardInjection_GuardAfterTheLaunchIsFound(t *testing.T) {
	t.Parallel()
	moved := mutate(t, guardBlock, "")
	src := moved["synth/render_test.go"]
	moved["synth/render_test.go"] = strings.Replace(src,
		`	return string(out)`, guardBlock+`	return string(out)`, 1)
	if f := auditSynth(t, moved); len(f) == 0 {
		t.Fatal("страж стоит ПОСЛЕ запуска, а гейт промолчал — порядок им не читается")
	}
}

// TestSubprocessGuardInjection_GuardThatDoesNotRefuseIsFound — предикат есть,
// отказа в теле нет: печать вместо падения зелёного не отменяет.
func TestSubprocessGuardInjection_GuardThatDoesNotRefuseIsFound(t *testing.T) {
	t.Parallel()
	c := mutate(t, "\t\tt.Fatal(msg)\n", "\t\tt.Log(msg)\n")
	if f := auditSynth(t, c); len(f) == 0 {
		t.Fatal("страж только печатает, а гейт засчитал его за отказ")
	}
}

// TestSubprocessGuardInjection_GuardAtTheEntryPointIsSilent — ВТОРАЯ законная
// форма: страж в точке входа, запуск глубже по графу вызовов. Так написан
// образец дерева (services/compute/cmd/compute/deploy_geo_edge_render_test.go:
// страж первым оператором TestGeoEdge_HelmRender_GeoEnvPresent), и гейт обязан
// на нём молчать.
func TestSubprocessGuardInjection_GuardAtTheEntryPointIsSilent(t *testing.T) {
	t.Parallel()
	c := mutate(t, guardBlock, "")
	src := c["synth/render_test.go"]
	src = strings.Replace(src, `func TestRenderCarriesTheHost(t *testing.T) {`,
		`func TestRenderCarriesTheHost(t *testing.T) {
	if treecorpus.RunResultWillBeCached() {
		t.Fatalf("рендер строит ПОДПРОЦЕСС: %s", treecorpus.CachedVerdictRefusal())
	}`, 1)
	c["synth/render_test.go"] = src
	f := auditSynth(t, c)
	for _, line := range f {
		if strings.Contains(line, "TestRenderCarriesTheHost") {
			t.Errorf("страж в точке входа не засчитан — форма образца дерева гейту неизвестна:\n%s", line)
		}
	}
}

// TestSubprocessGuardInjection_LookPathFormIsRecognised — третья форма записи
// программы: имя приходит из exec.LookPath. Литеральный поиск её не видит, и
// место запуска было бы гейту невидимо — ни красного, ни зелёного.
func TestSubprocessGuardInjection_LookPathFormIsRecognised(t *testing.T) {
	t.Parallel()
	c := mutate(t, guardBlock, "")
	src := c["synth/render_test.go"]
	src = strings.Replace(src,
		`	out, _ := exec.Command("helm", "template", ".").CombinedOutput()`,
		`	bin, _ := exec.LookPath("helm")
	out, _ := exec.Command(bin, "template", ".").CombinedOutput()`, 1)
	c["synth/render_test.go"] = src
	if f := auditSynth(t, c); !strings.Contains(strings.Join(f, "\n"), `"helm"`) {
		t.Fatalf("форма exec.LookPath гейту невидима — место запуска не судится вовсе:\n%s", strings.Join(f, "\n"))
	}
}

// TestSubprocessGuardInjection_UnknownToolIsAFinding — словарь ЗАКРЫТ: новый
// инструмент не проскакивает потому, что о нём не вспомнили.
func TestSubprocessGuardInjection_UnknownToolIsAFinding(t *testing.T) {
	t.Parallel()
	c := mutate(t, `exec.Command("jq", "-r", ".")`, `exec.Command("kubectl", "get", "pods")`)
	joined := strings.Join(auditSynth(t, c), "\n")
	if !strings.Contains(joined, "kubectl") {
		t.Errorf("неизвестный инструмент прошёл молча:\n%s", joined)
	}
	if !strings.Contains(joined, `"jq"`) {
		t.Errorf("запись словаря, которой нечего исключать, не названа — самоистечения нет:\n%s", joined)
	}
}

// TestSubprocessGuardInjection_MethodIsJudgedOnItsOwn — место запуска внутри
// МЕТОДА не теряется: ребра вызова метода гейт не строит, поэтому страж
// требуется в самом методе.
func TestSubprocessGuardInjection_MethodIsJudgedOnItsOwn(t *testing.T) {
	t.Parallel()
	c := baseSynthCorpus()
	c["synth/fixture_test.go"] = `package probe

import "os/exec"

type fixture struct{ dir string }

func (f fixture) render() []byte {
	out, _ := exec.Command("helm", "template", f.dir).CombinedOutput()
	return out
}
`
	joined := strings.Join(auditSynth(t, c), "\n")
	if !strings.Contains(joined, "fixture.render") {
		t.Errorf("запуск внутри метода гейту невидим — слепая зона:\n%s", joined)
	}
}

// TestSubprocessGuardPremise_EmptyWalkIsNotAVerdict — пустой обход обязан быть
// ОТКАЗОМ, а не чистотой. Проверяется на синтетике переписи: страж предпосылки,
// который нельзя позвать, сам никогда не проверялся.
func TestSubprocessGuardPremise_EmptyWalkIsNotAVerdict(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cen  repohygiene.SubprocessGuardCensus
		want string
	}{
		{"обход пуст", repohygiene.SubprocessGuardCensus{}, "ноль файлов"},
		{"файлы есть, мест нет", repohygiene.SubprocessGuardCensus{FilesRead: 10}, "ноль при непустом обходе"},
		{"места есть, предмета нет", repohygiene.SubprocessGuardCensus{FilesRead: 10, Sites: 3}, "требующих стража, ноль"},
	}
	for _, c := range cases {
		got := c.cen.PremiseFailure()
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: предпосылка промолчала (%q), ожидалось упоминание %q", c.name, got, c.want)
		}
	}
	held := repohygiene.SubprocessGuardCensus{FilesRead: 10, Sites: 3, NeedGuard: 1}
	if msg := held.PremiseFailure(); msg != "" {
		t.Errorf("на держащейся предпосылке страж обязан молчать, а он сказал: %s", msg)
	}
}

// requireHelmHead — помощник, отказывающий НА ВХОДЕ: страж первым, до всего
// остального. Так написан `services/registry/deploy/render_test.go`.
const requireHelmHead = `
func requireHelm(t *testing.T) {
	if msg := cachedverdict.SubprocessRefusal("helm"); msg != "" {
		t.Fatal(msg)
	}
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("нет helm")
	}
}
`

// requireHelmDeep — тот же помощник, но страж УТОПЛЕН в ветку: исполнится он
// или нет, неизвестно. Один факт против близнеца выше.
const requireHelmDeep = `
func requireHelm(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("нет helm")
	}
	if msg := cachedverdict.SubprocessRefusal("helm"); msg != "" {
		t.Fatal(msg)
	}
}
`

// withFactoredGuard — базовый корпус, где страж вынесен в помощника, а рендер
// зовёт его перед запуском.
func withFactoredGuard(t *testing.T, helper string) map[string]string {
	t.Helper()
	c := mutate(t, guardBlock, "\trequireHelm(t)\n")
	c["synth/render_test.go"] += helper
	return c
}

// TestSubprocessGuardInjection_GuardFactoredIntoAHelperIsSilent — вынесенный
// страж засчитывается. Без этого гейт краснел бы на исправной пробе, а автор
// снимал бы вынос ради зелёного.
func TestSubprocessGuardInjection_GuardFactoredIntoAHelperIsSilent(t *testing.T) {
	t.Parallel()
	if f := auditSynth(t, withFactoredGuard(t, requireHelmHead)); len(f) != 0 {
		t.Fatalf("вынесенный страж не засчитан:\n%s", strings.Join(f, "\n"))
	}
}

// TestSubprocessGuardInjection_GuardSunkIntoABranchOfTheHelperIsFound — и тот
// же помощник со стражем в глубине ветки засчитан НЕ БЫВАЕТ.
func TestSubprocessGuardInjection_GuardSunkIntoABranchOfTheHelperIsFound(t *testing.T) {
	t.Parallel()
	if f := auditSynth(t, withFactoredGuard(t, requireHelmDeep)); len(f) == 0 {
		t.Fatal("страж утоплен в ветку помощника, а гейт засчитал его за отказ на входе")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// A4. Каталог держит ДВА пакета
// ─────────────────────────────────────────────────────────────────────────────

// externalPackageLaunch — второй пакет ТОГО ЖЕ каталога (`probe_test` рядом с
// `probe`) с НЕЗАЩИЩЁННЫМ запуском в функции, одноимённой функции первого
// пакета. Имя файла выбрано так, чтобы он читался ПОСЛЕ `render_test.go`:
// иначе отбрасывался бы защищённый близнец, и дефект проявлялся бы сам собой.
const externalPackageLaunch = `package probe_test

import (
	"os/exec"
	"testing"
)

func render(t *testing.T) string {
	out, _ := exec.Command("helm", "template", ".").CombinedOutput()
	return string(out)
}

func TestExternalRenderCarriesTheHost(t *testing.T) {
	if render(t) == "" {
		t.Fatal("нет")
	}
}
`

// TestSubprocessGuardInjection_SecondPackageOfTheSameDirectoryIsWalked —
// одноимённое объявление во ВТОРОМ пакете каталога не отбрасывается молча.
//
// Каталог в Go держит два пакета: `x` и внешний `x_test`. Обход группирует
// файлы по КАТАЛОГУ, поэтому одноимённые объявления двух пакетов попадали в
// один ключ, и второе отбрасывалось — вместе со своими местами запуска. Исход
// был двойным ложным зелёным: находок ноль, и место запуска не попадало даже в
// перепись.
//
// Проверяется и то и другое: перепись обязана знать ОБА места (`helm×2`), а
// находка — назвать координату незащищённого.
func TestSubprocessGuardInjection_SecondPackageOfTheSameDirectoryIsWalked(t *testing.T) {
	t.Parallel()
	c := baseSynthCorpus()
	c["synth/zz_external_test.go"] = externalPackageLaunch

	findings, cen := repohygiene.AuditTestSubprocessCacheGuards(c)
	if got := cen.Programs["helm"]; got != 2 {
		t.Errorf("перепись знает helm×%d, а мест запуска в корпусе ДВА — "+
			"объявление второго пакета отброшено вместе со своим местом запуска; перепись: %s",
			got, cen.Line())
	}
	joined := strings.Join(findings, "\n")
	if !strings.Contains(joined, "synth/zz_external_test.go") {
		t.Errorf("незащищённый запуск во втором пакете каталога гейту невидим:\n%s", joined)
	}
}

// TestSubprocessGuardInjection_TwinInTwoPackagesIsSilent — законный близнец той
// же формы: каталог держит два пакета, и в ОБОИХ страж стоит. Один факт против
// случая выше. Без него красное там означало бы лишь «гейт краснеет на всяком
// втором пакете».
func TestSubprocessGuardInjection_TwinInTwoPackagesIsSilent(t *testing.T) {
	t.Parallel()
	c := baseSynthCorpus()
	c["synth/zz_external_test.go"] = strings.Replace(externalPackageLaunch,
		"	out, _ := exec.Command",
		`	if msg := cachedverdict.SubprocessRefusal("helm"); msg != "" {
		t.Fatal(msg)
	}
	out, _ := exec.Command`, 1)
	if f := auditSynth(t, c); len(f) != 0 {
		t.Fatalf("оба пакета каталога защищены, а гейт дал находки:\n%s", strings.Join(f, "\n"))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// A2. Страж, стоящий ПОД УСЛОВИЕМ
// ─────────────────────────────────────────────────────────────────────────────

// TestSubprocessGuardInjection_GuardUnderACondditionIsFound — страж в ветви
// `if` не исполняется на всех путях, и за безусловный не засчитывается.
//
// Требование к помощнику уже было («страж РОВНО в голове тела»), а к
// собственному стражу функции — нет: `ast.Inspect` находил `IfStmt` на любой
// глубине. Вынести страж под `if os.Getenv(...)` и получить зелёное можно было
// одной строкой.
func TestSubprocessGuardInjection_GuardUnderAConditionIsFound(t *testing.T) {
	t.Parallel()
	c := mutate(t, guardBlock, `	if os.Getenv("STRICT") != "" {
		if msg := cachedverdict.SubprocessRefusal("helm"); msg != "" {
			t.Fatal(msg)
		}
	}
`)
	findings, cen := repohygiene.AuditTestSubprocessCacheGuards(c)
	if cen.Guarded != 0 {
		t.Errorf("перепись объявила защищёнными на всех путях %d мест, а страж стоит под условием; перепись: %s",
			cen.Guarded, cen.Line())
	}
	if len(findings) == 0 {
		t.Fatal("страж утоплен под условие, а гейт засчитал его за безусловный")
	}
}

// TestSubprocessGuardInjection_GuardUnderALoopIsFound — то же одним фактом
// иначе: тело цикла тоже не исполняется гарантированно.
func TestSubprocessGuardInjection_GuardUnderALoopIsFound(t *testing.T) {
	t.Parallel()
	c := mutate(t, guardBlock, `	for range attempts {
		if msg := cachedverdict.SubprocessRefusal("helm"); msg != "" {
			t.Fatal(msg)
		}
	}
`)
	if f := auditSynth(t, c); len(f) == 0 {
		t.Fatal("страж утоплен в цикл, а гейт засчитал его за безусловный")
	}
}

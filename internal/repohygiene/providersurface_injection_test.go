// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// providersurface_injection_test.go — доказательство, что гейт ведомости СПОСОБЕН
// упасть и способен смолчать.
//
// Инъекция в ОБЕ стороны по каждой оси: дефект обязан находиться, законный
// близнец той же формы — молчать. Без второй половины гейт ловил бы форму, а не
// существо, и первый же ложный срабат его отключил бы.

// injectFinding — есть ли среди находок нужный вид по нужному файлу и пути.
func injectFinding(fs []ProviderFinding, file, kind, surface string) bool {
	for _, f := range fs {
		if f.File == file && f.Kind == kind && (surface == "" || f.Surface == surface) {
			return true
		}
	}
	return false
}

func injectRun(t *testing.T, src map[string]string, ledger []ProviderLedgerEntry) ([]ProviderFinding, ProviderCensus) {
	t.Helper()
	fs, c, err := FindProviderSurface(src, ledger, nil)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	return fs, c
}

// TestProviderSurfaceInjection_UnledgeredReachIsFound — А: новое место разговора
// краснеет и называет координату.
func TestProviderSurfaceInjection_UnledgeredReachIsFound(t *testing.T) {
	src := map[string]string{
		"services/x/internal/clients/new_reacher.go": `package clients
func reach(base string) string { return base + "/admin/clients" }
`,
	}
	fs, c := injectRun(t, src, nil)
	if !injectFinding(fs, "services/x/internal/clients/new_reacher.go", ProviderFindingUnledgered, "/admin/clients") {
		t.Fatalf("новое место разговора не найдено: %+v", fs)
	}
	if c.Carriers != 1 || c.Reaches != 1 {
		t.Fatalf("перепись не сошлась: несущих %d, мест %d", c.Carriers, c.Reaches)
	}
	if fs[0].Line != 2 {
		t.Fatalf("координата не названа верно: строка %d, ожидалась 2", fs[0].Line)
	}
}

// TestProviderSurfaceInjection_ProseTwinIsSilent — А′: ЗАКОННЫЙ БЛИЗНЕЦ — тот же
// путь в комментарии.
//
// Разбор переезда обязан называть пути прямо (иначе он непонятен), и гейт,
// краснеющий на собственном объяснении, был бы снят первым же обходом.
func TestProviderSurfaceInjection_ProseTwinIsSilent(t *testing.T) {
	src := map[string]string{
		"services/x/internal/doc.go": `package x

// Прежде здесь ходили в /admin/clients и в /oauth2/token; обоих вызовов больше нет.
const kept = "ничего общего"
`,
	}
	fs, c := injectRun(t, src, nil)
	if len(fs) != 0 {
		t.Fatalf("проза объявлена находкой: %+v", fs)
	}
	if c.ProseMentions != 0 {
		t.Fatalf("проза без имени поставщика зачтена в счётчик прозы: %d", c.ProseMentions)
	}
	if c.Files != 1 || c.Literals == 0 {
		t.Fatalf("перепись не сошлась: файлов %d, литералов %d", c.Files, c.Literals)
	}
}

// TestProviderSurfaceInjection_OurOwnKeySetPathIsSilent — А″: ЗАКОННЫЙ БЛИЗНЕЦ —
// путь набора ключей, который отдаём МЫ САМИ.
//
// Он неоднозначен by construction: по нему отвечает и поставщик, и наше зеркало.
// Гейт, включивший его в словарь, краснел бы на нашем обработчике — то есть на
// коде, ради которого переезд и делается.
func TestProviderSurfaceInjection_OurOwnKeySetPathIsSilent(t *testing.T) {
	src := map[string]string{
		"services/iam/internal/handler/jwksproxyhttp/handler.go": `package jwksproxyhttp
const WellKnownJWKSPath = "/.well-known/jwks.json"
const OurOwn = "/.well-known/kaname/jwks.json"
`,
	}
	fs, _ := injectRun(t, src, nil)
	if len(fs) != 0 {
		t.Fatalf("собственная публикация ключей объявлена разговором с поставщиком: %+v", fs)
	}
}

// TestProviderSurfaceInjection_UndeclaredSurfaceAtALedgeredFile — Б: «ещё один
// вызов туда же» краснеет у НАЗВАННОГО места.
//
// Это и есть способ, каким поверхность растёт незамеченной: файл в ведомости
// уже стоит, обзор диффа видит одну строку.
func TestProviderSurfaceInjection_UndeclaredSurfaceAtALedgeredFile(t *testing.T) {
	const file = "services/iam/internal/clients/hydra_oauth_clients.go"
	src := map[string]string{file: `package clients
func a(b string) string { return b + "/admin/clients" }
func c(b string) string { return b + "/admin/oauth2/introspect" }
`}
	ledger := []ProviderLedgerEntry{{
		File: file, Surfaces: []string{"/admin/clients"},
		Why: "зеркало клиента", Until: "выдача переехала",
	}}
	fs, _ := injectRun(t, src, ledger)
	if !injectFinding(fs, file, ProviderFindingUndeclared, "/admin/oauth2/introspect") {
		t.Fatalf("необъявленная поверхность у названного места не найдена: %+v", fs)
	}
	if injectFinding(fs, file, ProviderFindingUnledgered, "") {
		t.Fatalf("названное место объявлено неназванным: %+v", fs)
	}
	// Обратная сторона той же оси: объявленная поверхность молчит.
	if injectFinding(fs, file, ProviderFindingUndeclared, "/admin/clients") {
		t.Fatalf("объявленная поверхность объявлена необъявленной: %+v", fs)
	}
}

// TestProviderSurfaceInjection_StaleEntryIsFound — В: запись, пережившая предмет,
// краснеет.
//
// Без этого ведомость не сокращалась бы: код сняли, запись осталась и молча
// разрешает следующий разговор в том же файле.
func TestProviderSurfaceInjection_StaleEntryIsFound(t *testing.T) {
	src := map[string]string{
		"services/iam/internal/clients/hydra_login_sessions.go": `package clients
const nothing = "уже ничего не просит"
`,
	}
	ledger := []ProviderLedgerEntry{{
		File:     "services/iam/internal/clients/hydra_login_sessions.go",
		Surfaces: []string{"/admin/oauth2/auth/sessions/login"},
		Why:      "снятие сессий", Until: "вход человека перестал заводить сессию",
	}}
	fs, _ := injectRun(t, src, ledger)
	if !injectFinding(fs, "services/iam/internal/clients/hydra_login_sessions.go", ProviderFindingStale, "") {
		t.Fatalf("пережившая предмет запись не найдена: %+v", fs)
	}
	if !strings.Contains(fs[0].Detail, "вход человека перестал заводить сессию") {
		t.Fatalf("находка не назвала предикат снятия записи: %q", fs[0].Detail)
	}
}

// TestProviderSurfaceInjection_PartlyStaleEntryIsFound — В′: снята ОДНА из двух
// объявленных поверхностей.
//
// Проверка только по файлу зеленела бы здесь и продолжала разрешать снятое.
func TestProviderSurfaceInjection_PartlyStaleEntryIsFound(t *testing.T) {
	const file = "gateway/cmd/api-gateway/revocation_validation.go"
	src := map[string]string{file: `package main
const p = "/admin/oauth2/introspect"
`}
	ledger := []ProviderLedgerEntry{{
		File:     file,
		Surfaces: []string{"/admin/oauth2/introspect", "/admin/oauth2/auth/sessions/login"},
		Why:      "страж старта", Until: "край перестал принимать издателя-поставщика",
	}}
	fs, _ := injectRun(t, src, ledger)
	if !injectFinding(fs, file, ProviderFindingStale, "/admin/oauth2/auth/sessions/login") {
		t.Fatalf("снятая половина записи не найдена: %+v", fs)
	}
	if injectFinding(fs, file, ProviderFindingStale, "/admin/oauth2/introspect") {
		t.Fatalf("живая половина записи объявлена мёртвой: %+v", fs)
	}
}

// TestProviderSurfaceInjection_EmptyTreeAndEmptyLedgerPass — Г: цель фазы не
// является поломкой.
//
// Ноль мест разговора при пустой ведомости — исход, к которому ведёт задача
// #900. Проба, падающая на нём, подталкивала бы держать запись ради зелёного.
func TestProviderSurfaceInjection_EmptyTreeAndEmptyLedgerPass(t *testing.T) {
	src := map[string]string{
		"services/x/internal/ok.go": `package x
const s = "/iam/v1/token"
`,
	}
	fs, c := injectRun(t, src, nil)
	if len(fs) != 0 {
		t.Fatalf("достигнутая цель объявлена находкой: %+v", fs)
	}
	if c.Files != 1 {
		t.Fatalf("перепись не сошлась: файлов %d", c.Files)
	}
}

// TestProviderSurfaceInjection_LiteralFormDoesNotDecideTheVerdict — Д: вердикт не
// зависит от того, КАК автор записал путь.
//
// Разбор читает ЗНАЧЕНИЕ литерала. Гейт по исходному тексту пропустил бы форму
// с обратными кавычками — а это ровно тот способ, каким запись обходят, не
// заметив, что обходят.
func TestProviderSurfaceInjection_LiteralFormDoesNotDecideTheVerdict(t *testing.T) {
	src := map[string]string{
		"services/x/internal/raw.go": "package x\nconst s = `" + "/admin/trust/grants/jwt-bearer/issuers" + "`\n",
	}
	fs, _ := injectRun(t, src, nil)
	if !injectFinding(fs, "services/x/internal/raw.go", ProviderFindingUnledgered, "/admin/trust/grants/jwt-bearer/issuers") {
		t.Fatalf("путь в обратных кавычках не найден: %+v", fs)
	}
}

// TestProviderSurfaceInjection_ProseCounterCountsTheProvidersName — Е: счётчик
// прозы считает ИМЯ поставщика, а не путь.
//
// Печатается затем, чтобы «ноль находок» не читалось как «слова в дереве нет».
func TestProviderSurfaceInjection_ProseCounterCountsTheProvidersName(t *testing.T) {
	src := map[string]string{
		"services/x/internal/p.go": "package x\n\n// Зеркало клиента у Hydra снимается вместе со строкой.\nconst s = \"\"\n",
		"services/x/internal/q.go": "package x\n\n// Ни о чём.\nconst t = \"\"\n",
	}
	_, c := injectRun(t, src, nil)
	if c.ProseMentions != 1 {
		t.Fatalf("счётчик прозы не сошёлся: %d, ожидалась 1", c.ProseMentions)
	}
}

// TestProviderSurfaceInjection_UnparseableSourceIsAnError — Ж: нечитаемый
// исходник — ОТКАЗ, а не тишина.
//
// Молчаливый пропуск превратил бы «не прочитали» в «нарушений нет» — тот самый
// класс, который гейт ловит.
func TestProviderSurfaceInjection_UnparseableSourceIsAnError(t *testing.T) {
	src := map[string]string{"services/x/internal/broken.go": "не Go вовсе {{{"}
	if _, _, err := FindProviderSurface(src, nil, nil); err == nil {
		t.Fatal("нечитаемый исходник пропущен молча")
	}
}

// TestProviderSurfaceInjection_ExemptFileIsSkippedAndCounted — З: послабление
// снимает файл с рассмотрения И СЧИТАЕТСЯ.
//
// Молча пропущенный файл превратил бы «не смотрели» в «нарушений нет» — то
// самое различие, ради которого перепись и печатается.
func TestProviderSurfaceInjection_ExemptFileIsSkippedAndCounted(t *testing.T) {
	src := map[string]string{
		"internal/repohygiene/providersurface.go": `package repohygiene
const dict = "/admin/clients"
`,
		"services/x/internal/other.go": `package x
const s = "/admin/clients"
`,
	}
	exempt := func(p string) bool { return p == "internal/repohygiene/providersurface.go" }
	fs, c, err := FindProviderSurface(src, nil, exempt)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if c.Exempt != 1 {
		t.Fatalf("исключённый файл не сосчитан: Exempt=%d", c.Exempt)
	}
	if injectFinding(fs, "internal/repohygiene/providersurface.go", ProviderFindingUnledgered, "") {
		t.Fatalf("исключённый файл всё равно объявлен находкой: %+v", fs)
	}
	// Обратная сторона: послабление накрывает НАЗВАННЫЙ путь, а не всё дерево.
	if !injectFinding(fs, "services/x/internal/other.go", ProviderFindingUnledgered, "/admin/clients") {
		t.Fatalf("послабление накрыло чужой файл: %+v", fs)
	}
}

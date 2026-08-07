// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package paginationordergate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// write lays one Go file into a fresh root and returns the root.
func write(t *testing.T, src string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "handler.go"), []byte(src), 0o600); err != nil {
		t.Fatalf("фикстура не записана: %v", err)
	}
	return root
}

func analyse(t *testing.T, root string) Report {
	t.Helper()
	rep, err := Analyse(root)
	if err != nil {
		t.Fatalf("анализ упал: %v", err)
	}
	return rep
}

// preamble is the shared fixture head: a handler with a collaborator and an `authz`
// field, so premise 2 holds inside every fixture as it does in the tree.
const preamble = `package fixture

type req struct{}

func (r *req) GetPageSize() int64   { return 0 }
func (r *req) GetPageToken() string { return "" }

type uc struct{}

func (u *uc) List(size int32, token string) error { return nil }

type az struct{}

func (a *az) gate() error { return nil }

type filt struct {
	PageSize  int32
	PageToken string
}

type H struct {
	uc    *uc
	authz *az
}

// Package-qualified spelling, as in the tree: safeconv.ClampNonNegInt32,
// validate.PageSize.
type sconv struct{}

func (sconv) ClampNonNegInt32(v int64) int32 { return int32(v) }

type vpkg struct{}

func (vpkg) PageSize(field string, v int64) (int64, error)  { return v, nil }
func (vpkg) ValidatePagination(token string, size int32) error { return nil }

var safeconv sconv
var validate vpkg

// Package-local spelling, as registry keeps its own page predicates.
func validatePageSize(v int64) (int64, error) { return v, nil }
`

// ── Предпосылки и перепись ──────────────────────────────────────────────────

// Дерево обязано быть чистым — и обход обязан СКАЗАТЬ, что именно он осмотрел.
// «Ноль находок» из «ноль прочитанного» получаться не должно.
func TestTreeHasNoPageFormatDecidedByAccess(t *testing.T) {
	rep, err := Analyse("../../services", "../../gateway")
	if err != nil {
		t.Fatalf("анализ дерева упал: %v", err)
	}
	t.Log(rep.Census())

	if bad := rep.PremiseFailures(); len(bad) > 0 {
		t.Fatalf("предпосылки гейта больше не держатся: %s", strings.Join(bad, "; "))
	}
	if rep.Files < 100 {
		t.Fatalf("обход прочитал %d файлов — это не дерево сервисов; «чисто» здесь ничего не значит", rep.Files)
	}
	if rep.Paginated < 10 {
		t.Fatalf("осуждено %d пагинированных методов — слишком мало, чтобы вердикт что-то значил", rep.Paginated)
	}
	for _, f := range rep.Findings {
		t.Errorf("%s", f.String())
	}
}

// Предпосылка 2 названа отдельно: полоса ORDER узнаёт решение о доступе по полю
// `authz`. Если поля не осталось, правило беспредметно, и это находка, а не успех.
func TestGateSaysSoWhenTheAuthzFieldConventionIsGone(t *testing.T) {
	root := write(t, `package fixture

type req struct{}

func (r *req) GetPageSize() int64 { return 0 }

type H struct{ uc *struct{ List func(int64) } }

func PageSize(field string, v int64) (int64, error) { return v, nil }

func (h *H) List(r *req) error {
	_, err := PageSize("page_size", r.GetPageSize())
	return err
}
`)
	rep := analyse(t, root)
	if len(rep.Findings) != 0 {
		t.Fatalf("фикстура задумана чистой, а гейт нашёл: %v", rep.Findings)
	}
	bad := rep.PremiseFailures()
	if len(bad) == 0 {
		t.Fatalf("в обходе нет ни одного вызова через поле `authz`, но гейт отчитался успехом")
	}
	if !strings.Contains(strings.Join(bad, " "), authzFieldName) {
		t.Fatalf("гейт не назвал утраченную предпосылку: %v", bad)
	}
}

// Обход, ничего не прочитавший, обязан отличаться от чистого.
func TestGateRefusesToVouchForAnEmptyWalk(t *testing.T) {
	rep := analyse(t, t.TempDir())
	if len(rep.PremiseFailures()) == 0 {
		t.Fatalf("пустой обход отчитался как чистый: «ноль находок» стало достижимо из «ноль прочитанного»")
	}
}

// ── Полоса COERCION: инъекция в обе стороны ─────────────────────────────────

// Верни дефект — гейт краснеет И называет координату.
func TestGateIsRedOnACoercionBeforeJudgement(t *testing.T) {
	root := write(t, preamble+`
func (h *H) ListThings(r *req) error {
	f := filt{PageSize: safeconv.ClampNonNegInt32(r.GetPageSize()), PageToken: r.GetPageToken()}
	return h.uc.List(f.PageSize, f.PageToken)
}
`)
	rep := analyse(t, root)
	if !hasRule(rep, RuleCoercion) {
		t.Fatalf("насыщение до проверки не найдено; findings=%v; census=%s", rep.Findings, rep.Census())
	}
	f := findRule(rep, RuleCoercion)
	if !strings.Contains(f.Pos, "handler.go:") {
		t.Fatalf("находка без координаты: %q", f.Pos)
	}
	if !strings.Contains(f.Method, "ListThings") {
		t.Fatalf("находка не назвала метод: %q", f.Method)
	}
}

// Поставь ЗАКОННУЮ конструкцию той же формы — гейт молчит. Насыщение само по себе
// не дефект: сужение int64→int32 нужно и остаётся, важно лишь, что судят раньше.
func TestGateIsSilentWhenTheRawValueIsJudgedBeforeItIsCoerced(t *testing.T) {
	root := write(t, preamble+`
func (h *H) ListThings(r *req) error {
	if _, err := validate.PageSize("page_size", r.GetPageSize()); err != nil {
		return err
	}
	f := filt{PageSize: safeconv.ClampNonNegInt32(r.GetPageSize()), PageToken: r.GetPageToken()}
	return h.uc.List(f.PageSize, f.PageToken)
}
`)
	rep := analyse(t, root)
	if len(rep.Findings) != 0 {
		t.Fatalf("гейт ловит форму, а не существо: законная конструкция отвергнута: %v", rep.Findings)
	}
	if rep.Coercions != 1 {
		t.Fatalf("перепись не увидела насыщение, которое здесь законно: coercions=%d", rep.Coercions)
	}
}

// Ключевой различитель. Валидатор с правильным именем, стоящий ДО насыщения, но
// получающий УЖЕ суженное значение, не судит ничего из присланного. Гейт по имени
// вызова назвал бы это чистым — ровно то состояние, в котором были семь iam-RPC.
func TestGateIsNotFooledByAValidatorHandedACoercedValue(t *testing.T) {
	root := write(t, preamble+`
func (h *H) ListThings(r *req) error {
	f := filt{PageSize: safeconv.ClampNonNegInt32(r.GetPageSize()), PageToken: r.GetPageToken()}
	if err := validate.ValidatePagination(f.PageToken, f.PageSize); err != nil {
		return err
	}
	return h.uc.List(f.PageSize, f.PageToken)
}
`)
	rep := analyse(t, root)
	if !hasRule(rep, RuleCoercion) {
		t.Fatalf("валидатор, получивший уже суженное значение, принят за проверку: %v", rep.Findings)
	}
}

// ── Полоса ORDER: инъекция в обе стороны ────────────────────────────────────

// Гейт доступа ПЕРЕД передачей страницы — находка, с координатой.
func TestGateIsRedOnAnAccessDecisionBeforeFormat(t *testing.T) {
	root := write(t, preamble+`
func (h *H) ListThings(r *req) error {
	if err := h.authz.gate(); err != nil {
		return err
	}
	return h.uc.List(int32(r.GetPageSize()), r.GetPageToken())
}
`)
	rep := analyse(t, root)
	if !hasRule(rep, RuleOrder) {
		t.Fatalf("решение о доступе перед проверкой формата не найдено: %v; census=%s", rep.Findings, rep.Census())
	}
	if f := findRule(rep, RuleOrder); !strings.Contains(f.Pos, "handler.go:") {
		t.Fatalf("находка без координаты: %q", f.Pos)
	}
}

// ЗАКОННАЯ конструкция той же формы: тот же вызов через то же поле, но ПОСЛЕ
// чтения — это построчный фильтр выдачи, а формат уже осуждён на пути чтения.
// Различает их только позиция; вокабуляр имён здесь бессилен.
func TestGateIsSilentOnAPostReadRowFilter(t *testing.T) {
	root := write(t, preamble+`
func (h *H) ListThings(r *req) error {
	if err := h.uc.List(int32(r.GetPageSize()), r.GetPageToken()); err != nil {
		return err
	}
	return h.authz.gate()
}
`)
	rep := analyse(t, root)
	if len(rep.Findings) != 0 {
		t.Fatalf("построчный фильтр после чтения принят за гейт перед чтением: %v", rep.Findings)
	}
	if rep.AuthzCalls == 0 {
		t.Fatalf("перепись не увидела вызов через `authz`, который здесь есть и законен")
	}
}

// Проверка формата перед гейтом снимает находку — и гейт при этом никуда не делся.
func TestGateIsSilentWhenFormatIsJudgedBeforeAccess(t *testing.T) {
	root := write(t, preamble+`
func (h *H) ListThings(r *req) error {
	if _, err := validate.PageSize("page_size", r.GetPageSize()); err != nil {
		return err
	}
	if err := validate.ValidatePagination(r.GetPageToken(), 0); err != nil {
		return err
	}
	if err := h.authz.gate(); err != nil {
		return err
	}
	return h.uc.List(int32(r.GetPageSize()), r.GetPageToken())
}
`)
	rep := analyse(t, root)
	if len(rep.Findings) != 0 {
		t.Fatalf("законный порядок отвергнут: %v", rep.Findings)
	}
}

// ── Гейт читает исполняемое, а не текст ─────────────────────────────────────

// Абзац, объясняющий проверку, стоит вплотную к ней. Гейт, ищущий слова, остаётся
// зелёным после того, как сам вызов удалили.
func TestGateReadsCodeNotComments(t *testing.T) {
	root := write(t, preamble+`
// ListThings: формат страницы судится первым — PageSize("page_size", r.GetPageSize())
// отвергает значение вне [0..1000] до всего остального, см. ValidatePagination.
func (h *H) ListThings(r *req) error {
	f := filt{PageSize: safeconv.ClampNonNegInt32(r.GetPageSize()), PageToken: r.GetPageToken()}
	return h.uc.List(f.PageSize, f.PageToken)
}
`)
	rep := analyse(t, root)
	if !hasRule(rep, RuleCoercion) {
		t.Fatalf("комментарий про проверку принят за проверку: %v", rep.Findings)
	}
}

// Метод без пагинации предметом не является — иначе гейт зашумит на всём дереве.
func TestGateIgnoresMethodsThatDoNotPaginate(t *testing.T) {
	root := write(t, preamble+`
func (h *H) GetThing(r *req) error {
	if err := h.authz.gate(); err != nil {
		return err
	}
	return h.uc.List(0, "")
}
`)
	rep := analyse(t, root)
	if len(rep.Findings) != 0 {
		t.Fatalf("непагинированный метод осуждён: %v", rep.Findings)
	}
	if rep.Paginated != 0 {
		t.Fatalf("непагинированный метод попал в предмет: paginated=%d", rep.Paginated)
	}
}

// Гейт, которого никто не зовёт, — украшение: он не может ни покраснеть, ни
// отчитаться. Шаг конвейера — единственный вызывающий инструмента уровня репозитория.
func TestCIRunsThisGate(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "ci.yaml"))
	if err != nil {
		t.Fatalf("ci.yaml не прочитан: %v", err)
	}
	const invocation = "go run ./tools/paginationordergate/cmd/pagination-order-gate"
	if !strings.Contains(string(b), invocation) {
		t.Fatalf("ci.yaml не запускает %q — гейт нем", invocation)
	}
}

func hasRule(r Report, rule Rule) bool {
	for _, f := range r.Findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

func findRule(r Report, rule Rule) Finding {
	for _, f := range r.Findings {
		if f.Rule == rule {
			return f
		}
	}
	return Finding{}
}

// Регистр имени валидатора НЕ решает исход — утверждение о САМОМ предикате.
//
// Прежняя редакция требовала заглавной `Validate`, поэтому идиоматичный
// неэкспортируемый хелпер `validatePagination` не опознавался вовсе (issue #111).
// Тест держит предикат НАПРЯМУЮ, а не через сборку дерева: попытка доказать это
// синтетическим хендлером оказалась вакуумной — на таком входе находки нет ни с
// фиксом, ни без него, потому что решает там позиция вызова, а не имя. Проверка,
// зелёная в обе стороны, хуже отсутствующей: она занимает слот и создаёт
// уверенность, которой нет.
func TestPageValidatorNameIsCaseInsensitive(t *testing.T) {
	recognised := []string{
		"ValidatePagination", "validatePagination",
		"ValidatePageToken", "validatePageToken",
		"ValidateRepoListPage", "validateRepoListPage",
		"PageSize",
	}
	for _, name := range recognised {
		if !isPageValidator(name) {
			t.Errorf("валидатор %q НЕ опознан — регистр не вправе решать исход", name)
		}
	}

	// Отрицание в паре с положительным: без него «опознаёт всё подряд» выглядело бы
	// как успех. Эти имена не про формат страницы и опознаваться не должны ни в каком
	// регистре.
	foreign := []string{
		"Validate", "validate", "ValidateName", "validateName",
		"ListPages", "PageToken", "Paginate", "", "GetPageSize",
	}
	for _, name := range foreign {
		if isPageValidator(name) {
			t.Errorf("%q опознан валидатором формата страницы — предикат слишком широк", name)
		}
	}
}

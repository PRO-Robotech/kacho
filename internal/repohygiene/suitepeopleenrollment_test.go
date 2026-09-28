// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// suitepeopleenrollment_test.go — ЛЮДЕЙ НАБОРОВ ЗАВОДИТ ТОЛЬКО ПОСЕВ И ТОЛЬКО
// РЕГИСТРАЦИЕЙ, ОТМЕТКА ПОДТВЕРЖДЕНИЯ — НЕ ВНЕ СЛУЖБЫ (приёмка F6b, F6b-52;
// kacho#2901).
//
// # Предмет
//
// Решение владельца 2026-09-27: дальше экранов регистрации и входа человек
// проходит только с подтверждённым адресом почты, а подтверждает адрес код из
// письма под сессией того же человека. Человек фикстуры появляется тем же путём,
// что человек продукта, и место его заведения ОДНО — посев наборов
// (tests/authz-fixtures/verified_human.py). Дерево платформы держит это тремя
// видами находки, единица — СТРОКА файла:
//
//  1. строка с адресом хука поставщика в одной из двух форм —
//     `InternalUserService/UpsertFromIdentity` либо `users:upsertFromIdentity`,
//     — где бы в обходе она ни стояла: объявление адреса, сегмент пути, строка
//     сценария, задающая адрес запроса, — находки наравне. Человек, заведённый
//     хуком, подтвердить адрес не может никогда: это вид, которого у продукта нет;
//  2. строка, пишущая колонку отметки (`email_verified_at`) либо зовущая её
//     писателя (`MarkEmailVerified`). Писатель отметки у службы зовётся одним
//     местом — исходом глагола подтверждения; в дереве платформы записей отметки
//     нет, и обхода письма ни в одной посадке нет (Р18);
//  3. строка источника случая набора newman, несущая адрес регистрации: человек,
//     заведённый шагом случая, появлялся бы после переписи людей посева (F6b-51)
//     и не держался бы ничем.
//
// # Обход
//
// Шесть каталогов, каждый существует: `tests/`, `gateway/tests/`,
// `services/*/tests/`, `ui-future/e2e/`, `deploy/`, `.github/`. Единица осмотра —
// отслеживаемый непустой текстовый файл (та же, что у `git grep -I -l -e ''`).
// Число осмотренных печатается по каждому каталогу; каталог с нулём осмотренных —
// отказ гейта, а не зелёное: пустой обход неотличим от чистого.
//
// # Чего гейт не утверждает
//
// Что посев доводит людей до подтверждённого адреса — это исход прогона посева на
// стенде (F6b-46, F6b-51) и его самопроверка без стенда
// (`tests/authz-fixtures/verified_human.py --self-test`, F6b-50). Имя метода
// прозой (через точку) — не адрес, и находкой не считается.
package repohygiene

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/gitenv"
)

// suitePeopleWalk — каталоги обхода в порядке печати: pathspec и метка.
var suitePeopleWalk = []struct{ label, spec string }{
	{"tests/", "tests/**"},
	{"gateway/tests/", "gateway/tests/**"},
	{"services/*/tests/", "services/*/tests/**"},
	{"ui-future/e2e/", "ui-future/e2e/**"},
	{"deploy/", "deploy/**"},
	{".github/", ".github/**"},
}

var (
	// Вид (1): адрес хука в адресной форме — через «/» у полного имени метода и
	// через «:» у пути внутреннего слушателя края. Форма через точку — проза.
	suitePeopleHookRe = regexp.MustCompile(`InternalUserService/UpsertFromIdentity|users:upsertFromIdentity`)
	// Вид (2): запись колонки отметки либо вызов её писателя. Чтение колонки
	// (`SELECT email_verified_at …`) — не запись и находкой не считается.
	suitePeopleMarkWriterRe = regexp.MustCompile(`\bMarkEmailVerified\b`)
	suitePeopleMarkColumnRe = regexp.MustCompile(`(?i)(\bupdate\b.*\bset\b.*\bemail_verified_at\b|\binsert\s+into\b.*\bemail_verified_at\b|\bemail_verified_at\s*(:=|=[^=]))`)
	// Вид (3): адрес регистрации в источнике случая набора newman.
	suitePeopleRegisterRe = regexp.MustCompile(`/iam/v1/auth/register\b`)
	// Где живут источники случаев наборов newman.
	suitePeopleNewmanRe = regexp.MustCompile(`^(gateway/tests/newman/|services/[^/]+/tests/newman/)`)
)

type suitePeopleFile struct {
	path  string
	label string
	body  []byte
}

type suitePeopleFinding struct {
	path  string
	label string
	line  int
	kind  int
	text  string
}

func (f suitePeopleFinding) String() string {
	what := map[int]string{
		1: "адрес хука поставщика — человек, заведённый хуком, подтвердить адрес не может никогда",
		2: "запись отметки подтверждения вне службы — обход письма",
		3: "адрес регистрации в источнике случая набора — людей заводит только посев",
	}[f.kind]
	return fmt.Sprintf("%s:%d: вид (%d) %s\n      %s", f.path, f.line, f.kind, what, strings.TrimSpace(f.text))
}

// suitePeopleFindings — ядро: чистая функция над содержимым файлов.
func suitePeopleFindings(files []suitePeopleFile) (map[string]int, []suitePeopleFinding) {
	census := map[string]int{}
	var out []suitePeopleFinding
	for _, f := range files {
		if len(f.body) == 0 || bytes.IndexByte(f.body, 0) >= 0 {
			continue // пустой либо двоичный — не единица осмотра (как `git grep -I -l -e ''`)
		}
		census[f.label]++
		newman := suitePeopleNewmanRe.MatchString(f.path)
		for i, ln := range strings.Split(string(f.body), "\n") {
			switch {
			case suitePeopleHookRe.MatchString(ln):
				out = append(out, suitePeopleFinding{f.path, f.label, i + 1, 1, ln})
			case suitePeopleMarkWriterRe.MatchString(ln) || suitePeopleMarkColumnRe.MatchString(ln):
				out = append(out, suitePeopleFinding{f.path, f.label, i + 1, 2, ln})
			case newman && suitePeopleRegisterRe.MatchString(ln):
				out = append(out, suitePeopleFinding{f.path, f.label, i + 1, 3, ln})
			}
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].path != out[b].path {
			return out[a].path < out[b].path
		}
		return out[a].line < out[b].line
	})
	return census, out
}

func suitePeopleTree(t *testing.T) []suitePeopleFile {
	t.Helper()
	root := repoRoot(t)
	var files []suitePeopleFile
	for _, w := range suitePeopleWalk {
		out, err := gitenv.Command(root, "ls-files", "-z", "--", w.spec).Output()
		if err != nil {
			t.Fatalf("гейт НЕ ИСПОЛНЯЛСЯ: git ls-files -- %s отказал: %v", w.spec, err)
		}
		for _, p := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
			if p == "" {
				continue
			}
			b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p))) // #nosec G304 -- путь из индекса собственного дерева
			if err != nil {
				if os.IsNotExist(err) {
					continue // снят в рабочем дереве, ещё не в индексе
				}
				t.Fatalf("гейт НЕ ИСПОЛНЯЛСЯ: %s не читается: %v", p, err)
			}
			files = append(files, suitePeopleFile{path: p, label: w.label, body: b})
		}
	}
	return files
}

func suitePeopleReport(t *testing.T, census map[string]int) {
	t.Helper()
	var parts []string
	for _, w := range suitePeopleWalk {
		parts = append(parts, fmt.Sprintf("%s %d", w.label, census[w.label]))
	}
	t.Logf("осмотрено файлов по каталогам обхода: %s", strings.Join(parts, " · "))
	for _, w := range suitePeopleWalk {
		if census[w.label] == 0 {
			t.Errorf("каталог обхода %s — осмотрено 0 файлов: пустой обход неотличим от чистого, "+
				"и гейт не вправе считать его зелёным", w.label)
		}
	}
}

// TestSuitePeopleAreEnrolledOnlyBySeedRegistration — сам гейт.
func TestSuitePeopleAreEnrolledOnlyBySeedRegistration(t *testing.T) {
	t.Parallel()
	census, findings := suitePeopleFindings(suitePeopleTree(t))
	suitePeopleReport(t, census)
	byDir := map[string]int{}
	for _, f := range findings {
		byDir[f.label]++
	}
	var parts []string
	for _, w := range suitePeopleWalk {
		parts = append(parts, fmt.Sprintf("%s %d", w.label, byDir[w.label]))
	}
	t.Logf("находок %d (по каталогам: %s)", len(findings), strings.Join(parts, " · "))
	for _, f := range findings {
		t.Error(f.String())
	}
}

// Строки инъекции взяты из дерева платформы @b7608fe9750 (голова ветки волны
// 2797 до этого изменения) дословно; координата — рядом с каждой.
const (
	// tests/authz-fixtures/prodseed_matrix.py:191 @b7608fe9750
	suitePeopleInjHookCall = `            "-d", body, IAM_GRPC, "kaname.cloud.iam.v1.InternalUserService/UpsertFromIdentity"]`
	// gateway/tests/newman/collections/cluster_admin.postman_collection.json:579 @b7608fe9750
	suitePeopleInjHookScript = `                  "  pm.request.url = __cfgUrl + '/iam/v1/internal/users:upsertFromIdentity';",`
	// ui-future/e2e/specs/ceremony-seed.ts:60 @b7608fe9750
	suitePeopleInjRegister = `  register: "/iam/v1/auth/register",`
)

// TestSuitePeopleGate_FindsEachKindAndStaysSilentOnTheLegalTwin — гейт умеет
// покраснеть на каждом виде находки и молчит на законных конструкциях той же
// формы. Входы — строки дерева (выше), а не выдуманные.
func TestSuitePeopleGate_FindsEachKindAndStaysSilentOnTheLegalTwin(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, path, line string
		kind             int // 0 — находки быть не должно
	}{
		{"вызов хука в посеве", "tests/authz-fixtures/prodseed_matrix.py", suitePeopleInjHookCall, 1},
		{"адрес хука в строке сценария коллекции", "gateway/tests/newman/collections/cluster_admin.postman_collection.json", suitePeopleInjHookScript, 1},
		{"адрес регистрации в случае набора", "gateway/tests/newman/cases/cluster_admin.py", suitePeopleInjRegister, 3},
		{"адрес регистрации в случае набора службы", "services/vpc/tests/newman/cases/network.py", suitePeopleInjRegister, 3},
		{"запись колонки отметки в посеве", "tests/authz-fixtures/prodseed_matrix.py", `UPDATE kaname.users SET email_verified_at = now() WHERE id = 'usr0'`, 2},
		{"вызов писателя отметки", "deploy/scripts/seed.go", `	_ = repo.MarkEmailVerified(ctx, uid, now)`, 2},
		// Законные близнецы той же формы.
		{"адрес регистрации в посеве наборов — место заведения", "tests/authz-fixtures/verified_human.py", `REGISTER = "/iam/v1/auth/register"`, 0},
		{"адрес регистрации в посеве консоли", "ui-future/e2e/specs/ceremony-seed.ts", suitePeopleInjRegister, 0},
		{"имя метода прозой через точку", "tests/authz-fixtures/README.md", "- люди не через `InternalUserService.UpsertFromIdentity`", 0},
		{"чтение колонки отметки", "tests/authz-fixtures/prodseed_matrix.py", `SELECT email_verified_at FROM kaname.users WHERE id = 'usr0'`, 0},
	}
	for _, c := range cases {
		label := strings.SplitN(c.path, "/", 2)[0] + "/"
		switch {
		case strings.HasPrefix(c.path, "services/"):
			label = "services/*/tests/"
		case strings.HasPrefix(c.path, "gateway/tests/"):
			label = "gateway/tests/"
		case strings.HasPrefix(c.path, "ui-future/e2e/"):
			label = "ui-future/e2e/"
		}
		body := []byte("# законная строка до\n" + c.line + "\n# законная строка после\n")
		census, got := suitePeopleFindings([]suitePeopleFile{{path: c.path, label: label, body: body}})
		if census[label] != 1 {
			t.Errorf("%s: осмотрено %d файлов, ждали 1", c.name, census[label])
		}
		switch {
		case c.kind == 0 && len(got) != 0:
			t.Errorf("%s: законный близнец дал находку: %v", c.name, got)
		case c.kind != 0 && len(got) != 1:
			t.Errorf("%s: ждали одну находку вида (%d), получили %d", c.name, c.kind, len(got))
		case c.kind != 0 && (got[0].kind != c.kind || got[0].line != 2 || got[0].path != c.path):
			t.Errorf("%s: находка %v — ждали вид (%d) с координатой %s:2", c.name, got[0], c.kind, c.path)
		}
	}
	// Двоичный и пустой файл — не единица осмотра.
	census, got := suitePeopleFindings([]suitePeopleFile{
		{path: "deploy/x.bin", label: "deploy/", body: []byte("users:upsertFromIdentity\x00")},
		{path: "deploy/empty", label: "deploy/", body: nil},
	})
	if census["deploy/"] != 0 || len(got) != 0 {
		t.Errorf("двоичный и пустой файлы осмотрены (%d) либо дали находку (%v)", census["deploy/"], got)
	}
}

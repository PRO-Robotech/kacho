// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// uiremotepathshadow_test.go — путь, по которому консоль забирает ассеты модуля,
// не заслоняет собственный маршрут консоли.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Консоль — микрофронтенд: оболочка на nginx проксирует ассеты каждого модуля по
// своему пути и всё остальное отдаёт как одностраничное приложение. Если путь
// ассетов совпадёт с маршрутом приложения, то этот маршрут перестанет
// открываться напрямую: сервер отдаст по нему ассеты модуля, а не оболочку.
//
// Наблюдалось на поднятом стенде 2026-08-11. Восемь модулей адресовались как
// `/<домен>-remote/`, девятый — как `/dashboard/`, и это ровно тот маршрут, на
// который оболочка уводит с корня И с любого неизвестного пути:
//
//	<Route index element={<Navigate to="/dashboard" replace />} />
//	<Route path="*" element={<Navigate to="/dashboard" replace />} />
//
// Следствие: первый вход работал (переход делает браузер, запроса к серверу
// нет), а ОБНОВЛЕНИЕ СТРАНИЦЫ и переход по ссылке — нет. Вместо консоли
// приходил голый модуль (`<title>Kacho Dashboard Remote</title>` вместо
// `Kacho Future UI`), а `/dashboard` без косой черты давал ещё и абсолютный
// редирект на внутренний порт контейнера.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ЭТОГО НЕ ВИДНО В РАЗРАБОТКЕ
//
// В режиме разработки каждый модуль живёт на СВОЁМ порту
// (`http://localhost:4175…4182`), поэтому общего пространства путей нет и
// заслонить нечего. Столкновение существует только в собранной посадке — то
// есть ровно там, где его никто не проверяет глазами.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО УТВЕРЖДАЕТ ГЕЙТ
//
// Форму пути: у КАЖДОГО модуля он обязан быть `/<имя>-remote/…`. Суффикс —
// не украшение, а то, что выводит путь ассетов из пространства маршрутов
// приложения целиком: маршрута `-remote` у одностраничного приложения нет и
// быть не может, потому что это не домен продукта.
//
// Гейт НЕ перечисляет маршруты приложения и не сверяется с ними: перечень
// маршрутов меняется чаще, чем конвенция путей, и сверка с ним поймала бы
// сегодняшний случай, но пропустила бы завтрашний — новый маршрут поверх
// старого пути ассетов.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// uiHostDockerfile — где объявлены адреса модулей, попадающие в сборку оболочки.
const uiHostDockerfile = "ui-future/host/Dockerfile"

// uiRemoteArgRe — объявление адреса модуля.
var uiRemoteArgRe = regexp.MustCompile(`(?m)^ARG\s+KACHO_([A-Z0-9_]+)_REMOTE=(\S+)`)

// uiPublicBaseRe — базовый путь, с которым модуль СОБИРАЕТСЯ.
var uiPublicBaseRe = regexp.MustCompile(`(?m)^ARG\s+KACHO_PUBLIC_BASE=(\S+)`)

// uiPathFinding — один модуль, чей путь выведен из пространства маршрутов не был.
type uiPathFinding struct {
	Module string
	Got    string
	Want   string
}

// wantRemotePath — единственная форма пути ассетов модуля.
func wantRemotePath(module string) string { return "/" + module + "-remote/" }

// auditUIRemoteAssetPaths — обход объявлений адресов модулей у оболочки.
//
// Принимает КОРЕНЬ, а не читает фиксированный путь от корня репозитория: иначе
// гейт нельзя прогнать на синтетическом дереве, то есть нельзя доказать
// инъекцией — ни что он краснеет на дефекте, ни что он молчит на законном.
func auditUIRemoteAssetPaths(root string) (findings []uiPathFinding, declared int, err error) {
	path := filepath.Join(root, uiHostDockerfile)
	// #nosec G304 -- путь собран из корня обхода и константы этого файла.
	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		return nil, 0, fmt.Errorf("%s не читается: %w", uiHostDockerfile, rerr)
	}
	for _, m := range uiRemoteArgRe.FindAllStringSubmatch(string(raw), -1) {
		declared++
		name, got := strings.ToLower(m[1]), m[2]
		if want := wantRemotePath(name); !strings.HasPrefix(got, want) {
			findings = append(findings, uiPathFinding{Module: name, Got: got, Want: want})
		}
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].Module < findings[j].Module })
	return findings, declared, nil
}

// uiDockerfilesInTree — оболочки модулей консоли ПО ИНДЕКСУ git.
//
// Настоящее дерево спрашивается у индекса: под `ui-future/` на всякой машине,
// где собирали фронтенд, лежит игнорируемое (`node_modules`, сборочные
// каталоги, распаковки), и обход диска считал бы его частью репозитория.
func uiDockerfilesInTree(t *testing.T, root string) []string {
	t.Helper()
	dfs, err := treecorpus.Glob(filepath.Join(root, "ui-future", "*", "Dockerfile"))
	if err != nil {
		t.Fatalf("перечень оболочек модулей: %v — предпосылка гейта исчезла, "+
			"а не дерево стало чистым", err)
	}
	return dfs
}

// uiDockerfilesInSyntheticTree — то же для дерева, собранного САМОЙ пробой во
// временном каталоге. Репозиторием оно не является, индекса у него нет, и обход
// диска здесь законен — предмет запрета в другом.
func uiDockerfilesInSyntheticTree(t *testing.T, root string) []string {
	t.Helper()
	dfs, err := filepath.Glob(filepath.Join(root, "ui-future", "*", "Dockerfile"))
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	return dfs
}

// auditUIRemoteBuildBase — разбор базовых путей сборки модулей.
//
// Состав ПРИНИМАЕТСЯ, а не собирается: тот же разбор исполняется и на настоящем
// дереве (состав из индекса), и на синтетическом (состав с диска). Собирай он
// состав сам — одна из двух полос была бы неверной, а сведение их к одному
// источнику лишило бы гейт способности исполниться на синтетике.
func auditUIRemoteBuildBase(dfs []string) (findings []uiPathFinding, checked, dockerfiles int, err error) {
	sort.Strings(dfs)
	dockerfiles = len(dfs)
	for _, df := range dfs {
		name := filepath.Base(filepath.Dir(df))
		// #nosec G304 -- путь получен обходом каталога, собранного от корня обхода.
		raw, rerr := os.ReadFile(df)
		if rerr != nil {
			return nil, 0, 0, fmt.Errorf("%s не читается: %w", df, rerr)
		}
		m := uiPublicBaseRe.FindStringSubmatch(string(raw))
		if m == nil {
			// Оболочка базового пути не несёт — она и есть корень.
			continue
		}
		checked++
		if want := wantRemotePath(name); m[1] != want {
			findings = append(findings, uiPathFinding{Module: name, Got: m[1], Want: want})
		}
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].Module < findings[j].Module })
	return findings, checked, dockerfiles, nil
}

func TestUIRemoteAssetPathsDoNotShadowConsoleRoutes(t *testing.T) {
	findings, declared, err := auditUIRemoteAssetPaths(repoRoot(t))
	if err != nil {
		t.Fatalf("%v — предпосылка гейта исчезла, а не дерево стало чистым", err)
	}
	t.Logf("осмотрено: объявлений адреса модуля в %s — %d; находок %d",
		uiHostDockerfile, declared, len(findings))

	if declared == 0 {
		t.Fatal("объявлений адреса модуля не найдено — «столкновений нет» здесь означало бы " +
			"«ничего не читал», а не чистую конвенцию")
	}

	for _, f := range findings {
		t.Errorf("модуль %s забирает ассеты по %q, а не по %q — этот путь лежит В ПРОСТРАНСТВЕ "+
			"МАРШРУТОВ консоли и заслоняет одноимённый маршрут: прямой заход и обновление "+
			"страницы отдадут голый модуль вместо оболочки. Первый вход при этом работает "+
			"(переход делает браузер), поэтому дефект не виден ни в разработке — там у каждого "+
			"модуля свой порт, — ни при обычном клике",
			f.Module, f.Got, f.Want)
	}
}

// TestUIRemoteBuildBaseMatchesItsServedPath — базовый путь сборки модуля совпадает
// с путём, по которому его отдают.
//
// # Почему это ОТДЕЛЬНОЕ утверждение, а не часть предыдущего
//
// Адресов два, и живут они в разных файлах. Оболочка знает, откуда взять ВХОД
// модуля (`ARG KACHO_*_REMOTE` в её Dockerfile). Сам модуль знает, откуда
// браться его ЛЕНИВЫМ кускам, — и это зашито в ЕГО образ при сборке
// (`ARG KACHO_PUBLIC_BASE`, попадает в `vite base`). Совпадать они обязаны, но
// ничто их не связывает.
//
// Наблюдалось на стенде 2026-08-11, и обошлось дороже, чем выглядело: починив
// первый адрес и не тронув второй, я получил маршрут, который открывается, и
// модуль, чьи куски отдают 404 — то есть заменил один дефект другим. Кодами
// ответа это не ловится: страница приходит 200, а недостающее видно только в
// журнале браузера (`__federation_expose_*.js` → 404). Поймано проходом
// headless-браузером, не проверкой HTTP.
func TestUIRemoteBuildBaseMatchesItsServedPath(t *testing.T) {
	findings, checked, dockerfiles, err := auditUIRemoteBuildBase(uiDockerfilesInTree(t, repoRoot(t)))
	if err != nil {
		t.Fatalf("%v", err)
	}
	if dockerfiles == 0 {
		t.Fatal("модулей консоли с Dockerfile не найдено — предмет гейта исчез")
	}

	for _, f := range findings {
		t.Errorf("модуль %s собирается с базовым путём %q, а отдаётся по %q — его ленивые "+
			"куски будут запрашиваться по несуществующему адресу и отдадут 404. Страница при "+
			"этом придёт 200: расхождение видно только в журнале браузера, поэтому проверкой "+
			"кодов ответа оно не ловится",
			f.Module, f.Got, f.Want)
	}

	t.Logf("осмотрено: модулей с базовым путём — %d (из %d Dockerfile'ов; оболочка базы не несёт); "+
		"находок %d", checked, dockerfiles, len(findings))
	if checked == 0 {
		t.Fatal("ни один модуль не объявляет базовый путь — гейт молча проверяет пустоту")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ИНЪЕКЦИЯ: обе стороны гоняют ТУ ЖЕ функцию, что и гейты по дереву
//
// Дефект здесь пишется одной строкой объявления, и законный случай отличается от
// него ОДНИМ суффиксом. Без второй половины гейт нельзя отличить от проверки
// «объявление вообще есть»: она зеленела бы на любом пути.

// synthUITree — синтетическая консоль: файлы пишутся как есть, обхода индекса
// эти гейты не делают (они читают объявления сборки, а не состав дерева).
func synthUITree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

// Сторона дефекта: путь модуля лежит в пространстве маршрутов консоли.
func TestUIRemotePathGateRedOnAShadowingPath(t *testing.T) {
	root := synthUITree(t, map[string]string{
		uiHostDockerfile: "FROM nginx\n" +
			"ARG KACHO_VPC_REMOTE=/vpc-remote/assets/remoteEntry.js\n" +
			"ARG KACHO_DASHBOARD_REMOTE=/dashboard/assets/remoteEntry.js\n",
	})
	findings, declared, err := auditUIRemoteAssetPaths(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if declared != 2 {
		t.Fatalf("прочитано объявлений %d, ожидалось 2 — краснота относится не к тому, "+
			"что проверяли", declared)
	}
	if len(findings) != 1 || findings[0].Module != "dashboard" {
		t.Fatalf("заслоняющий путь не пойман или назван не тот модуль: %+v", findings)
	}
}

// Законный близнец той же формы: тот же модуль, тот же файл, суффикс на месте.
//
// Без этой половины гейт был бы неотличим от проверки «объявление существует» и
// краснел бы на КАЖДОМ модуле консоли — то есть был бы снят первым.
func TestUIRemotePathGateSilentOnTheLawfulTwin(t *testing.T) {
	root := synthUITree(t, map[string]string{
		uiHostDockerfile: "FROM nginx\n" +
			"ARG KACHO_VPC_REMOTE=/vpc-remote/assets/remoteEntry.js\n" +
			"ARG KACHO_DASHBOARD_REMOTE=/dashboard-remote/assets/remoteEntry.js\n",
	})
	findings, declared, err := auditUIRemoteAssetPaths(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if declared != 2 {
		t.Fatalf("прочитано объявлений %d, ожидалось 2", declared)
	}
	if len(findings) != 0 {
		t.Fatalf("гейт нашёл дефект в законном дереве: %+v", findings)
	}
}

// Сторона дефекта второго гейта: модуль собран с базой, по которой его не отдают.
func TestUIRemoteBuildBaseGateRedOnAMismatch(t *testing.T) {
	root := synthUITree(t, map[string]string{
		"ui-future/host/Dockerfile":      "FROM nginx\n",
		"ui-future/dashboard/Dockerfile": "FROM node\nARG KACHO_PUBLIC_BASE=/dashboard/\n",
	})
	findings, checked, dockerfiles, err := auditUIRemoteBuildBase(uiDockerfilesInSyntheticTree(t, root))
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if dockerfiles != 2 || checked != 1 {
		t.Fatalf("осмотрено Dockerfile'ов %d, из них с базой %d — ожидалось 2 и 1",
			dockerfiles, checked)
	}
	if len(findings) != 1 || findings[0].Module != "dashboard" {
		t.Fatalf("расхождение базы сборки не поймано: %+v", findings)
	}
}

// Законный близнец: база совпадает с путём отдачи, а оболочка базы не несёт —
// и это не находка, а её роль.
func TestUIRemoteBuildBaseGateSilentOnTheLawfulTwin(t *testing.T) {
	root := synthUITree(t, map[string]string{
		"ui-future/host/Dockerfile":      "FROM nginx\n",
		"ui-future/dashboard/Dockerfile": "FROM node\nARG KACHO_PUBLIC_BASE=/dashboard-remote/\n",
	})
	findings, checked, dockerfiles, err := auditUIRemoteBuildBase(uiDockerfilesInSyntheticTree(t, root))
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if dockerfiles != 2 || checked != 1 {
		t.Fatalf("осмотрено Dockerfile'ов %d, из них с базой %d — ожидалось 2 и 1",
			dockerfiles, checked)
	}
	if len(findings) != 0 {
		t.Fatalf("гейт нашёл дефект в законном дереве: %+v", findings)
	}
}

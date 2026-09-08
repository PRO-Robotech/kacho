// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// buildcontextguard_test.go — контекст сборки образов не содержит того, что
// пишут наши же конвейеры.
//
// # Предмет
//
// Каждый Dockerfile дерева начинается с `COPY . .`, поэтому слой попадает в кэш
// только если КОНТЕНТ контекста тот же, что в прошлый раз. Файл, который
// перезаписывает наш собственный конвейер, делает это условие невыполнимым:
// контекст отличается от прошлого прогона by construction, слой промахивается, и
// `go mod download` вместе с полной компиляцией ВОСЬМИ образов исполняется
// заново — при неизменном исходнике.
//
// Наблюдалось 2026-08-05 в двух видах сразу:
//
//   - `values.image-ids.yaml` пишет цель build-services идентификаторами
//     содержимого образов, которые она только что собрала. Цикл не сходится
//     никогда: сборка меняет собственный вход, новые образы дают новые
//     идентификаторы, следующий прогон промахивается снова;
//   - отчёты newman и пропатченное посевом окружение пишет каждый прогон e2e —
//     164 файла из 179 игнорируемых лежали в контексте.
//
// Цена не только во времени: `go mod download` — сетевой шаг, поэтому
// детерминированная часть подъёма стенда зависела от восьми подряд удачных
// обращений в сеть при полностью неизменном дереве.
//
// # Предикат
//
// Единица счёта — файл, который git ИГНОРИРУЕТ и который при этом лежит на
// диске (`git ls-files --others --ignored --exclude-standard`). Такой файл по
// определению не является исходником: его кто-то произвёл. Если он не исключён
// `.dockerignore`, он едет в контекст — находка.
//
// Обратная сторона предиката проверяется отдельным тестом: правило обязано
// краснеть на настоящем произведённом файле и молчать на исключённом.
//
// # Перепись
//
// Печатается, сколько игнорируемых файлов найдено и сколько из них исключено.
// Ноль ПРОСМОТРЕННЫХ — отказ: «нечего исключать» и «мы не посмотрели» обязаны
// быть различимы (в чистом клоне без прогонов игнорируемых файлов может не быть
// вовсе, поэтому пустой список сам по себе находкой не является — но он обязан
// быть напечатан, а не подразумеваться).
package repohygiene

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// dockerIgnorePatterns — исполняемые строки `.dockerignore` (без комментариев).
func dockerIgnorePatterns(body string) []string {
	var out []string
	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		out = append(out, l)
	}
	return out
}

// dockerIgnoreExcludes — попадает ли путь под какой-либо образец.
//
// Реализуется ровно то подмножество синтаксиса, которое дерево использует:
// точный путь, префикс-каталог, glob по имени и ведущее `**/`. Образец с
// отрицанием (`!`) в дереве отсутствует; если он появится, предикат обязан
// научиться его читать — до тех пор тест на его наличие падает (см. ниже),
// чтобы «не умею» не превратилось в «разрешено».
func dockerIgnoreExcludes(pats []string, rel string) bool {
	match := func(p, s string) bool {
		if ok, _ := path.Match(p, s); ok {
			return true
		}
		if ok, _ := path.Match(p+"/*", s); ok {
			return true
		}
		return false
	}
	for _, p := range pats {
		if rel == p || strings.HasPrefix(rel, strings.TrimSuffix(p, "/")+"/") {
			return true
		}
		if match(p, rel) {
			return true
		}
		if strings.HasPrefix(p, "**/") {
			suf := p[3:]
			parts := strings.Split(rel, "/")
			for i := range parts {
				tail := strings.Join(parts[i:], "/")
				if match(suf, tail) {
					return true
				}
				// Каталог исключается ЦЕЛИКОМ, на любую глубину. `match` выше
				// доходит лишь до одного уровня (`p+"/*"`, где `*` не проходит
				// через разделитель), поэтому `**/node_modules` покрывал бы
				// `node_modules/x`, но не `node_modules/.bin/x` — и гейт
				// предъявлял бы находкой файл, который docker в контекст не
				// берёт. Ложная находка здесь дороже пропуска: гейт, который
				// краснеет на исключённом, снимают целиком.
				if strings.HasPrefix(tail, strings.TrimSuffix(suf, "/")+"/") {
					return true
				}
			}
		}
	}
	return false
}

func TestBuildContextCarriesNothingOurPipelinesWrite(t *testing.T) {
	root := repoRoot(t)

	raw, err := os.ReadFile(filepath.Join(root, ".dockerignore"))
	if err != nil {
		t.Fatalf(".dockerignore не прочитан (%v) — без него КАЖДЫЙ произведённый "+
			"файл едет в контекст, и утверждать тут нечего", err)
	}
	pats := dockerIgnorePatterns(string(raw))
	if len(pats) == 0 {
		t.Fatal(".dockerignore пуст — контекст не сужается ничем")
	}
	for _, p := range pats {
		if strings.HasPrefix(p, "!") {
			t.Fatalf(".dockerignore несёт образец-исключение %q, которого предикат этого "+
				"гейта читать не умеет. Это отказ, а не разрешение: «не умею проверить» "+
				"не должно выглядеть как «проверено и чисто».", p)
		}
	}

	cmd := gitenv.Command(root, "ls-files", "--others", "--ignored", "--exclude-standard")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files --others --ignored: %v — состав произведённых файлов "+
			"неизвестен, значит вердикт был бы утверждением ни о чём", err)
	}

	var ignored, leaking []string
	for _, rel := range strings.Split(string(out), "\n") {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		ignored = append(ignored, rel)
		if !dockerIgnoreExcludes(pats, rel) {
			leaking = append(leaking, rel)
		}
	}
	sort.Strings(leaking)

	if len(leaking) > 0 {
		show := leaking
		if len(show) > 15 {
			show = show[:15]
		}
		t.Fatalf("в контекст сборки едут %d произведённых (git-ignored) файлов из %d — "+
			"слой `COPY . .` промахивается мимо кэша на КАЖДОМ прогоне, и все восемь "+
			"образов пересобираются заново при неизменном исходнике (вместе с сетевым "+
			"`go mod download`).\n  %s\n\nПочинка — исключить их в .dockerignore: "+
			"ни один Dockerfile дерева их не читает.",
			len(leaking), len(ignored), strings.Join(show, "\n  "))
	}

	t.Logf("игнорируемых файлов на диске: %d; попадают в контекст: 0; образцов в .dockerignore: %d",
		len(ignored), len(pats))
}

// TestBuildContextPredicateCutsBothWays — предикат обязан краснеть на настоящем
// произведённом пути и молчать на исключённом. Входы взяты из дерева, а не
// придуманы: это те самые пути, которые сегодня писали конвейеры.
func TestBuildContextPredicateCutsBothWays(t *testing.T) {
	pats := dockerIgnorePatterns(`.git
**/tests/newman/collections
deploy/helm/umbrella/values.image-ids.yaml
**/tests/newman/out
**/environments/local.postman_environment.json
tests/authz-fixtures/out
**/__pycache__
# комментарий не является образцом
`)
	cases := []struct {
		rel  string
		want bool // true = исключён
	}{
		{"deploy/helm/umbrella/values.image-ids.yaml", true},
		{"services/iam/tests/newman/out/iam-role.json", true},
		{"services/nlb/tests/newman/environments/local.postman_environment.json", true},
		{"tests/authz-fixtures/out/seed.json", true},
		{"tests/authz-fixtures/__pycache__/prodseed_all.cpython-312.pyc", true},
		{".git/HEAD", true},
		// ЗАКОННЫЕ ИСХОДНИКИ — обязаны ехать в контекст, иначе сборка сломается.
		{"services/iam/cmd/kaname/main.go", false},
		{"go.mod", false},
		{"proto/kaname/cloud/iam/v1/fga_model.fga", false},
		// Произведённый файл, которого в списке образцов НЕТ, — обязан быть виден.
		{"deploy/helm/umbrella/Chart.lock", false},
	}
	for _, tc := range cases {
		got := dockerIgnoreExcludes(pats, tc.rel)
		if got != tc.want {
			t.Errorf("%s: исключён=%v, ожидалось %v", tc.rel, got, tc.want)
		}
	}
}

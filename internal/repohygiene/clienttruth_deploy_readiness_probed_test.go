// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_deploy_readiness_probed_test.go — готовность, которую сервис
// СТРОИТ, обязана быть той, которую чарт СПРАШИВАЕТ.
//
// Сервисы этого дерева собирают готовность, знающую про зависимости: коннект к
// базе, поднятый boot-gate, канал к iam, живой LRO-worker. Она отдаётся на
// `/readyz` — и это настоящая работа, а не заглушка. Чарт при этом мог пробировать
// `tcpSocket`, то есть спрашивать «открыт ли порт». Слушающий сокет открыт и у
// процесса, который отвергает каждую мутацию, поэтому обещанного NotReady не
// наступало ни при каких условиях: под рапортовал Ready, а оператор узнавал о
// неготовности от арендаторов.
//
// Гейт сверяет ДВЕ ПОЛОСЫ ОДНОГО МЕХАНИЗМА между собой, а не каждую по
// отдельности. Проба «сервис отдаёт /readyz» зеленела бы при tcpSocket в чарте;
// проба «в чарте есть readinessProbe» зеленела бы при tcpSocket тем более.
// Расходится именно РАЗНИЦА между ними, и увидеть её можно только сравнением.
//
// Единица счёта — ПАРА «сервис × шаблон развёртывания»: у части сервисов чартов
// два (свой и в зонтике), и разойтись они могут поодиночке.
//
// Чего гейт НЕ утверждает, названо прямо: он не требует `/readyz` от сервиса,
// который его не обслуживает. Такому нужна не проба в чарте, а сама готовность —
// это другой предмет, и подменять его пробой значило бы поставить живость в слот
// готовности. На момент заведения таких сервисов три: storage и registry отдают
// безусловный `/healthz`, geo — только `/metrics`.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// deploymentTemplates — каждый шаблон развёртывания в дереве, сопоставленный
// КЛЮЧУ СЕРВИСА. Перечень выводится обходом, а не выписывается: чарт, заведённый
// завтра, попадает под гейт сам.
//
// Ключ — имя каталога чарта без приставки `kacho-`: в зонтике чарты зовутся
// `kacho-iam`/`kacho-geo`, а каталог кода — `iam`/`geo`. Приставка снимается
// здесь, в одном месте, и печатается в переписи, чтобы сопоставление можно было
// прочитать глазами, а не угадывать.
func deploymentTemplates(t *testing.T, root string) map[string][]string {
	t.Helper()
	roots := []string{
		filepath.Join(root, "services"),
		filepath.Join(root, "deploy", "helm", "umbrella", "charts"),
	}
	out := map[string][]string{}
	for _, dir := range roots {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			tpl := filepath.Join(dir, e.Name(), "deploy", "templates", "deployment.yaml")
			if _, serr := os.Stat(tpl); serr != nil {
				tpl = filepath.Join(dir, e.Name(), "templates", "deployment.yaml")
				if _, serr2 := os.Stat(tpl); serr2 != nil {
					continue
				}
			}
			key := strings.TrimPrefix(e.Name(), "kacho-")
			out[key] = append(out[key], tpl)
		}
	}
	// Край живёт своим каталогом, а не среди сервисов.
	if tpl := filepath.Join(root, "gateway", "deploy", "templates", "deployment.yaml"); fileExists(tpl) {
		out["gateway"] = append(out["gateway"], tpl)
	}
	if len(out) == 0 {
		t.Fatalf("обход не нашёл ни одного шаблона развёртывания — гейт не утверждает ничего")
	}
	return out
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// serviceCodeRoot — корень исходников сервиса. Край живёт своим каталогом.
func serviceCodeRoot(root, svc string) string {
	if svc == "gateway" {
		return filepath.Join(root, "gateway")
	}
	return filepath.Join(root, "services", svc)
}

// servesReadyz отвечает, регистрирует ли сервис маршрут `/readyz`.
//
// Обход идёт по ВСЕМУ дереву сервиса, а не только по каталогу команды, и это не
// щедрость: маршрут регистрируют В ДВУХ РАЗНЫХ МЕСТАХ, и обе формы законны. У
// vpc/compute/nlb он поднимается в корне команды, рядом с диагностической
// поверхностью; у iam — в `internal/handler`, вместе с приёмом вебхуков. Первая
// редакция этого гейта читала только `cmd/*` и не видела iam вовсе: сервис,
// строящий готовность и не пробируемый по ней, оказывался НЕ нарушителем, а
// невидимкой. Форма, о которой распознаватель не знает, — не край и не редкость.
//
// Судит СТРОКОВЫЙ ЛИТЕРАЛ в синтаксическом дереве, а не текст файла: про
// готовность в этих корнях написано много прозы, и гейт по подстроке считал бы
// объяснение реализацией.
func servesReadyz(t *testing.T, dir string) (found bool, filesRead int) {
	t.Helper()
	fset := token.NewFileSet()
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // недоступный подкаталог обходим, не роняя гейт
		}
		if d.IsDir() {
			// Собственные пробы и данные проб маршрутов не регистрируют.
			if n := d.Name(); n == "testdata" || n == "docs" || n == "deploy" {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("read %s: %v", path, rerr)
		}
		file, perr := parser.ParseFile(fset, path, src, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", path, perr)
		}
		filesRead++
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if strings.Contains(lit.Value, "/readyz") {
				found = true
				return false
			}
			return true
		})
		return nil
	})
	return found, filesRead
}

var (
	// commentLineRe — строка-комментарий YAML целиком.
	commentLineRe = regexp.MustCompile(`(?m)^[ \t]*#.*$`)
	// readinessRe — начало блока готовности вместе с его отступом.
	readinessRe = regexp.MustCompile(`(?m)^([ \t]*)readinessProbe:[ \t]*$`)
)

// readinessProbesPath возвращает пути, которые чарт спрашивает в СЛОТЕ
// ГОТОВНОСТИ. Пустая строка в списке означает пробу без httpGet (tcpSocket или
// exec) — она не спрашивает ничего про готовность зависимостей.
//
// Комментарии снимаются ДО разбора: у нескольких чартов над пробой стоит абзац,
// объясняющий, почему готовность зависит от зависимостей, и он содержит слово
// `/readyz`. Гейт, читающий текст, зеленел бы на этом объяснении при tcpSocket
// строкой ниже.
func readinessProbesPath(t *testing.T, tplPath string) []string {
	t.Helper()
	raw, err := os.ReadFile(tplPath)
	if err != nil {
		t.Fatalf("read %s: %v", tplPath, err)
	}
	body := commentLineRe.ReplaceAllString(string(raw), "")
	lines := strings.Split(body, "\n")

	var out []string
	for i, line := range lines {
		m := readinessRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		indent := len(m[1])
		path := ""
		// Блок пробы — последующие строки с БОЛЬШИМ отступом. Первая строка с
		// отступом не больше — конец блока, и дальше начинается уже чужое поле
		// (в частности livenessProbe, чей путь к готовности отношения не имеет).
		for j := i + 1; j < len(lines); j++ {
			cur := lines[j]
			if strings.TrimSpace(cur) == "" {
				continue
			}
			if len(cur)-len(strings.TrimLeft(cur, " \t")) <= indent {
				break
			}
			if p := strings.TrimSpace(cur); strings.HasPrefix(p, "path:") {
				path = strings.Trim(strings.TrimSpace(strings.TrimPrefix(p, "path:")), `"'`)
			}
		}
		out = append(out, path)
	}
	return out
}

// TestReadinessProbeAsksWhatTheServiceActuallyServes — сервис, отдающий
// `/readyz`, обязан пробироваться по нему.
//
// Проваливается на: чарте, чей слот готовности не спрашивает `/readyz` у
// сервиса, который его обслуживает; на шаблоне без блока готовности вовсе; и на
// пустом обходе любой из двух полос.
func TestReadinessProbeAsksWhatTheServiceActuallyServes(t *testing.T) {
	root := repoRoot(t)
	charts := deploymentTemplates(t, root)

	keys := make([]string, 0, len(charts))
	for k := range charts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var serving, checked, goFilesRead, tplRead int
	for _, svc := range keys {
		servesIt, read := servesReadyz(t, serviceCodeRoot(root, svc))
		goFilesRead += read
		if !servesIt {
			continue // готовности нет вовсе — это другой предмет, см. шапку
		}
		serving++

		for _, tpl := range charts[svc] {
			tplRead++
			rel, rerr := filepath.Rel(root, tpl)
			if rerr != nil {
				rel = tpl
			}
			paths := readinessProbesPath(t, tpl)
			if len(paths) == 0 {
				t.Errorf("%s: %s не несёт ни одного readinessProbe, тогда как сервис обслуживает "+
					"/readyz — kubelet не узнает о неготовности зависимостей ничего", svc, rel)
				continue
			}
			var asks bool
			for _, p := range paths {
				if p == "/readyz" {
					asks = true
				}
			}
			if !asks {
				t.Errorf("%s: %s спрашивает в слоте ГОТОВНОСТИ %q, тогда как сервис строит "+
					"готовность по зависимостям и отдаёт её на /readyz. Открытый сокет (или "+
					"безусловный /healthz) готовностью не является: под рапортует Ready, отвергая "+
					"каждую мутацию. Образец провязки — services/nlb/deploy/templates/deployment.yaml",
					svc, rel, strings.Join(paths, ", "))
				continue
			}
			checked++
		}
	}

	// Перепись: «ноль находок» обязано быть отличимо от «ноль прочитанного».
	if goFilesRead == 0 {
		t.Fatal("прочитано ноль файлов Go — полоса «что сервис обслуживает» не построена")
	}
	if serving == 0 {
		t.Fatalf("ни один из %d чартов не сопоставился сервису, отдающему /readyz — либо дерево "+
			"перестало строить готовность, либо разбор перестал её видеть; и то и другое находка", len(charts))
	}
	t.Logf("перепись: чартов %d · сервисов с /readyz %d · шаблонов сверено %d (провязано %d) · файлов Go прочитано %d",
		len(charts), serving, tplRead, checked, goFilesRead)
}

// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy

// nginx_upstream_form_test.go — адрес, который резолвит NGINX, обязан быть полным.
//
// ─────────────────────────────────────────────────────────────────────────────
// РАЗЛИЧЕНИЕ, РАДИ КОТОРОГО ЗАВЕДЁН ГЕЙТ
//
// Короткая форма имени (`<svc>.<ns>.svc`) правильна для вызовов сервис→сервис:
// там резолвит libc, поисковый список применяется, и полная форма при `ndots:5`
// уходит перебирать чужие домены узла (#145).
//
// Для nginx это НЕВЕРНО. При `proxy_pass` с переменной nginx резолвит своим
// клиентом, который `/etc/resolv.conf` не читает вовсе — что доказывает сам чарт
// консоли: скрипт рядом достаёт оттуда адрес сервера имён ВРУЧНУЮ именно потому,
// что nginx его не видит. Поискового списка у nginx нет, и короткая форма не
// резолвится НИКОГДА: апстрим отвечает 502, а в журнале — «could not be
// resolved (3: Host not found)».
//
// Признак различения — КТО резолвит, а не «какая форма короче». Гейт закрепляет
// именно его: он смотрит только на значения, попадающие в nginx.
//
// Класс, который это ловит: правило, верное для одного резолвера, применённое
// ко всем адресам сразу. Правка, сломавшая консоль, была именно такой —
// механически последовательной и неверной для половины потребителей.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// nginxUpstreamValue — объявление адреса, который уедет в nginx.
//
// Ищем по ДВУМ признакам сразу: имя ключа содержит `upstream` (так их называет
// чарт консоли) и значение похоже на кластерное имя службы. Одного признака
// мало: `upstream` встречается в прозе, а кластерное имя — у адресов, которые
// резолвит не nginx.
var (
	reHelperUpstream = regexp.MustCompile(`printf\s+"%s\.%s\.svc(\.cluster\.local)?:`)
	reValueUpstream  = regexp.MustCompile(`(?i)^\s*[a-z0-9_-]*(?:upstream|apiGateway|kratosPublic|hydraPublic|kratosUi)[a-z0-9_-]*\s*:\s*"?([a-z0-9.-]+\.svc(?:\.cluster\.local)?)(:\d+)?"?\s*$`)
)

// uiChartFiles — файлы чарта консоли, объявляющие апстримы.
func uiChartFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	root := "../ui-future/deploy"
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(p, ".tpl") || strings.HasSuffix(p, ".yaml") {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("обход чарта консоли: %v — гейт, не прочитавший предмет, обязан падать", err)
	}
	if len(out) == 0 {
		t.Fatal("в чарте консоли не найдено ни одного файла — сравнивать не с чем")
	}
	return out
}

func TestNginxResolvedUpstreamsUseTheFullName(t *testing.T) {
	files := uiChartFiles(t)
	checked, short := 0, 0

	for _, f := range files {
		raw, err := os.ReadFile(f) // #nosec G304 -- путь из обхода каталога репозитория
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			var addr string
			switch {
			case reHelperUpstream.MatchString(line):
				checked++
				if !strings.Contains(line, ".svc.cluster.local:") {
					short++
					t.Errorf("%s:%d — апстрим консоли объявлен КОРОТКОЙ формой:\n    %s\n"+
						"nginx резолвит своим клиентом, поискового списка у него нет, и такое "+
						"имя не резолвится никогда: апстрим отвечает 502. Короткая форма верна "+
						"для вызовов сервис→сервис (там резолвит libc), но не здесь",
						f, i+1, strings.TrimSpace(line))
				}
			default:
				m := reValueUpstream.FindStringSubmatch(line)
				if m == nil {
					continue
				}
				addr = m[1]
				checked++
				if !strings.HasSuffix(addr, ".svc.cluster.local") {
					short++
					t.Errorf("%s:%d — адрес %q, который потребляет nginx, объявлен короткой формой. "+
						"Признак различения — КТО резолвит: nginx поисковый список не применяет",
						f, i+1, addr)
				}
			}
		}
	}

	if checked == 0 {
		t.Fatal("не найдено ни одного объявления апстрима — «нарушений нет» означало бы " +
			"«ничего не прочитано». Либо чарт перестал объявлять апстримы (тогда гейт " +
			"удаляется вместе с ними), либо сломался разбор")
	}
	t.Logf("осмотрено: файлов чарта консоли %d, объявлений апстрима %d, короткой формы %d",
		len(files), checked, short)
}

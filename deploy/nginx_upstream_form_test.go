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
//
// nginxUpstreamWord — слово, которым чарт консоли называет апстримы: карта
// `upstreams:` его значений. Его носитель — ключ, несущий слово.
const nginxUpstreamWord = "upstream"

// nginxUpstreamKeys — ключи апстримов, объявляемых адресом прямо в значениях.
// У каждого обязан быть носитель в чарте консоли
// (TestNginxUpstreamKeyFormsEachHaveACarrier): ключ без носителя — перечень,
// переживший свой предмет.
var nginxUpstreamKeys = []string{"apiGateway"}

var (
	reHelperUpstream = regexp.MustCompile(`printf\s+"%s\.%s\.svc(\.cluster\.local)?:`)
	reValueUpstream  = valueUpstreamOf(append([]string{nginxUpstreamWord}, nginxUpstreamKeys...))
	reUpstreamWord   = regexp.MustCompile(`(?i)^\s*[a-z0-9_-]*` + nginxUpstreamWord + `[a-z0-9_-]*\s*:`)
)

// valueUpstreamOf — распознаватель объявления апстрима по формам имени ключа.
func valueUpstreamOf(forms []string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)^\s*[a-z0-9_-]*(?:` + strings.Join(forms, "|") +
		`)[a-z0-9_-]*\s*:\s*"?([a-z0-9.-]+\.svc(?:\.cluster\.local)?)(:\d+)?"?\s*$`)
}

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

// TestNginxUpstreamKeyFormsEachHaveACarrier — у каждой формы имени ключа,
// которую знает распознаватель, есть носитель в чарте консоли: у слова
// апстрима — ключ, который его несёт, у ключа апстрима — хотя бы одно
// объявление адресом. Форма без носителя ничего не судит, а перечень, который
// её несёт, описывает уже не чарт: так пережили свою полосу раздачи ключи
// апстримов прежнего поставщика личности (#1276).
func TestNginxUpstreamKeyFormsEachHaveACarrier(t *testing.T) {
	files := uiChartFiles(t)
	word, keys := 0, map[string]int{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if reUpstreamWord.MatchString(line) {
				word++
			}
			for _, key := range nginxUpstreamKeys {
				if valueUpstreamOf([]string{key}).MatchString(line) {
					keys[key]++
				}
			}
		}
	}
	if word == 0 {
		t.Errorf("слово апстрима %q не стоит ни в одном ключе чарта консоли (файлов %d) — "+
			"распознаватель знает форму, которой в предмете нет", nginxUpstreamWord, len(files))
	}
	for _, key := range nginxUpstreamKeys {
		if keys[key] == 0 {
			t.Errorf("ключ апстрима %q не несёт ни одного объявления адресом в чарте консоли "+
				"(файлов %d) — распознаватель знает ключ, которого в предмете нет; снимите его", key, len(files))
		}
	}
	t.Logf("слово апстрима: ключей %d · ключи апстрима %d, носители %v · файлов чарта консоли %d",
		word, len(nginxUpstreamKeys), keys, len(files))
}

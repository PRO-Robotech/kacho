// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// generatorpluginpin_test.go — версию генератора выбирает объявление генерации,
// а не то, что оказалось в PATH.
//
// # Предмет
//
// Содержимое `pkg/api` определяет ПЛАГИН, а не buf: у двух человек с разными
// версиями плагина одна и та же команда на одном и том же контракте даёт разный
// вывод. Пока плагин назван голым именем (`local: protoc-gen-go`), его выбирает
// переменная окружения, и выбор этот невидим: обе стороны отвечают «сгенерил».
//
// Наблюдалось дважды за один день. Между машиной и стволом: регенерация после
// правки одного домена дала диффы в 36 файлах ЧУЖИХ доменов, и разница была не
// косметической — слив тела запроса переехал до разбора параметра пути, то есть
// на негодном параметре соединение стало переиспользоваться вместо разрыва.
// И внутри одной ветки: файл, сгенерированный и закоммиченный одним заходом, на
// следующем перегенерировался иначе при неизменном контракте.
//
// Отдельно стоит назвать, чем это дороже обычного дрейфа: смена версии
// генератора меняет транскодирование REST у КАЖДОГО сервиса разом. Такое
// изменение обязано быть отдельным и осознанным, а не побочным следствием того,
// у кого что установлено.
//
// # Что здесь считается пином
//
// Объявление обязано само нести версию, а не полагаться на окружение:
//
//   - `local: [go, run, <пакет>]` — версия приходит из `go.mod` В МОМЕНТ ВЫЗОВА.
//     Пакет обязан быть в графе модуля (у нас — `tools/tools.go` под тегом
//     сборки), иначе `go run` не соберёт его вовсе;
//   - `remote: <плагин>:<версия>` — версия названа прямо в объявлении.
//
// Голое имя не считается пином НИКОГДА, даже если рядом стоит `go install` без
// `@latest`: установка кладёт свою копию в PATH, но не отменяет чужую, стоящую
// раньше, — и вердикт снова становится свойством машины. Эта форма пина ровно
// тем и хороша, что её нельзя обойти по невнимательности: копия в PATH просто
// не исполняется.
//
// # Перепись
//
// Объявлений генерации в дереве немного, и именно поэтому обход обязан говорить,
// сколько их нашёл: гейт, переставший их находить, выходит зелёным на пустом
// множестве и переживает возврат дефекта.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// genPluginDoc — то немногое из buf.gen.yaml, что нужно этому гейту.
//
// `local` в buf v2 — либо строка, либо argv-список, поэтому читается как
// `yaml.Node`: жёсткий тип отверг бы одну из двух законных форм ещё на разборе.
type genPluginDoc struct {
	Plugins []struct {
		Local  yaml.Node `yaml:"local"`
		Remote string    `yaml:"remote"`
		Out    string    `yaml:"out"`
	} `yaml:"plugins"`
}

// localArgv — argv плагина: одна строка даёт argv из одного элемента.
func localArgv(n yaml.Node) []string {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Value == "" {
			return nil
		}
		return []string{n.Value}
	case yaml.SequenceNode:
		var out []string
		for _, item := range n.Content {
			out = append(out, item.Value)
		}
		return out
	default:
		return nil
	}
}

// goRunPackage — пакет, который argv исполняет через `go run`; пусто, если argv
// не этой формы.
func goRunPackage(argv []string) string {
	if len(argv) < 3 || argv[0] != "go" || argv[1] != "run" {
		return ""
	}
	return argv[2]
}

// checkGeneratorPluginPins — находки одного объявления генерации.
//
// modulePkgs — пакеты-генераторы, которые дерево держит в графе модуля.
// Принадлежность проверяется БЕЗУСЛОВНО: ветка «множество пусто — пропускаем»
// сделала бы сужение действующим ровно до тех пор, пока список непуст, а на
// пустом — тождественно истинным и внешне неотличимым от работающего. Пустое
// множество здесь означает отказ, а не разрешение, и синтетика обязана его
// называть так же, как дерево.
func checkGeneratorPluginPins(path, raw string, modulePkgs map[string]bool) ([]string, int) {
	var doc genPluginDoc
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return []string{path + ": не разобран YAML: " + err.Error() + " — файл НЕ проверен"}, 0
	}

	var findings []string
	for i, p := range doc.Plugins {
		where := path + ": плагин #" + itoa(i+1)
		argv := localArgv(p.Local)

		switch {
		case p.Remote != "":
			if !strings.Contains(p.Remote, ":") {
				findings = append(findings, where+" — удалённый плагин «"+p.Remote+
					"» назван БЕЗ версии: она будет выбрана в момент вызова, и один и "+
					"тот же контракт даст разный вывод в разное время")
			}
		case len(argv) == 0:
			findings = append(findings, where+" — не назван ни `local`, ни `remote`")
		default:
			pkg := goRunPackage(argv)
			if pkg == "" {
				findings = append(findings, where+" — плагин «"+strings.Join(argv, " ")+
					"» берётся из PATH: версию выбирает окружение, а не дерево. Содержимое "+
					"сгенерированного определяет ПЛАГИН, поэтому у двоих с разными версиями "+
					"одна команда на одном контракте даст разный вывод — и обе стороны "+
					"ответят «сгенерил». Назови версию в объявлении: `local: [go, run, "+
					"<пакет>]` (версия из go.mod) либо `remote: <плагин>:<версия>`")
				continue
			}
			if !modulePkgs[pkg] {
				findings = append(findings, where+" — `go run "+pkg+"`, но этого пакета нет "+
					"в графе модуля. Пин, который не резолвится, — не пин: генерация "+
					"утянет версию из сети, а не из go.mod")
			}
		}
	}
	sort.Strings(findings)
	return findings, len(doc.Plugins)
}

// TestGeneratorPluginsArePinned — по дереву.
func TestGeneratorPluginsArePinned(t *testing.T) {
	root := repoRoot(t)

	out, err := gitenv.Command(root, "ls-files", "-z", "*buf.gen.yaml").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v — без переписи «ноль находок» неотличимо от "+
			"«ноль прочитанного»", err)
	}
	var files []string
	for _, p := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if p != "" {
			files = append(files, p)
		}
	}
	if len(files) == 0 {
		t.Fatal("объявлений генерации в дереве не найдено — обход сломан, а не дерево чисто")
	}

	modulePkgs := modulePinnedPackages(t, root)
	if len(modulePkgs) == 0 {
		t.Fatal("в графе модуля не найдено ни одного пакета-генератора — проверка " +
			"принадлежности стала бы тождественно ложной")
	}

	plugins := 0
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil {
			t.Errorf("%s не прочитан: %v — файл НЕ проверен", f, err)
			continue
		}
		findings, n := checkGeneratorPluginPins(f, string(raw), modulePkgs)
		plugins += n
		for _, msg := range findings {
			t.Error(msg)
		}
	}
	t.Logf("осмотрено объявлений генерации: %d; плагинов в них: %d; "+
		"пакетов-генераторов в графе модуля: %d", len(files), plugins, len(modulePkgs))
}

// modulePinnedPackages — пакеты-генераторы, которые дерево держит в графе
// модуля: они перечислены импортами в tools/tools.go под тегом сборки.
//
// Читается ФАЙЛ, а не `go list`: список нужен и тогда, когда сборка сломана, а
// вердикт гейта не должен зависеть от того, собирается ли дерево прямо сейчас.
func modulePinnedPackages(t *testing.T, root string) map[string]bool {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, "tools", "tools.go"))
	if err != nil {
		t.Fatalf("tools/tools.go не прочитан: %v — множество пиннутых пакетов "+
			"неизвестно, и проверка принадлежности была бы вакуумной", err)
	}
	out := map[string]bool{}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, "_ \"")
		if !ok {
			continue
		}
		if i := strings.IndexByte(rest, '"'); i > 0 {
			out[rest[:i]] = true
		}
	}
	return out
}

// TestGeneratorPluginPinDetectorSeesBothForms — инъекция в обе стороны.
func TestGeneratorPluginPinDetectorSeesBothForms(t *testing.T) {
	pinned := map[string]bool{"example.com/cmd/protoc-gen-go": true}

	cases := []struct {
		name    string
		yaml    string
		pkgs    map[string]bool // nil → берётся pinned; пустая карта задаётся явно
		wantHit bool
	}{
		{
			name:    "голое имя плагина — находка",
			yaml:    "version: v2\nplugins:\n  - local: protoc-gen-go\n    out: ../pkg/api\n",
			wantHit: true,
		},
		{
			name:    "argv без go run — тоже из PATH, находка",
			yaml:    "version: v2\nplugins:\n  - local: [protoc-gen-go, --flag]\n    out: ../pkg/api\n",
			wantHit: true,
		},
		{
			name:    "go run пакета из графа модуля — законный близнец, молчит",
			yaml:    "version: v2\nplugins:\n  - local: [go, run, example.com/cmd/protoc-gen-go]\n    out: ../pkg/api\n",
			pkgs:    pinned,
			wantHit: false,
		},
		{
			name:    "go run пакета, которого в графе модуля нет — находка",
			yaml:    "version: v2\nplugins:\n  - local: [go, run, example.com/cmd/protoc-gen-чужой]\n    out: ../pkg/api\n",
			pkgs:    pinned,
			wantHit: true,
		},
		{
			name:    "удалённый плагин с версией — законный близнец, молчит",
			yaml:    "version: v2\nplugins:\n  - remote: buf.build/protocolbuffers/go:v1.36.11\n    out: ../pkg/api\n",
			wantHit: false,
		},
		{
			name:    "удалённый плагин без версии — находка",
			yaml:    "version: v2\nplugins:\n  - remote: buf.build/protocolbuffers/go\n    out: ../pkg/api\n",
			wantHit: true,
		},
		{
			name:    "плагин без объявления вовсе — находка",
			yaml:    "version: v2\nplugins:\n  - out: ../pkg/api\n",
			wantHit: true,
		},
		{
			// Fail-closed: если множество пиннутых пакетов не удалось собрать,
			// «пин» перестаёт быть проверяемым — и это находка, а не разрешение.
			name:    "множество пиннутых пакетов пусто — находка даже на go run",
			yaml:    "version: v2\nplugins:\n  - local: [go, run, example.com/cmd/protoc-gen-go]\n    out: ../pkg/api\n",
			pkgs:    map[string]bool{},
			wantHit: true,
		},
		{
			name: "один пиннут, второй нет — находка ровно на втором",
			yaml: "version: v2\nplugins:\n" +
				"  - local: [go, run, example.com/cmd/protoc-gen-go]\n    out: ../pkg/api\n" +
				"  - local: protoc-gen-go-grpc\n    out: ../pkg/api\n",
			pkgs:    pinned,
			wantHit: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkgs := tc.pkgs
			if pkgs == nil {
				pkgs = pinned
			}
			findings, n := checkGeneratorPluginPins("синтетика.yaml", tc.yaml, pkgs)
			if got := len(findings) > 0; got != tc.wantHit {
				t.Fatalf("ожидалась находка=%v, получено %v (плагинов %d): %v",
					tc.wantHit, got, n, findings)
			}
		})
	}
}

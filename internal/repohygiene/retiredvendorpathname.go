// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// retiredvendorpathname.go — ГЕЙТ КЛАССА: имя снятого издателя личности,
// носимое НАШИМ ПУТЁМ — именем файла или каталога (#2759).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Единица счёта — ПУТЬ отслеживаемого файла, а не строка и не имя внутри кода.
// Путь, который выбрали мы, переживает снятие поставщика ложью: файл лежит под
// прежним именем и после того, как поставщика в посадке не стало, и компилятор
// этого не заметит. Правится он переименованием в термин РОЛИ порта (`provider`,
// `admin-hop`, `identity`), а не переписыванием факта.
//
// Соседние держатели эту единицу не судят и судить не могут:
//
//	foreignidpname.go                объявленное имя Go — узел разбора, не путь;
//	retiredidentityvendorceiling.go  путь входит осью УБЫВАЮЩЕГО ПОТОЛКА: число не
//	                                 растёт над базой — но и не обязано быть нулём.
//
// Здесь — ноль. Наш путь с именем издателя есть находка сразу, с координатой и
// словом, которым он узнан.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕМ УЗНАЁТСЯ СЛОВО — ТЕМ ЖЕ РАСПОЗНАВАТЕЛЕМ, ЧТО ИМЯ В УЗЛЕ РАЗБОРА
//
// Своего словаря здесь нет: слово узнаёт `ForeignIDPNameWord` над перечнем
// `foreignIDPWords`. Путь режется на сегменты по `/`, сегмент — на куски по
// всякому знаку, не являющемуся буквой или цифрой (`.`, `-`, `_`, `+`, `@` …),
// кусок — на слова по camelCase и цифровой границе. Длинное слово перечня
// засчитывается вхождением (склейка строчными тоже), трёхбуквенное — только
// РАВЕНСТВОМ слова: поэтому `repository`, `directory`, `memory`, `factory`
// находкой не становятся по построению, а гидратацию отсеивает
// `foreignIDPNotWords`. Второй словарь разошёлся бы с первым молча.
//
// ─────────────────────────────────────────────────────────────────────────────
// ИМЯ, ДАННОЕ ПОСТАВЩИКОМ, — НЕ НАХОДКА, И ВЫПИСАННОГО ПЕРЕЧНЯ ИСКЛЮЧЕНИЙ НЕТ
//
// Архив внешней зависимости зонта helm кладёт в `charts/` под именем
// `<name>-<version>.tgz`, где `name` — имя чарта ПОСТАВЩИКА из его собственного
// Chart.yaml. Мы его не выбираем, и переименованием он не правится: следующий
// `helm dependency build` вернул бы прежнее имя. Такой путь узнаётся не списком,
// а ОБЪЯВЛЕНИЕМ: зависимость Chart.yaml зонта, чей репозиторий не `file://`, а
// имя и версия дают ровно этот базовый сегмент в каталоге `charts/` зонта.
//
// Отсюда самоистечение без записи, которая пережила бы предмет: снятие
// зависимости из Chart.yaml снимает и прощение, и архив, оставшийся после этого
// в дереве, становится находкой — он больше ничьим объявлением не является.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ГЕЙТ НЕ ВИДИТ — НАЗВАНО ВСЛУХ
//
//   - имя в СОДЕРЖИМОМ файла: строки держит потолок, объявленные имена Go —
//     `foreignidpname.go`;
//   - пути ДРУГИХ деревьев графа сборки: служба доступа и фундамент судят свои
//     пути сами (kaname#331); обход здесь — индекс git этого репозитория;
//   - неотслеживаемые файлы: слой кредов площадки намеренно лежит вне git
//     (`.gitignore`) и путём дерева не является;
//   - имя чарта, ключ значений, имя объекта Kubernetes — это свойства, а не
//     пути: каталог подчарта переименовывается, а `name:` его Chart.yaml — нет,
//     потому что по нему helm строит ключ значений и строку `# Source:`.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДПОСЫЛКА
//
// Обход, не прочитавший ни одного пути, — не «находок нет», а «не выполнилось».
// Chart.yaml зонта, который не разбирается, — тоже: отличить имя поставщика от
// нашего тогда не по чему, и всякий архив зависимости стал бы ложной находкой.
//
// Способность упасть и смолчать доказана инъекцией —
// retiredvendorpathname_injection_test.go.

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// RetiredVendorUmbrellaChart — объявление зонта, из которого выводится имя,
// данное поставщиком.
const RetiredVendorUmbrellaChart = "deploy/helm/umbrella/Chart.yaml"

// retiredVendorUmbrellaChartsDir — каталог, куда helm кладёт архивы зависимостей
// зонта. Архив с именем издателя в ЛЮБОМ другом месте поставщиком не назван.
const retiredVendorUmbrellaChartsDir = "deploy/helm/umbrella/charts/"

// errRetiredVendorPathEmptyWalk — обход пуст: вердикта нет.
var errRetiredVendorPathEmptyWalk = errors.New(
	"обход не прочёл ни одного пути — «находок нет» означало бы «ноль прочитанного»")

// RetiredVendorPathWord — сегмент пути и слово перечня, которым этот путь несёт
// имя издателя. Пустые строки — не несёт.
func RetiredVendorPathWord(rel string) (segment, word string) {
	notAlnum := func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }
	for _, seg := range strings.Split(rel, "/") {
		for _, piece := range strings.FieldsFunc(seg, notAlnum) {
			if w := ForeignIDPNameWord(piece); w != "" {
				return seg, w
			}
		}
	}
	return "", ""
}

// retiredVendorExternalArchives — базовые сегменты архивов внешних зависимостей
// зонта: `<name>-<version>.tgz` каждой зависимости, чей репозиторий не `file://`.
func retiredVendorExternalArchives(chartYAML []byte) (map[string]bool, error) {
	var doc struct {
		Dependencies []struct {
			Name       string `yaml:"name"`
			Version    string `yaml:"version"`
			Repository string `yaml:"repository"`
		} `yaml:"dependencies"`
	}
	if err := yaml.Unmarshal(chartYAML, &doc); err != nil {
		return nil, fmt.Errorf("%s не разобран: %w", RetiredVendorUmbrellaChart, err)
	}
	out := map[string]bool{}
	for _, d := range doc.Dependencies {
		if d.Name == "" || d.Version == "" || strings.HasPrefix(d.Repository, "file://") {
			continue
		}
		out[d.Name+"-"+d.Version+".tgz"] = true
	}
	return out, nil
}

// RetiredVendorPathVerdict — вердикт одного обхода вместе с его переписью.
type RetiredVendorPathVerdict struct {
	// Walked — путей обойдено.
	Walked int
	// Named — из них несут имя издателя.
	Named int
	// External — внешних зависимостей объявлено в Chart.yaml зонта.
	External int
	// Given — пути, имя которым дал поставщик: архивы объявленных внешних
	// зависимостей. Печатаются поимённо, а не числом.
	Given []string
	// Findings — НАШИ пути, несущие имя издателя, с координатой и словом.
	Findings []string
}

// Census — перепись одной строкой: «ноль находок» отличим от «ноль прочитанного».
func (v RetiredVendorPathVerdict) Census() string {
	given := "нет"
	if len(v.Given) > 0 {
		given = strings.Join(v.Given, ", ")
	}
	return fmt.Sprintf("путей обойдено %d · несут имя издателя %d · из них имя дал "+
		"поставщик %d (архивы внешних зависимостей зонта, объявлено внешних %d): %s · "+
		"наших путей с именем издателя (находок) %d",
		v.Walked, v.Named, len(v.Given), v.External, given, len(v.Findings))
}

// JudgeRetiredVendorPaths судит перечень отслеживаемых путей против объявления
// зонта. Ошибка — третья категория: вердикта нет.
func JudgeRetiredVendorPaths(paths []string, chartYAML []byte) (RetiredVendorPathVerdict, error) {
	var v RetiredVendorPathVerdict
	if len(paths) == 0 {
		return v, errRetiredVendorPathEmptyWalk
	}
	archives, err := retiredVendorExternalArchives(chartYAML)
	if err != nil {
		return v, err
	}
	v.External = len(archives)
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)
	for _, rel := range sorted {
		v.Walked++
		seg, word := RetiredVendorPathWord(rel)
		if word == "" {
			continue
		}
		v.Named++
		if dir, base := path.Split(rel); dir == retiredVendorUmbrellaChartsDir && archives[base] {
			v.Given = append(v.Given, rel)
			continue
		}
		v.Findings = append(v.Findings, fmt.Sprintf(
			"%s: сегмент %q несёт имя снятого издателя словом %q — это НАШ путь, имя "+
				"ему дали мы: переименовать в термин роли порта, все ссылки и импортёры "+
				"тем же изменением (#2759)", rel, seg, word))
	}
	return v, nil
}

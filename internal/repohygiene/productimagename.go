// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// productimagename.go — разбор рецептов стенда на предмет ВЫВОДА имени образа
// части продукта приставкой платформы.
//
// # Предмет
//
// Соответствие «каталог исходников ↔ имя чарта ↔ имя образа» объявлено ОДИН раз
// — в `internal/productnaming`. До задачи #2076 оно ВЫВОДИЛОСЬ приставкой:
// `services/<svc>` → `kacho-<svc>`. Вывод был верен, пока имя платформы было и
// именем каждой её части; служба управления доступом получила собственное имя
// продукта (Kaname), и приставка перестала связывать.
//
// # Чем это было опасно — отказ ТИХИЙ, и наступает он не там, где ошибка
//
// Рендер чарта остаётся ЗЕЛЁНЫМ: `helm template` подставляет `kaname:dev` и не
// обязан знать, лежит ли такой образ в кластере. Рецепт же собирает и грузит в
// kind `kacho-iam:dev`. Образа `kaname:dev` в кластере нет, `pullPolicy:
// IfNotPresent` уходит за ним в реестр — и отказ наступает на ПОДНЯТОМ кластере,
// в виде `ImagePullBackOff`, то есть после всех проверок формы.
//
// # Что здесь считается находкой
//
// Приставка платформы, приклеенная к ПЕРЕМЕННОЙ, в слове, несущем тег образа:
// `kacho-$$svc:dev`, `kacho-$(SVC):dev`, `kacho-${svc}:dev`. Тег обязателен как
// признак: он отделяет ссылку на образ от прочих имён, которые законно
// выводятся приставкой и частью продукта НЕ являются.
//
// # Чего здесь НЕ судится — названо, чтобы «ноль находок» не читалось шире
//
//   - СЕЛЕКТОРЫ МЕТОК (`app.kubernetes.io/name=kacho-$1` в
//     `deploy/scripts/assert-metrics-surfaces-answer.sh`). Там это ОСОЗНАННЫЙ
//     перебор форм: гейт спрашивает кластер, чем резолвится каждый процесс, и
//     не притворяется, что знает раскладку меток лучше чартов. Единообразие
//     меток — предмет задачи #1007, а не этой проверки. Тега у такого слова нет,
//     поэтому оно не совпадает by construction, а не по списку прощённых;
//   - ИМЕНА КЛАСТЕРОВ kind (`CLUSTER_NAME: kacho-<шард>`): кластер частью
//     продукта не является, и тега у него тоже нет;
//   - СЕМЕЙСТВО МОДУЛЕЙ КОНСОЛИ (`kacho-ui-future-$$p:dev`): приставка склеена
//     не с переменной, а со своим сегментом семейства, и ведомости имён у
//     модулей консоли нет — заводить её здесь значило бы завести ВТОРОЙ словарь
//     об одном предмете;
//   - ИМЕНА БАЗ (`kacho_$(SVC)`) и ИМЕНА РАЗВЁРТЫВАНИЙ (`DEPLOY_NAME`): это
//     ДРУГИЕ словари — имя развёртывания задаёт каждый подчарт сам, и для vpc
//     оно `vpc`, а не `kacho-vpc`. Свести их сюда значило бы унифицировать по
//     самой ШИРОКОЙ семантике, а расходятся такие своды молча.
//
// # Популяция
//
// Рецепты, которыми поднимается стенд: `deploy/Makefile`, `deploy/*.mk` и
// `deploy/scripts/**/*.sh`. Конвейер сборки образов держит СВОЮ ведомость
// (`.github/workflows/docker-build.yml`), и она судится отдельной осью — на
// согласие с объявленным источником, а не на форму записи.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/internal/productnaming"
)

// imageNameFinding — одна находка с координатой. Координата обязательна: без
// неё читатель ищет предмет глазами, а гейт, не называющий места, снимают.
type imageNameFinding struct {
	File string
	Line int
	Text string
	Why  string
}

func (f imageNameFinding) String() string {
	return fmt.Sprintf("%s:%d: %s — %s", f.File, f.Line, f.Text, f.Why)
}

// imageNameScope — объём осмотренного. Печатается всегда: «ноль находок»
// обязано быть отличимо от «ноль прочитанного».
type imageNameScope struct {
	FilesRead   int
	LinesRead   int
	PrefixWords int // слов, начавшихся с приставки платформы, — с тегом и без
}

// platformPrefix — приставка платформы, ВЫВЕДЕННАЯ у объявленного источника.
//
// Выписанная здесь второй копией, она разошлась бы с пакетом молча: гейт
// продолжал бы искать прежнюю форму и не видел бы новой — то есть не краснел
// бы, а замолчал.
func platformPrefix() string {
	// Имя-зонд заведомо отсутствует в ведомости собственных имён, поэтому
	// ChartName обязан ответить выводом по приставке.
	const probe = "zzz-probe-not-a-service"
	return strings.TrimSuffix(productnaming.ChartName(probe), probe)
}

// wordEnd — конец «слова» ссылки на образ. Слово рвут пробельные знаки, кавычки
// и разделители команд; скобки НЕ рвут — `$(SVC)` есть часть слова.
func wordEnd(s string) int {
	for i, r := range s {
		switch r {
		case ' ', '\t', '\n', '"', '\'', '`', ';', ',', '|', '&', '\\':
			return i
		}
	}
	return len(s)
}

// varStart — начинается ли остаток с раскрытия переменной оболочки или make.
//
// Цифра в наборе НЕ для полноты: `$1` — позиционный параметр функции оболочки,
// и это РАБОЧАЯ здесь форма (`deploy/scripts/*.sh` принимают имя службы
// аргументом). Без неё `kacho-$1:dev` не совпадал бы вовсе — то есть уходил бы
// из-под наблюдения молча, а не отвергался. Обнаружено ломом собственного
// доказательства: снятие признака тега не покраснело, потому что близнец с
// `$1` не совпадал и по первому признаку.
var varStart = regexp.MustCompile(`^(\$\$?[A-Za-z_0-9{(]|\{\{)`)

// imageTag — несёт ли слово тег образа. Тег и есть признак, отделяющий ссылку
// на образ от прочих имён, законно выводимых приставкой.
var imageTag = regexp.MustCompile(`:[A-Za-z0-9_.$({][A-Za-z0-9_.${}()-]*$`)

// scanImageDerivations — находки в ОДНОМ файле.
//
// Принимает содержимое, а не путь, чтобы доказательство способности упасть
// подавало НАСТОЯЩИЙ вход этой же функции, а не повторяло её логику копией:
// копия осталась бы зелёной ровно тогда, когда гейт перестал бы работать.
func scanImageDerivations(path, content string) ([]imageNameFinding, int) {
	prefix := platformPrefix()
	var out []imageNameFinding
	words := 0

	for i, line := range strings.Split(content, "\n") {
		// Комментарий рецепта объясняет предмет запрета и обязан остаться
		// читаемым: гейт, судящий по сырому тексту, покраснел бы на
		// собственном объяснении (в шапке этого файла — тоже).
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "#") {
			continue
		}
		rest := line
		for {
			k := strings.Index(rest, prefix)
			if k < 0 {
				break
			}
			tail := rest[k+len(prefix):]
			words++
			word := prefix + tail[:wordEnd(tail)]
			if varStart.MatchString(tail) && imageTag.MatchString(word) {
				out = append(out, imageNameFinding{
					File: path, Line: i + 1, Text: word,
					Why: "имя образа выведено приставкой платформы из переменной; " +
						"часть со СВОИМ именем продукта (Kaname) так не называется, " +
						"и её образ в кластер не попадает — спрашивай имя у " +
						"scripts/lib/product-names.sh",
				})
			}
			rest = tail
		}
	}
	return out, words
}

// standRecipeFiles — популяция: рецепты, которыми поднимается стенд.
func standRecipeFiles(root string) ([]string, error) {
	var out []string
	deploy := filepath.Join(root, "deploy")
	err := filepath.WalkDir(deploy, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if name == "Makefile" || strings.HasSuffix(name, ".mk") || strings.HasSuffix(name, ".sh") {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// productImageDerivations — обход популяции целиком.
func productImageDerivations(root string) ([]imageNameFinding, imageNameScope, error) {
	files, err := standRecipeFiles(root)
	if err != nil {
		return nil, imageNameScope{}, err
	}
	var (
		out   []imageNameFinding
		scope imageNameScope
	)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, scope, err
		}
		rel, _ := filepath.Rel(root, f)
		found, words := scanImageDerivations(rel, string(b))
		out = append(out, found...)
		scope.FilesRead++
		scope.LinesRead += strings.Count(string(b), "\n") + 1
		scope.PrefixWords += words
	}
	return out, scope, nil
}

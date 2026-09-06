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
// Приставка платформы, приклеенная к ПЕРЕМЕННОЙ, — когда слово либо несёт тег
// образа (`kacho-$$svc:dev`, `kacho-$(SVC):dev`, `kacho-${svc}:dev`), либо
// стоит в ПОЗИЦИИ ссылки на образ (`docker build -t`, `kind load docker-image`,
// `docker image inspect`).
//
// Позиция добавлена ко второму кругу, и вот почему одного тега мало: `docker
// build -t kacho-$svc` без тега соберёт `:latest`, а `kind load docker-image
// kacho-$svc` его загрузит — то есть беда та же, а признака нет. Прежняя
// редакция объявляла размен («тег отделяет ссылку на образ»), но подавала его
// шире правды: бестеговая форма не судилась вовсе и не была названа зоной.
//
// Признак «тег ИЛИ позиция» оставляет вне предмета именно то, что и должно
// остаться: имя без тега и не в команде работы с образом ссылкой на образ не
// является.
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
	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
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
		// Токены, стоящие в позиции ссылки на образ ЭТОЙ строки: `-t X`,
		// `kind load docker-image X`, `docker image inspect … X`. Такой токен —
		// ссылка на образ ПО МЕСТУ, даже когда тега на нём нет: `docker build -t
		// kacho-$svc` соберёт `:latest`, а `kind load` его загрузит.
		inImagePos := map[string]bool{}
		for _, a := range imageArgsOn(line) {
			inImagePos[a.Tok] = true
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
			if varStart.MatchString(tail) && (imageTag.MatchString(word) || inImagePos[word]) {
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
//
// СОСТАВ БЕРЁТСЯ У ИНДЕКСА GIT, А НЕ С ДИСКА, и это не стиль.
//
// Прежняя редакция обходила `deploy/` через filepath.WalkDir. Под этим
// каталогом правила игнорирования действуют на любой глубине: `helm dep update`
// распаковывает туда вендоренные чарты (`charts/postgresql/`, `charts/vpc/`,
// `tmpcharts-<pid>/`), и на всякой машине, где хоть раз поднимали стенд, они
// лежат. Обход диска их читает.
//
// Цена измерена, а не предположена: файл `charts/postgresql/scripts/entry.sh`
// со строкой `docker build -t kacho-$1:dev .` невидим для `git status`, но
// давал НАХОДКУ и поднимал перепись с 94 рецептов до 95. То есть вердикт гейта
// становился свойством РАБОЧЕГО КАТАЛОГА, а не коммита — притом находка
// указывала на чужой чарт, который мы не пишем и чинить не можем.
//
// Пустой состав здесь — ОТКАЗ, а не пустой успех: `treecorpus.Under` возвращает
// ошибку, и она обязана дойти до вызывающего. «Ноль находок» на «ноль
// прочитанного» неотличимо от чистого дерева.
func standRecipeFiles(root string) ([]string, error) {
	files, err := treecorpus.Under(filepath.Join(root, "deploy"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range files {
		name := filepath.Base(f)
		if name == "Makefile" || strings.HasSuffix(name, ".mk") || strings.HasSuffix(name, ".sh") {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("под %s нет ни одного отслеживаемого рецепта стенда — "+
			"обход беспредметен, и это отказ, а не «ноль находок»", filepath.Join(root, "deploy"))
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
		b, err := os.ReadFile(f) // #nosec G304 -- путь из индекса git собственного дерева
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

// ─────────────────────────────────────────────────────────────────────────────
// ССЫЛКА НА ОБРАЗ В ЦЕЛИ СБОРКИ — ТОЛЬКО ОТВЕТ ЧИТАТЕЛЯ, НИКОГДА ЛИТЕРАЛ
//
// # Зачем этого мало — «в теле цели упомянут читатель»
//
// Прежняя редакция проверяла ВХОЖДЕНИЕ подстроки: тело цели упоминает
// `scripts/lib/product-names.sh` — значит читает источник. Проверка вакуумна с
// двух сторон сразу, и обе показаны опытом:
//
//   - снять ОДНО из двух подключений читателя в `build-services` — проба
//     остаётся зелёной, потому что второе упоминание на месте;
//   - вернуть исходную беду ЛИТЕРАЛОМ (`-t kacho-iam:dev`) рядом с живым
//     подключением — зелены все проверки ветки, весь пакет `deploy` и весь
//     `repohygiene`. То есть инвариант, ради которого заведена полоса, не имел
//     держателя вовсе.
//
// # Что проверяется вместо
//
// КАЖДЫЙ токен, стоящий в позиции ссылки на образ (`docker build -t X`,
// `kind load docker-image X`, `docker image inspect … X`), обязан быть
// раскрытием переменной, ЗНАЧЕНИЕ КОТОРОЙ В ЭТОМ ЖЕ ТЕЛЕ получено у читателя
// (`X=$(product_image_name …)`). Литерал отвергается, даже когда он верен
// сегодня: верность литерала — совпадение, а не свойство.
//
// Число найденных ссылок печатается: если формы команд сменятся и разбор
// перестанет их видеть, «ноль находок» обязано быть отличимо от «ноль
// прочитанного».

// imageArgPatterns — позиции ссылки на образ. Перечень закрыт и назван: это те
// три команды, которыми образ попадает в кластер стенда.
//
// # `-t` ТРЕБУЕТ СТРАЖА, И ВОТ ПОЧЕМУ
//
// Голый `-t\s+(\S+)` ключом к образу НЕ является: тот же ключ у `kubectl exec
// -t <под>` и `docker exec -t <контейнер>` — там это ИМЯ ПОДА и ИМЯ
// КОНТЕЙНЕРА. Замер по обеим формам давал находку «имя образа выведено
// приставкой… её образ в кластер не попадает» на строках, к образам отношения
// не имеющих. Живых вхождений в дереве нет, поэтому вреда пока не было — но
// основание, на котором шапка выше исключает имена развёртываний и кластеров
// («тега у него тоже нет»), при широком ключе переставало действовать, а шапка
// об этом молчала.
//
// Гейт, у которого находки ложные, отключают первым — и вместе с ним перестают
// читать настоящие. Поэтому `-t` признаётся ссылкой на образ ТОЛЬКО после
// стража `docker build` / `docker buildx build` на той же строке.
var imageArgPatterns = []struct {
	Cmd string
	// Guard — команда, после которой ключ вообще что-то говорит об образе.
	// nil означает, что ключ самодостаточен (имя команды уже в самом образце).
	Guard *regexp.Regexp
	Re    *regexp.Regexp
}{
	{"docker build -t", regexp.MustCompile(`docker\s+(?:buildx\s+)?build\b`), regexp.MustCompile(`-t\s+(\S+)`)},
	{"kind load docker-image", nil, regexp.MustCompile(`kind load docker-image\s+(\S+)`)},
	{"docker image inspect", nil, regexp.MustCompile(`docker image inspect\s+--format\s+'[^']*'\s+(\S+)`)},
}

// imageArgsOn — токены в позиции ссылки на образ на одной строке.
//
// Со стражем поиск идёт по остатку строки ПОСЛЕ него: `-t` до `docker build`
// (его там не бывает) ключом к образу не является, а после — является, включая
// вторую и третью метку многотеговой сборки.
func imageArgsOn(line string) []struct{ Cmd, Tok string } {
	var out []struct{ Cmd, Tok string }
	for _, p := range imageArgPatterns {
		seg := line
		if p.Guard != nil {
			loc := p.Guard.FindStringIndex(line)
			if loc == nil {
				continue
			}
			seg = line[loc[1]:]
		}
		for _, m := range p.Re.FindAllStringSubmatch(seg, -1) {
			out = append(out, struct{ Cmd, Tok string }{p.Cmd, m[1]})
		}
	}
	return out
}

// readerBound — присваивание значения от читателя: `img=$(product_image_name …)`.
var readerBound = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)=\$\$?\(\s*product_image_name`)

// varNameOf — имя переменной в токене ссылки. Пусто, если токен литерал.
func varNameOf(tok string) string {
	if i := strings.IndexByte(tok, ':'); i >= 0 {
		tok = tok[:i]
	}
	tok = strings.TrimLeft(tok, "$")
	tok = strings.Trim(tok, "{}()\"'")
	if tok == "" {
		return ""
	}
	for _, r := range tok {
		if r != '_' && (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return ""
		}
	}
	return tok
}

// recipeLine — ЛОГИЧЕСКАЯ строка рецепта: физические строки, склеенные `\\`.
//
// Единица именно такая, а не «тело цели», и это несущее различие. Каждая
// логическая строка исполняется ОТДЕЛЬНОЙ оболочкой, поэтому переменная,
// полученная у читателя в одной, в другой не существует. Проверка по всему телу
// цели этого не видит: приёмщик снял читателя из ОДНОГО из двух блоков
// `build-services` — привязка осталась во втором, и проверка молчала, тогда как
// сборка уже пользовалась именем, взятым не у источника.
type recipeLine struct {
	Line int // номер первой физической строки — координата для читателя
	Text string
}

// recipeLines — разбор тела цели на логические строки.
func recipeLines(body string) []recipeLine {
	var out []recipeLine
	var cur strings.Builder
	start := 0
	for i, ln := range strings.Split(body, "\n") {
		if cur.Len() == 0 {
			start = i + 1
		}
		trimmed := strings.TrimSuffix(ln, "\\")
		cur.WriteString(trimmed)
		cur.WriteString(" ")
		if !strings.HasSuffix(ln, "\\") {
			if t := strings.TrimSpace(cur.String()); t != "" {
				out = append(out, recipeLine{Line: start, Text: cur.String()})
			}
			cur.Reset()
		}
	}
	if t := strings.TrimSpace(cur.String()); t != "" {
		out = append(out, recipeLine{Line: start, Text: cur.String()})
	}
	return out
}

// imageArgumentFindings — ссылки на образ в теле цели, не пришедшие от читателя.
//
// Привязка ищется в ТОЙ ЖЕ логической строке, что и ссылка: см. recipeLine.
//
// Второй возврат — сколько ссылок вообще осмотрено: ноль означает, что формы
// команд сменились и разбор ослеп, а не что нарушений нет.
func imageArgumentFindings(target, body string) ([]imageNameFinding, int) {
	var out []imageNameFinding
	seen := 0
	for _, rl := range recipeLines(body) {
		if t := strings.TrimSpace(rl.Text); strings.HasPrefix(t, "#") || strings.HasPrefix(t, "@#") {
			continue
		}
		bound := map[string]bool{}
		for _, m := range readerBound.FindAllStringSubmatch(rl.Text, -1) {
			bound[m[1]] = true
		}
		for _, a := range imageArgsOn(rl.Text) {
			seen++
			name := varNameOf(a.Tok)
			if name == "" {
				out = append(out, imageNameFinding{
					File: "deploy/Makefile (цель " + target + ")", Line: rl.Line, Text: a.Tok,
					Why: a.Cmd + ": ссылка на образ задана ЛИТЕРАЛОМ. Верность литерала — " +
						"совпадение, а не свойство: он не меняется вслед за именем части. " +
						"Имя обязано приходить от product_image_name",
				})
				continue
			}
			if !bound[name] {
				out = append(out, imageNameFinding{
					File: "deploy/Makefile (цель " + target + ")", Line: rl.Line, Text: a.Tok,
					Why: a.Cmd + ": переменная " + name + " в ЭТОЙ логической строке рецепта " +
						"не получена у читателя имён (нет " + name + "=$(product_image_name …)). " +
						"Каждая строка рецепта — своя оболочка: привязка из соседней здесь не действует",
				})
			}
		}
	}
	return out, seen
}

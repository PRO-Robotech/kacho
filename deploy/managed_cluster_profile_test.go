// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// managed_cluster_profile_test.go — стенд, который ТЯНЕТ образы продукта из
// реестра, отвечает за три вещи, каждая из которых до сих пор держалась
// памятью: учётные данные хранилища слоёв приходят ссылкой, пины образов
// выводятся из дерева, а обещание «файл порождается» имеет порождающего.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ЭТИ ТРИ ВМЕСТЕ
//
// Общий признак у них один: стенд на ЧУЖОМ кластере. Пока продукт поднимался
// только в kind, каждое из трёх было безобидно — образ грузится в узел, а не
// тянется; учётные данные хранилища слоёв — заведомо непубличный плейсхолдер
// одноразового кластера; пин не нужен вовсе. На управляемом кластере все три
// меняют смысл разом, и ни одно из изменений не наблюдается ни рендером, ни
// `helm lint`: манифест собирается, значения подставляются, стенд поднимается.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭТИ ПРОВЕРКИ ЧИТАЮТ
//
// Только ОБЪЯВЛЕНИЯ — таблицу стеков и файлы значений. Ни чартов, ни сети, ни
// кластера, поэтому пропуститься они не умеют. Рендер тут и не помог бы:
// значение, приехавшее из слоя под профилем, в манифесте выглядит ровно так же,
// как объявленное самим профилем, — а предмет здесь именно «кто объявил».
package deploy_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// ─────────────────────────────────────────────────────────────────────────────
// Разбор ссылок на образы. Форма объявления у подчартов разная (плоская строка
// либо карта {repository,tag}), и обе — деталь их чартов, а не предмет проверок.

// productImageRe — ссылка на образ ПРОДУКТА в любом файле значений.
// Хвост тега необязателен: в слое стенда образ объявлен как `kacho-vpc:dev`,
// на управляемом кластере — как `docker.io/prorobotech/kacho-vpc:main-<коммит>`.
var productImageRe = regexp.MustCompile(`([A-Za-z0-9._/-]*\bkacho-[a-z0-9-]+):([A-Za-z0-9._-]+)`)

// productImageRefs — ссылки на образы продукта, найденные в дереве значений.
// Обход рекурсивный: образы лежат и на верхнем уровне (`vpc.image`), и внутри
// компонента консоли (`uif.dashboard.image`).
func productImageRefs(node any) []string {
	var out []string
	switch v := node.(type) {
	case map[string]any:
		for _, key := range sortedKeys(v) {
			if key == "image" {
				switch img := v[key].(type) {
				case string:
					if ref := strings.TrimSpace(img); ref != "" {
						out = append(out, ref)
					}
					continue
				case map[string]any:
					repo, _ := img["repository"].(string)
					tag, _ := img["tag"].(string)
					if strings.TrimSpace(repo) != "" {
						out = append(out, strings.TrimSpace(repo)+":"+strings.TrimSpace(tag))
					}
					continue
				}
			}
			out = append(out, productImageRefs(v[key])...)
		}
	case []any:
		for _, item := range v {
			out = append(out, productImageRefs(item)...)
		}
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// pulledFromARegistry — образ ТЯНЕТСЯ из реестра, а не грузится в узел стенда.
//
// Признак — наличие сегмента пространства имён: `docker.io/prorobotech/kacho-vpc`
// против `kacho-vpc`. Это свойство ЭТОГО дерева, а не догадка: собственные
// сборки продукта объявлены голым именем ровно там, где их загружает в kind
// `make dev-up`, и полным адресом всюду, где их тянет kubelet. Свойство
// проверяется положительным контролем: стек, у которого ни одна ссылка не
// прошла предикат, обязан существовать (`dev`) — иначе предикат разошёлся с
// деревом и молчание перестало быть отличимым от чистоты.
func pulledFromARegistry(ref string) bool {
	repo := ref
	if i := strings.LastIndex(repo, ":"); i > strings.LastIndex(repo, "/") {
		repo = repo[:i]
	}
	return strings.Contains(repo, "/")
}

// effectiveValues — значения стенда: умолчания умбреллы плюс профили цепочки,
// наложенные слева направо ровно так, как их получает helm.
func effectiveValues(t *testing.T, chain []string) map[string]any {
	t.Helper()
	out := mergeValues(map[string]any{}, readYAML(t, filepath.Join(umbrellaDir, "values.yaml")))
	for _, p := range chain {
		out = mergeValues(out, readYAML(t, filepath.Join(umbrellaDir, p)))
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// A. Учётные данные хранилища слоёв на стенде, тянущем образы из реестра.

// zotAuth — объявление учётных данных хранилища слоёв в дереве значений.
func zotAuth(tree map[string]any) map[string]any {
	reg, ok := tree["registry"].(map[string]any)
	if !ok {
		return nil
	}
	zot, ok := reg["zot"].(map[string]any)
	if !ok {
		return nil
	}
	auth, _ := zot["auth"].(map[string]any)
	return auth
}

// TestStandPullingFromARegistryTakesLayerStoreCredentialByReference — стенд,
// который тянет образы продукта из реестра, не несёт учётных данных хранилища
// слоёв ЗНАЧЕНИЕМ.
//
// ПРЕДМЕТ. Хранилище слоёв не различает тенантов: его пароль — это доступ ко
// всем слоям стенда мимо всей авторизации плоскости данных. Пока стенд — это
// одноразовый kind на ноутбуке, значение в профиле безобидно и объявлено
// плейсхолдером. Стенд на управляемом кластере живёт долго и доступен не только
// своему автору, а значение при этом лежит в ПУБЛИЧНОМ репозитории — то есть
// «пароль» перестаёт быть словом, обозначающим защиту.
//
// ПОЧЕМУ ПРИЗНАК — ИМЕННО «ТЯНЕТ ИЗ РЕЕСТРА». Это единственное свойство,
// которое отличает две посадки МАШИННО и приезжает из самого профиля: стенд,
// чьи образы грузятся в узел, по построению одноразовый и локальный. Правило
// самонастраивается — профиль, переведённый на публичные образы, приходит под
// проверку без правки этого файла.
func TestStandPullingFromARegistryTakesLayerStoreCredentialByReference(t *testing.T) {
	stacks := deployStacks(t)

	var (
		pulling []string
		local   []string
		refs    int
	)
	for _, name := range sortedStackNames(stacks) {
		values := effectiveValues(t, stacks[name])
		fromRegistry := false
		for _, ref := range productImageRefs(values) {
			if !productImageRe.MatchString(ref) {
				continue
			}
			refs++
			if pulledFromARegistry(ref) {
				fromRegistry = true
			}
		}
		if !fromRegistry {
			local = append(local, name)
			continue
		}
		pulling = append(pulling, name)

		auth := zotAuth(values)
		if auth == nil {
			t.Errorf("стек %q: учётные данные хранилища слоёв не объявлены вовсе — "+
				"чарт откажет в рендере, и это правильный исход, но обнаружить его "+
				"здесь дешевле", name)
			continue
		}
		byReference, _ := auth["existingSecret"].(string)
		if strings.TrimSpace(byReference) == "" {
			t.Errorf("стек %q тянет образы продукта из реестра и объявляет учётные данные "+
				"хранилища слоёв ЗНАЧЕНИЕМ. На таком стенде они приходят ССЫЛКОЙ на секрет, "+
				"созданный оператором (`registry.zot.auth.existingSecret`, как в "+
				"values.prod.yaml): хранилище слоёв не различает тенантов, его пароль — "+
				"доступ ко всем слоям мимо авторизации плоскости данных, а значение в "+
				"профиле лежит в публичном репозитории", name)
		}
		for _, key := range []string{"password", "htpasswd"} {
			if v, _ := auth[key].(string); strings.TrimSpace(v) != "" {
				t.Errorf("стек %q: `registry.zot.auth.%s` остаётся непустым значением. "+
					"Ссылка на секрет делает его инертным для чарта, но не убирает из "+
					"публичного файла — а читатель профиля не обязан знать, какое из двух "+
					"объявлений выигрывает", name, key)
			}
		}
	}

	sort.Strings(pulling)
	sort.Strings(local)
	t.Logf("осмотрено: стеков=%d, ссылок на образы продукта=%d; тянут из реестра=%d %v; "+
		"грузят образ в узел=%d %v", len(stacks), refs, len(pulling), pulling, len(local), local)

	if refs == 0 {
		t.Fatal("ни одной ссылки на образ продукта не найдено — «учётные данные в порядке» " +
			"здесь означало бы «ни один стенд не прочитан»")
	}
	if len(pulling) == 0 {
		t.Fatal("ни один стек не опознан тянущим образы из реестра — предикат разошёлся с " +
			"деревом; молчание в этом состоянии неотличимо от чистоты")
	}
	if len(local) == 0 {
		t.Fatal("ни один стек не опознан грузящим образ в узел — положительный контроль " +
			"предиката не сработал: он стал слишком широким и обвинит любой стенд")
	}
}

func sortedStackNames(stacks map[string][]string) []string {
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ─────────────────────────────────────────────────────────────────────────────
// B. Пины образов выводятся из дерева.

// managedProfile — профиль посадки на управляемый кластер, чьи пины порождаются.
const managedProfile = "values.a8f60d.yaml"

// derivedFromRe — запись о коммите, из которого выведены пины. Одна строка на
// файл: и ветка, и полный коммит, потому что тег сервиса несёт ВОСЕМЬ первых
// знаков коммита, а тег консоли — все сорок. Две записи об одном предмете
// разошлись бы молча, поэтому запись одна, а обе формы выводятся из неё.
var derivedFromRe = regexp.MustCompile(`(?m)^#\s*порождено-от:\s*([a-zA-Z0-9._-]+)\s+([0-9a-f]{40})\s*$`)

// notDerivedRe — вторая директива того же предмета: пин, который вывести
// НЕЛЬЗЯ, с перечнем образов и ПИСЬМЕННОЙ причиной.
//
//	# без-вывода: <образ> [<образ>…] — <причина>
//
// Зачем она есть. Часть профилей дерева запинена коммитами, которых в этом
// репозитории НЕТ: образы собирались прежними полирепозиториями, и их коммиты
// уехали вместе с ними. Такой пин не «ещё не выведен» — он не выводим здесь
// вообще, и записать ему «порождено-от» значило бы объявить вывод из коммита,
// которого не существует. Исход у него один и он принадлежит оператору:
// пере-пин на коммит этого дерева. До тех пор пин обязан НАЗЫВАТЬ СЕБЯ
// невыводимым — иначе он неотличим от выведенного и лжёт молча.
//
// Директива самоистекает по ВНЕШНЕМУ факту, а не по чьей-то памяти: как только
// коммит из тега начинает разрешаться в этом дереве, вывод становится возможен
// и запись роняет прогон. Образ, которого в профиле больше нет, — тоже находка.
var notDerivedRe = regexp.MustCompile(`(?m)^#\s*без-вывода:\s*([^—\n]+?)\s*—\s*(\S[^\n]*?)\s*$`)

// notDerivedExemptions — образы профиля, объявленные невыводимыми, и причина
// по каждому. Пустая карта означает «в профиле такой директивы нет».
func notDerivedExemptions(raw string) map[string]string {
	out := map[string]string{}
	for _, m := range notDerivedRe.FindAllStringSubmatch(raw, -1) {
		for _, img := range strings.Fields(m[1]) {
			out[img] = m[2]
		}
	}
	return out
}

// shaOfTag — часть тега после первого дефиса: то, чем тег называет коммит.
// Для `main-latest` это `latest` — не коммит, и разрешаться оно не будет
// никогда, что и требуется: заглушка не «станет выводимой» сама.
func shaOfTag(tag string) string {
	if i := strings.Index(tag, "-"); i >= 0 {
		return tag[i+1:]
	}
	return ""
}

// commitResolves — разрешается ли строка в коммит ЭТОГО дерева.
func commitResolves(sha string) bool {
	if len(sha) < 7 {
		return false
	}
	for _, r := range sha {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return gitenv.Command("", "cat-file", "-e", sha+"^{commit}").Run() == nil
}

// expectedTag — тег, который CI публикует для этого образа на этом коммите.
// Сборка сервисов режет коммит до восьми знаков, сборка консоли — нет; обе
// формы держатся в .github/workflows/{docker-build.yml,ui.yml}.
func expectedTag(image, branch, sha string) string {
	if strings.HasPrefix(image, "kacho-ui-future-") {
		return branch + "-" + sha
	}
	return branch + "-" + sha[:8]
}

// TestProductImagePinsAreDerivedFromTheRecordedCommit — пин образа продукта в
// профиле, объявившем порождение, РАВЕН выводу из записанного коммита.
//
// ПРЕДМЕТ. Пин, поставленный рукой, свежести не имеет и иметь не может: он
// верен ровно в момент написания и с первого же следующего коммита описывает
// не то состояние, которое стенд разворачивает. Ловится это только на стенде и
// только тем, кто знает, чего ждать. Здесь пины ВЫВОДЯТСЯ — из одной записи о
// коммите, — и проверка утверждает именно вывод, а не «пин присутствует».
//
// ЧТО ПРОВЕРКА НЕ УТВЕРЖДАЕТ. Что коммит свежий. Свежесть — свойство раскатки,
// а не файла: профиль записывает, из чего собран разворачиваемый стенд, и это
// его работа. Проверяется здесь другое: между записью и пинами не может
// возникнуть расхождения, а расхождение — единственный способ, которым пин
// начинает лгать незаметно.
func TestProductImagePinsAreDerivedFromTheRecordedCommit(t *testing.T) {
	profiles := trackedUmbrellaProfiles(t)

	// Предпосылка половины про самоистечение: история этого дерева доступна. При
	// усечённом клоне («глубина 1») не разрешается НИ ОДИН коммит, и проверка
	// «вывод стал возможен» молчала бы, ничего не прочитав. Молчание объявляется,
	// а не подразумевается.
	historyAvailable := commitResolves(headSHA(t))

	profilesWithPins, declaring, pins, exempted := 0, 0, 0, 0
	for _, name := range profiles {
		raw, err := os.ReadFile(filepath.Join(umbrellaDir, name))
		if err != nil {
			t.Fatalf("профиль %s не читается (%v) — предпосылка проверки исчезла", name, err)
		}
		if n := len(derivedFromRe.FindAllString(string(raw), -1)); n > 1 {
			t.Errorf("%s несёт %d записей «порождено-от» — два места об одном предмете "+
				"расходятся молча; запись одна на профиль", name, n)
		}
		rec := derivedFromRe.FindStringSubmatch(string(raw))
		exempt := notDerivedExemptions(string(raw))

		// Ссылки берутся из РАЗОБРАННОГО дерева, а не из текста. Первая редакция
		// читала текст выражением и видела только плоскую форму (`image: <строка>`):
		// четыре образа из семнадцати объявлены картой `{repository, tag}`, и правка
		// их тега мимо записи осталась бы незамеченной — гейт, читающий одну из двух
		// законных форм, молчит там, где выглядит работающим.
		pinned := map[string]bool{}
		profilePins, derivedHere := 0, 0
		for _, ref := range productImageRefs(readYAML(t, filepath.Join(umbrellaDir, name))) {
			m := productImageRe.FindStringSubmatch(ref)
			if m == nil || !pulledFromARegistry(ref) {
				continue // образ стенда либо сторонний: коммита этого дерева он не называет
			}
			repo, tag := m[1], m[2]
			image := repo[strings.LastIndex(repo, "/")+1:]
			pinned[image] = true
			profilePins++
			pins++

			if _, ok := exempt[image]; ok {
				exempted++
				// Самоистечение по внешнему факту: коммит из тега разрешается —
				// значит вывод возможен, и объявление о невыводимости пережило
				// свой предмет.
				if historyAvailable && commitResolves(shaOfTag(tag)) {
					t.Errorf("%s: образ %s объявлен невыводимым, но коммит из его тега %q "+
						"разрешается в этом дереве — вывод стал возможен, и запись "+
						"«без-вывода» больше нечего покрывать", name, image, tag)
				}
				continue
			}
			if rec == nil {
				t.Errorf("%s: пин образа %s (%q) не покрыт ничем. Пин, поставленный рукой, "+
					"свежести не имеет и иметь не может — он верен ровно в момент "+
					"написания.\n    Исходов два: (а) `# порождено-от: <ветка> <коммит>` "+
					"и тег, выведенный из неё; (б) `# без-вывода: %s — <причина>`, если "+
					"вывести нельзя. Умолчания «просто число» среди них нет",
					name, image, tag, image)
				continue
			}
			derivedHere++
			if want := expectedTag(image, rec[1], rec[2]); tag != want {
				t.Errorf("%s: образ %s запинен тегом %q, а из записанного коммита выводится "+
					"%q. Пин и запись — два места об одном предмете, и расходятся они молча",
					name, image, tag, want)
			}
		}
		if profilePins > 0 {
			profilesWithPins++
		}
		if rec != nil {
			declaring++
			// Запись, которой больше нечего выводить, — находка: послабление и
			// объявление, пережившие свой предмет, ведут себя одинаково.
			if derivedHere == 0 {
				t.Errorf("%s несёт запись «порождено-от», но ни один её пин из неё не "+
					"выводится — запись пережила свой предмет", name)
			}
		}
		for img, why := range exempt {
			if !pinned[img] {
				t.Errorf("%s: образ %s объявлен невыводимым (%q), а пина с таким образом в "+
					"профиле нет — запись без предмета", name, img, why)
			}
		}
	}

	t.Logf("осмотрено: профилей умбреллы=%d, из них с пинами продукта=%d, записывают "+
		"коммит вывода=%d; пинов=%d, из них объявлены невыводимыми=%d; история дерева "+
		"доступна=%t", len(profiles), profilesWithPins, declaring, pins, exempted,
		historyAvailable)

	if len(profiles) == 0 {
		t.Fatal("профилей умбреллы не найдено — проверять оказалось нечего, и это отказ, " +
			"а не чистое дерево")
	}
	if declaring == 0 {
		t.Fatal("ни один профиль не записывает коммит вывода — «все пины выведены» здесь " +
			"означало бы «ни один не проверен»")
	}
	if pins == 0 {
		t.Fatal("в профилях с записью вывода не найдено ни одного пина — предикат разошёлся " +
			"с формой объявления образа")
	}
}

// headSHA — коммит рабочего дерева. Нужен как ЗАВЕДОМО присутствующий объект,
// чтобы отличить «коммит не существует» от «истории нет вовсе».
func headSHA(t *testing.T) string {
	t.Helper()
	out, err := gitenv.Command("", "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// trackedUmbrellaProfiles — файлы значений умбреллы, ОТСЛЕЖИВАЕМЫЕ git.
// Единица счёта — отслеживаемый элемент, а не то, что лежит на диске: рядом
// живут порождённые и не отслеживаемые файлы (карта идентификаторов образов),
// и включать их в перечень значило бы считать не тот предмет.
func trackedUmbrellaProfiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(umbrellaDir)
	if err != nil {
		t.Fatalf("каталог умбреллы %s не читается (%v) — предпосылка проверки исчезла",
			umbrellaDir, err)
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "values") || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		if !gitTracks(t, filepath.Join(umbrellaDir, name)) {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// trackedPaths — множество путей, отслеживаемых git, от корня репозитория.
// Читается ОДИН раз: вопрос «отслеживается ли» задаётся по каждому файлу
// каталога, и запуск git на каждый из них превратил бы предикат в самое
// дорогое место проверки.
var trackedPaths = struct {
	once sync.Once
	set  map[string]bool
	err  error
}{}

func gitTracks(t *testing.T, path string) bool {
	t.Helper()
	trackedPaths.once.Do(func() {
		out, err := gitenv.Command("", "ls-files", "-z", "--", ".").Output()
		if err != nil {
			trackedPaths.err = err
			return
		}
		trackedPaths.set = map[string]bool{}
		for _, p := range strings.Split(string(out), "\x00") {
			if p != "" {
				trackedPaths.set[p] = true
			}
		}
	})
	if trackedPaths.err != nil {
		t.Fatalf("перечень отслеживаемых файлов не получен (%v) — единицей счёта стало бы "+
			"содержимое рабочего каталога, то есть другой предмет", trackedPaths.err)
	}
	return trackedPaths.set[filepath.ToSlash(path)]
}

// ─────────────────────────────────────────────────────────────────────────────
// C. Обещание порождения имеет порождающего.

// generationClaimRe — профиль утверждает, что ЕГО СОБСТВЕННОЕ содержимое
// порождается. Предмет утверждения обязан быть в самом утверждении: первая
// редакция предиката ловила любое слово о порождении и покраснела на трёх
// профилях, где речь шла о ссылках, ключах и материале сертификата, — то есть
// провалила контроль на законном близнеце и числа не измеряла. Поэтому
// требуется либо адресат правки (`порождающий`, `не редактировать`,
// `generated by`), либо явный субъект — сам файл, блок или пины.
var generationClaimRe = regexp.MustCompile(`(?im)^[\t ]*#.*(порождающ|не редактировать|do not edit|generated by|(файл|блок|профиль|пины)[^\n]*(порожда|порождён|порождено|генерир))`)

// producerPathRe — путь, названный внутри профиля. Путь узнаётся по разделителю
// и расширению исполняемого файла: слово `порождающий` без пути — не координата,
// и следующий читатель искать по нему нечего.
var producerPathRe = regexp.MustCompile(`[A-Za-z0-9._/-]*scripts/[A-Za-z0-9._-]+\.(sh|py|js)`)

// TestProfileClaimingGenerationNamesAProducerThatExists — профиль, объявивший
// себя порождаемым, называет порождающего, и порождающий существует и знает
// про этот профиль.
//
// ПРЕДМЕТ. «Файл порождается, правится порождающий скрипт» — указание, по
// которому следующий читатель НЕ станет править файл. Если порождающего нет,
// указание не запрещает правку, а лишь отменяет её: правку не сделает никто, а
// содержимое останется тем, каким его однажды написали рукой.
func TestProfileClaimingGenerationNamesAProducerThatExists(t *testing.T) {
	profiles := trackedUmbrellaProfiles(t)

	claims := 0
	for _, name := range profiles {
		raw, err := os.ReadFile(filepath.Join(umbrellaDir, name))
		if err != nil {
			t.Fatalf("профиль %s не читается (%v) — предпосылка проверки исчезла", name, err)
		}
		body := string(raw)
		if !generationClaimRe.MatchString(body) {
			continue
		}
		claims++

		found := producerPathRe.FindAllString(body, -1)
		if len(found) == 0 {
			t.Errorf("%s объявляет порождение и не называет порождающего. Указание «правится "+
				"порождающий» без координаты не переадресует правку — оно её отменяет: "+
				"следующий читатель не станет править файл и не найдёт, что править вместо",
				name)
			continue
		}
		// Существование требуется от КАЖДОГО названного пути: мёртвая координата
		// в профиле — находка независимо от того, порождающий это или нет.
		//
		// А вот отношение «знает про этот профиль» требуется ОТ ОДНОГО, и это не
		// послабление, а исправление предиката. Профиль вправе называть скрипты,
		// которые его не порождают: values.prorobotech.yaml называет провизор
		// секретов стенда, и требовать от провизора знать имя профиля значит
		// требовать отношения, которого между ними нет. Предмет проверки —
		// «у объявленного порождения есть порождающий», а он выполнен, как
		// только такой скрипт нашёлся хотя бы один. Профиль, где ни один
		// названный скрипт про него не знает, по-прежнему находка — ради этого
		// случая проверка и написана.
		related := 0
		for _, p := range found {
			rel := strings.TrimPrefix(p, "deploy/")
			if _, err := os.Stat(rel); err != nil {
				t.Errorf("%s называет порождающего %s, которого в дереве нет: %v", name, p, err)
				continue
			}
			producer, err := os.ReadFile(rel)
			if err != nil {
				t.Errorf("%s: порождающий %s не читается: %v", name, p, err)
				continue
			}
			if strings.Contains(string(producer), name) {
				related++
			}
		}
		if related == 0 {
			t.Errorf("%s объявляет порождение и называет %v, но НИ ОДИН из этих скриптов "+
				"про этот профиль не знает — ссылка есть, отношения нет", name, found)
		}
	}

	t.Logf("осмотрено: профилей умбреллы=%d, из них объявляют порождение=%d",
		len(profiles), claims)

	if len(profiles) == 0 {
		t.Fatal("профилей умбреллы не найдено — предпосылка проверки исчезла")
	}
	if claims == 0 {
		t.Fatal("ни один профиль не объявляет порождения — предикат разошёлся с деревом " +
			"либо предмет исчез; в обоих случаях молчание неотличимо от чистоты")
	}
}

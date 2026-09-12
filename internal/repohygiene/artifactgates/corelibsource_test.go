// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// corelibsource_test.go — общий помощник для гейтов этого пакета, чей предмет
// ПЕРЕЕХАЛ из каталога дерева в модуль общего фундамента
// (`github.com/PRO-Robotech/corelib`): `pkg/outbox/drainer` и `pkg/operations`.
//
// # Почему это ТРЕТЬЯ копия разбора go.mod, а не импорт готовой
//
// Одна реализация разбора `require` и кодировки пути модуля для кэша уже есть
// экспортированной в `internal/repohygiene` (`dependencylicense.go`,
// `ParseGoModRequires`/`ModuleCacheDir`), и вторая — своя копия — в
// `deploy/pool_out_of_pool_test.go`. Импорт первой отсюда БЫЛ первой попыткой
// (пакеты одного модуля, циклической ссылки нет: `internal/repohygiene` не
// импортирует `artifactgates` ни в одном не-тестовом файле) — и он ОТКЛОНЁН
// по факту, а не по вкусу: `internal/repohygiene` в этой же рабочей копии
// правят СЕЙЧАС две другие полосы, и его не-тестовые файлы в момент правки
// этого файла не собирались (`grpcmountparity.go`/`catalogreachability.go`,
// несовпадение сигнатур). Импорт превратил бы сборку `artifactgates` —
// пакета, который эти полосы не трогают, — в заложника ЧУЖОГО промежуточного
// состояния: гейт, ничего не знающий о migrationе pkg/, стал бы красным по
// причине, к его предмету не имеющей отношения, и наоборот — временное
// восстановление repohygiene молча замаскировало бы реальную поломку здесь.
//
// Поэтому — третья копия, маленькая и версия minimal: разбор ТОЛЬКО блочной
// формы `require (…)` (у go.mod этого дерева однострочной формы `require x y`
// нет — предпосылка проверяется явно, ниже) и кодировка пути кэша (правило
// экосистемы Go, не наше). Прочитай обе копии перед тем, как трогать эту
// третью: `internal/repohygiene/dependencylicense.go` (`ParseGoModRequires`,
// `EscapeModulePath`, `ModuleCacheDir`) и `deploy/pool_out_of_pool_test.go`
// (`corelibModuleVersion`, `escapeModulePath`, `corelibModuleDir`) — те, что
// уже разошлись между собой (первая читает обе формы `require`, вторая —
// только однострочную; при переносе одной из них на другую форму эта пара
// молча перестанет находить зависимость).
//
// # Почему ОТКАЗ, а не пустой список
//
// Модуль, не извлечённый в кэш, и пакет, переехавший внутри модуля, — РАЗНЫЕ
// события, и оба дают одно и то же пустое множество, если тут промолчать.
// Тогда «объявления не стало» и «кэш не наполнен» неотличимы, а гейт,
// беспредметный по второй причине, выглядел бы нашедшим первую. Поэтому оба
// пути возвращают ошибку, а не пустой срез: вызывающий гейт обязан упасть сам
// (`t.Fatal(err)`), а не молча решить, что предмета не стало.
//
// # Синтетический путь и НЕ-рекурсивный обход — не всегда одно и то же
//
// `corelibPackageGoFiles` читает РОВНО один пакет по имени каталога
// относительно корня модуля, не рекурсивно: вложенный пакет — другой предмет.
// Census, которому нужен и он, и его подпакет (`operations` +
// `operations/operationspb` — счётчик кодов gRPC полосы операций, где
// обработчик RPC лежит в подпакете), зовёт этот помощник ПО ИМЕНИ КАЖДОГО из
// них отдельно, а не рекурсией: рекурсия молча подмешала бы в перепись любой
// будущий подпакет, который никто не называл, и «ноль находок» перестало бы
// быть проверяемым утверждением о конкретных двух путях.
package artifactgates

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// corelibModulePath — путь модуля общего фундамента. Один на все гейты этого
// класса: второе написание разошлось бы с этим молча при следующем переносе.
const corelibModulePath = "github.com/PRO-Robotech/corelib"

// corelibRequireVersion — версия `corelibModulePath`, закреплённая go.mod
// судимого дерева.
//
// Читает ТОЛЬКО блочную форму `require (\n\tPATH VERSION\n)`: у go.mod этого
// дерева однострочной формы `require PATH VERSION` нет вовсе (предпосылка —
// вызывающий обязан проверить её сам, до вызова: `strings.Contains(body, "\n"+
// corelibModulePath+" ")` вне блока даёт находку, а не молчание). Строка
// комментария после версии (`// indirect` и подобные) отбрасывается по
// первому пробелу после версии — версия у Go-модуля пробелов не несёт.
func corelibRequireVersion(goModBody string) (version string, found bool) {
	inBlock := false
	for _, raw := range strings.Split(goModBody, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "require (":
			inBlock = true
			continue
		case inBlock && line == ")":
			inBlock = false
			continue
		}
		if !inBlock {
			continue
		}
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == corelibModulePath {
			return fields[1], true
		}
	}
	return "", false
}

// corelibEscapeModulePath — кодировка пути модуля для каталога кэша:
// заглавная буква становится восклицательным знаком со строчной
// (`H-BF` → `!h-!b!f`). Правило экосистемы модулей Go, а не наш вкус —
// файловые системы бывают регистронезависимы, и без него два разных модуля
// делили бы один каталог.
func corelibEscapeModulePath(p string) string {
	var b strings.Builder
	for _, r := range p {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('!')
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// corelibGoEnvModCache — каталог кэша модулей Go. Спрашивается у самого `go`,
// а не собирается из переменных окружения: `GOMODCACHE` бывает не задан, и
// тогда значение выводится из `GOPATH`, а он тоже бывает не задан.
func corelibGoEnvModCache() (string, error) {
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		return "", fmt.Errorf("go env GOMODCACHE: %w", err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", fmt.Errorf("go env GOMODCACHE пуст — каталог кэша модулей не установлен")
	}
	return dir, nil
}

// corelibModuleDir — каталог модуля `corelib` в кэше модулей Go, разрешённый по
// версии, которую закрепляет go.mod судимого дерева, и сама эта версия (нужна
// текстам отказа, чтобы читатель понимал, какую ревизию проверка читала).
func corelibModuleDir(root string) (dir, version string, err error) {
	body, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", "", fmt.Errorf("чтение go.mod: %w", err)
	}
	version, found := corelibRequireVersion(string(body))
	if !found {
		return "", "", fmt.Errorf("go.mod не закрепляет %s — общий фундамент не резолвится, "+
			"гейту нечем проверить перенесённое объявление", corelibModulePath)
	}
	cache, cerr := corelibGoEnvModCache()
	if cerr != nil {
		return "", "", cerr
	}
	dir = filepath.Join(cache, filepath.FromSlash(corelibEscapeModulePath(corelibModulePath))+"@"+version)
	return dir, version, nil
}

// corelibFilePath — абсолютный путь к ОДНОМУ named-файлу пакета общего
// фундамента (например pkg="outbox/drainer", file="classify.go"). Для гейтов,
// которым нужна не перепись каталога, а конкретный файл по имени.
func corelibFilePath(root, pkg, file string) (path, version string, err error) {
	dir, version, err := corelibModuleDir(root)
	if err != nil {
		return "", "", err
	}
	path = filepath.Join(dir, filepath.FromSlash(pkg), file)
	if _, statErr := os.Stat(path); statErr != nil {
		return "", "", fmt.Errorf("файл %s/%s общего фундамента (%s@%s) не найден: %w — "+
			"модуль не извлечён в кэш модулей (`go mod download`), либо файл переехал "+
			"внутри модуля", pkg, file, corelibModulePath, version, statErr)
	}
	return path, version, nil
}

// corelibPackageGoFiles — не-тестовые файлы Go ОДНОГО пакета общего фундамента
// по имени каталога ОТНОСИТЕЛЬНО корня модуля (например "operations" либо
// "operations/operationspb"). Обход НЕ рекурсивный — см. шапку файла.
//
// Ключ отдаваемой карты — синтетический путь `corelib/<pkg>/<файл>`, а не
// абсолютный путь кэша: версия модуля в имени файла не участвует, поэтому её
// бамп не меняет текст находок.
func corelibPackageGoFiles(root, pkg string) (map[string][]byte, error) {
	dir, version, err := corelibModuleDir(root)
	if err != nil {
		return nil, err
	}
	pkgDir := filepath.Join(dir, filepath.FromSlash(pkg))
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return nil, fmt.Errorf("каталог %s общего фундамента (%s@%s) не читается: %w — модуль не "+
			"извлечён в кэш модулей (`go mod download`), либо пакет переехал внутри "+
			"модуля. Гейт беспредметен: отличить «кэш не наполнен» от «объявления не "+
			"стало» по пустому списку нельзя, поэтому он отказывается судить, а не "+
			"молчит успехом",
			pkg, corelibModulePath, version, err)
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	out := make(map[string][]byte, len(names))
	for _, name := range names {
		src, rerr := os.ReadFile(filepath.Join(pkgDir, name))
		if rerr != nil {
			return nil, fmt.Errorf("файл %s/%s общего фундамента (%s@%s) не читается: %w",
				pkg, name, corelibModulePath, version, rerr)
		}
		out["corelib/"+pkg+"/"+name] = src
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("в %s общего фундамента (%s@%s) не нашлось ни одного не-тестового "+
			"файла Go — каталог пуст либо весь пакет составляют тесты. Гейт беспредметен ровно "+
			"тем же способом, что и на несуществующем каталоге",
			pkg, corelibModulePath, version)
	}
	return out, nil
}

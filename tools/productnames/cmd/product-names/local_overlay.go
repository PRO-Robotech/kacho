// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// ─────────────────────────────────────────────────────────────────────────────
// РЕЖИМ `--local-overlay` — НАКЛАДКА, ПЕРЕВОДЯЩАЯ ЦЕПОЧКУ НА ОБРАЗЫ СБОРКИ ДЕРЕВА
//
//	product-names --local-overlay -tag <тег> -built "<образ> <образ>…" СЛОЙ [СЛОЙ…] > накладка.yaml
//
// СЛОЙ — файлы значений в том порядке, в каком их получает helm (первым —
// умолчания умбреллы, дальше цепочка стенда из deploy/stacks.txt). `-built` —
// имена образов, которые сборка стенда ДЕЙСТВИТЕЛЬНО положила в узлы, через
// пробел либо запятую. Накладка печатается в стандартный вывод, перепись — в
// поток ошибок.
//
// Решение о каждом объявлении принимает internal/localimages: правило «чья это
// часть» живёт в источнике имён, и здесь второй раз не толкуется. Этот файл —
// ввод, вывод и КОДЫ:
//
//	0  накладка напечатана;
//	1  находка — цепочка объявляет часть этого дерева, которую сборка не
//	   произвела (названа поимённо);
//	2  предпосылка исчезла: не названы слои, перечень собранного или тег, слой
//	   не читается либо не разбирается, либо частей этого дерева цепочка не
//	   объявляет НИ ОДНОЙ. Пустая накладка при коде 0 подняла бы стенд на
//	   опубликованных образах чужой ревизии — «переводить нечего» не есть успех.

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/internal/localimages"
)

// localOverlayMode — вход режима. Потоки приходят аргументами по тому же доводу,
// что у externalPinsMode: пробе нужен вывод, а подмена `os.Stdout` делала бы её
// зависимой от чужой печати.
func localOverlayMode(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("--local-overlay", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tag := fs.String("tag", "", "тег, под которым сборка кладёт образы в узлы")
	built := fs.String("built", "", "образы, которые сборка произвела (через пробел или запятую)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	refuse := func(why string) int {
		_, _ = fmt.Fprintf(stderr, "product-names --local-overlay: %s\n", why)
		return 2
	}
	files := fs.Args()
	if len(files) == 0 {
		return refuse("не названо ни одного слоя значений — накладку выводить не из чего.\n" +
			"               Цепочку называет вызывающий (deploy/stacks.txt через tests/helm/stacks.sh).")
	}
	names := strings.FieldsFunc(*built, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' })
	if len(names) == 0 {
		return refuse("не назван перечень собранного (-built) — судить, что сборка произвела, нечем,\n" +
			"               а накладка на образ, которого в узлах нет, дала бы ImagePullBackOff после «helm upgrade прошёл».")
	}
	if strings.TrimSpace(*tag) == "" {
		return refuse("не назван тег (-tag) — ссылку на образ сборки собрать не из чего.")
	}

	folded := map[string]any{}
	for _, f := range files {
		// Путь приходит от рецепта стенда ЭТОГО дерева; `Clean` — по тому же
		// доводу, что у externalPinsMode.
		raw, err := os.ReadFile(filepath.Clean(f))
		if err != nil {
			return refuse(fmt.Sprintf("слой %s не читается (%v) — накладка по неполной цепочке "+
				"оставила бы часть образов на опубликованных пинах. Это «не выполнилось».", f, err))
		}
		var layer map[string]any
		if err := yaml.Unmarshal(raw, &layer); err != nil {
			return refuse(fmt.Sprintf("слой %s не разбирается (%v).", f, err))
		}
		folded = localimages.Merge(folded, layer)
	}

	overlay, census := localimages.Overlay(folded, names, strings.TrimSpace(*tag))
	_, _ = fmt.Fprintf(stderr,
		"осмотрено: слоёв %d, объявлений образа %d; переписано %d (части этого дерева), "+
			"пинов вынесенных частей оставлено %d, сторонних %d, карт без репозитория %d; "+
			"собранных образов названо %d\n",
		len(files), census.Declarations, census.TreeParts, census.External, census.Foreign,
		census.UnknownRepo, len(names))
	if len(census.Unused) > 0 {
		_, _ = fmt.Fprintf(stderr, "собраны, но не объявлены цепочкой: %s\n", strings.Join(census.Unused, " "))
	}
	if len(census.Unbuilt) > 0 {
		_, _ = fmt.Fprintf(stderr,
			"НАХОДКА: цепочка объявляет части этого дерева, которых сборка не произвела: %s.\n"+
				"         Образа с таким именем в узлах нет — под ушёл бы в ImagePullBackOff уже после\n"+
				"         «helm upgrade прошёл». Добавьте часть в сборку стенда либо уберите её из цепочки.\n",
			strings.Join(census.Unbuilt, " "))
		return 1
	}
	if census.TreeParts == 0 {
		return refuse("частей этого дерева цепочка не объявляет ни одной — переводить нечего.\n" +
			"               Это НЕ «накладка не нужна»: пустой слой поднял бы стенд на опубликованных образах.")
	}

	// Отступ в два знака — тот же, что у профилей умбреллы: накладку читает человек,
	// разбирающий красный прогон, рядом с цепочкой.
	var body strings.Builder
	enc := yaml.NewEncoder(&body)
	enc.SetIndent(2)
	if err := enc.Encode(overlay); err != nil {
		return refuse(fmt.Sprintf("накладка не сериализуется (%v).", err))
	}
	_ = enc.Close()
	_, _ = fmt.Fprintf(stdout,
		"# generated by 'product-names --local-overlay' — не редактировать, не коммитить\n%s", body.String())
	return 0
}

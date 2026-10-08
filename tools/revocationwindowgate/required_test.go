// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// required_test.go — разбор не отбрасывает ИЗВЕСТНУЮ ручку окна молча.
//
// # Предмет
//
// Ручка из словаря knobNames, объявленная без умолчания в исходнике (форма
// «ручка обязательна, величину выбирает посадка» — так объявлено окно звена
// прав notify-api, приёмка NTF-4 Р16), прежде проходила мимо переписи: разбор
// узнавал имя, не находил тега `default` и выходил из ветки. Площадка, которую
// разбор УЗНАЛ, исчезала, и гейт дерева называл последствие, а не причину:
// «запись политики без предмета … такой площадки в дереве нет». Площадка в
// дереве была; не было величины в исходнике.
//
// То же отбрасывание стояло на двух соседних ветках: умолчание, которое разбор
// не умеет прочитать (тег с непарсящимся значением, `SetDefault` с выражением
// вместо литерала). Все три — одна форма отказа: известная ручка, о которой
// перепись промолчала.
//
// # Что доказывается
//
// Каждая проверка — пара: настоящая форма ⇒ площадка либо находка с
// координатой; законный близнец, отличающийся ОДНИМ фактом ⇒ прежний исход.
package revocationwindowgate_test

import (
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/tools/revocationwindowgate"
)

// srcRequiredWindowKnob — форма объявления окна звена прав notify-api
// (services/notify/cmd/notify-api/internal/config/config.go): ручка из
// словаря, тега `default` нет.
const srcRequiredWindowKnob = `package config

import "time"

type Config struct {
	AuthzCacheTTL time.Duration ` + "`" + `envconfig:"KACHO_NOTIFY_AUTHZ_CACHE_TTL" knob:"notify.authz.cacheTTL"` + "`" + `
}
`

// srcDefaultedWindowKnob — законный близнец: та же ручка, тот же файл, ровно
// один факт другой — умолчание в исходнике есть.
const srcDefaultedWindowKnob = `package config

import "time"

type Config struct {
	AuthzCacheTTL time.Duration ` + "`" + `envconfig:"KACHO_NOTIFY_AUTHZ_CACHE_TTL" knob:"notify.authz.cacheTTL" default:"5s"` + "`" + `
}
`

// srcRequiredForeignKnob — близнец с другой стороны: та же форма без
// умолчания, но ручка не размеряет окно вердиктов (срок вопроса к службе
// доступа). Её разбор обязан пропустить, иначе перепись меряет форму
// объявления, а не предмет.
const srcRequiredForeignKnob = `package config

import "time"

type Config struct {
	AuthzCheckTimeout time.Duration ` + "`" + `envconfig:"KACHO_NOTIFY_AUTHZ_CHECK_TIMEOUT" knob:"notify.authz.checkTimeout"` + "`" + `
}
`

func scan(t *testing.T, src string) *revocationwindowgate.Report {
	t.Helper()
	rep := &revocationwindowgate.Report{}
	if err := revocationwindowgate.ScanFile(rep, "notify", "services/notify/cmd/notify-api/internal/config/config.go", src); err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if rep.FilesParsed != 1 {
		t.Fatalf("файл не разобран (FilesParsed=%d) — молчание значит «не читал», а не «чисто»", rep.FilesParsed)
	}
	return rep
}

func TestScanFile_RequiredWindowKnobIsASiteWithoutASourceValue(t *testing.T) {
	rep := scan(t, srcRequiredWindowKnob)
	if len(rep.Sites) != 1 || rep.SitesMatched != 1 {
		t.Fatalf("известная ручка окна без умолчания дала площадок %d (сопоставлено %d); ожидалась 1. "+
			"Разбор узнал ручку и отбросил её — гейт дерева называет это «записью без предмета», "+
			"хотя площадка в дереве есть", len(rep.Sites), rep.SitesMatched)
	}
	s := rep.Sites[0]
	if s.Knob != "KACHO_NOTIFY_AUTHZ_CACHE_TTL" || s.Service != "notify" || s.Line != 6 {
		t.Errorf("площадка %+v: ожидались ручка KACHO_NOTIFY_AUTHZ_CACHE_TTL, процесс notify, строка 6", s)
	}
	if s.InSource {
		t.Errorf("площадка без тега default помечена как несущая величину в исходнике (%s)", s.Window)
	}
	if s.Window != 0 {
		t.Errorf("площадке без величины в исходнике приписано окно %s — величину выдумал разбор", s.Window)
	}
	if len(rep.Findings) != 0 {
		t.Errorf("обязательная ручка — площадка, а не нечитаемая форма: находки %v", rep.Findings)
	}
}

func TestScanFile_DefaultedTwinCarriesItsSourceValue(t *testing.T) {
	rep := scan(t, srcDefaultedWindowKnob)
	if len(rep.Sites) != 1 {
		t.Fatalf("близнец с умолчанием дал площадок %d; ожидалась 1", len(rep.Sites))
	}
	s := rep.Sites[0]
	if !s.InSource || s.Window != 5*time.Second {
		t.Errorf("близнец с default:\"5s\" прочитан как %+v; ожидались величина 5s из исходника", s)
	}
}

func TestScanFile_RequiredForeignKnobIsSilent(t *testing.T) {
	rep := scan(t, srcRequiredForeignKnob)
	if len(rep.Sites) != 0 || len(rep.Findings) != 0 {
		t.Errorf("ручка срока вопроса (не окно вердиктов) дала площадок %d, находок %d: %+v %v",
			len(rep.Sites), len(rep.Findings), rep.Sites, rep.Findings)
	}
}

// Две оставшиеся ветки того же отбрасывания: умолчание известной ручки есть,
// но разбор не умеет его прочитать. Это находка с координатой, а не тишина.
func TestScanFile_UnreadableDefaultOfAKnownKnobIsAFinding(t *testing.T) {
	cases := map[string]string{
		"тег с нечитаемым значением": `package config

import "time"

type Config struct {
	AuthzCacheTTL time.Duration ` + "`" + `envconfig:"KACHO_NOTIFY_AUTHZ_CACHE_TTL" default:"five"` + "`" + `
}
`,
		"SetDefault с выражением вместо литерала": `package config

func defaults(v interface{ SetDefault(string, any) }, ttl any) {
	v.SetDefault("authz.cache-ttl", ttl)
}
`,
	}
	for name, src := range cases {
		rep := scan(t, src)
		if len(rep.Sites) != 0 {
			t.Errorf("%s: нечитаемое умолчание стало площадкой %+v — величину выдумал разбор", name, rep.Sites)
		}
		if len(rep.Findings) != 1 {
			t.Errorf("%s: находок %d, ожидалась 1 — известная ручка исчезла из переписи молча", name, len(rep.Findings))
			continue
		}
		if !strings.Contains(rep.Findings[0], "config.go:") {
			t.Errorf("%s: находка без координаты: %q", name, rep.Findings[0])
		}
	}

	// Законный близнец ветки SetDefault: тот же вызов с литералом — площадка.
	rep := scan(t, `package config

func defaults(v interface{ SetDefault(string, any) }) {
	v.SetDefault("authz.cache-ttl", "5s")
}
`)
	if len(rep.Sites) != 1 || len(rep.Findings) != 0 || rep.Sites[0].Window != 5*time.Second || !rep.Sites[0].InSource {
		t.Errorf("SetDefault с литералом: площадок %d, находок %d (%+v %v); ожидалась одна площадка 5s",
			len(rep.Sites), len(rep.Findings), rep.Sites, rep.Findings)
	}
}

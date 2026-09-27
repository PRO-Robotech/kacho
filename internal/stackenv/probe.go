// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package stackenv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Service — служба под пробой: где её чарт, под каким ключом умбреллы её
// значения и как она поднимается.
type Service struct {
	// Name — имя для переписи.
	Name string
	// Root — корень дерева продукта относительно каталога пакета пробы.
	Root string
	// Chart — каталог чарта службы относительно Root.
	Chart string
	// ValuesKey — ключ подчарта в значениях умбреллы.
	ValuesKey string
	// EnvPrefix — приставка имён службы; по ней находится основной контейнер.
	EnvPrefix string
	// Boot — стражи старта службы на ТЕКУЩЕМ окружении процесса: загрузка
	// конфигурации тем же вызовом и те же стражи, что исполняет процесс.
	// Аргументы контейнера и смонтированные файлы — в Env (пути уже переписаны на
	// положенные копии). Имена стражей — для переписи.
	Boot       func(Env) error
	GuardNames []string
}

// Finding — стенд, с объявлением которого служба не поднимется.
type Finding struct {
	Chain Chain
	Err   error
}

// Report — исход пробы по службе.
type Report struct {
	ChainsRead   int
	ChainsRaised int // стендов, поднимающих службу
	EnvNames     int // имён окружения, поданных стражам (сумма по стендам)
	FromSecret   int // из них доставлено секретом
	Files        int // смонтированных файлов конфигурации (сумма по стендам)
	Findings     []Finding
}

// CensusLine — перепись осмотренного.
func (r Report) CensusLine(s Service) string {
	return fmt.Sprintf("служба %s: цепочек прочитано %d · поднимают службу %d · стражей опрошено %d (%s) · "+
		"имён окружения подано %d, из них доставлено секретом %d · смонтированных файлов %d · "+
		"стендов, где старт отказал, %d",
		s.Name, r.ChainsRead, r.ChainsRaised, len(s.GuardNames), strings.Join(s.GuardNames, ", "),
		r.EnvNames, r.FromSecret, r.Files, len(r.Findings))
}

// Judge — проба без привязки к `testing`: стенд за стендом рендерит окружение
// службы, подаёт его стражам и возвращает исход значением. Окружение процесса
// меняется на время вопроса и возвращается на место.
func Judge(s Service, workDir string) (Report, error) {
	chains, err := ReadChains(filepath.Join(s.Root, "deploy", "stacks.txt"))
	if err != nil {
		return Report{}, err
	}
	var r Report
	umbrella := filepath.Join(s.Root, "deploy", "helm", "umbrella")
	for _, c := range chains {
		r.ChainsRead++
		values, err := SubchartValues(umbrella, c, s.ValuesKey)
		if err != nil {
			return r, err
		}
		if !Enabled(values) {
			continue
		}
		r.ChainsRaised++
		dir, err := os.MkdirTemp(workDir, "stack-"+c.Name+"-")
		if err != nil {
			return r, err
		}
		env, err := Render(filepath.Join(s.Root, s.Chart), values, s.EnvPrefix, dir)
		if err != nil {
			return r, fmt.Errorf("стенд %s: %w", c.Name, err)
		}
		files, err := env.Materialize(filepath.Join(dir, "root"))
		if err != nil {
			return r, fmt.Errorf("стенд %s: %w", c.Name, err)
		}
		r.Files += files
		n, bootErr := bootWith(env, s.Boot)
		r.EnvNames += n
		r.FromSecret += len(env.FromSecret)
		if bootErr != nil {
			r.Findings = append(r.Findings, Finding{Chain: c, Err: bootErr})
		}
	}
	if r.ChainsRaised == 0 {
		return r, fmt.Errorf("НЕ ВЫПОЛНИЛОСЬ: из %d стендов службу %s не поднимает ни один — стражам нечего "+
			"подать, и «отказов нет» здесь значило бы «не спрашивали»", r.ChainsRead, s.Name)
	}
	return r, nil
}

// bootWith выставляет окружение рендера (только имена платформы `KACHO_`),
// снимает прочие имена платформы и зовёт стражей. Возвращает число поданных имён.
func bootWith(env Env, boot func(Env) error) (int, error) {
	saved := map[string]string{}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "KACHO_") {
			saved[k] = v
			_ = os.Unsetenv(k)
		}
	}
	defer func() {
		for _, kv := range os.Environ() {
			if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "KACHO_") {
				_ = os.Unsetenv(k)
			}
		}
		for k, v := range saved {
			_ = os.Setenv(k, v)
		}
	}()
	names := make([]string, 0, len(env.Values))
	for k := range env.Values {
		if strings.HasPrefix(k, "KACHO_") {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	for _, k := range names {
		if err := os.Setenv(k, env.Values[k]); err != nil {
			return 0, err
		}
	}
	return len(names), boot(env)
}

// T — то, что пробе нужно от исполнителя.
type T interface {
	Helper()
	TempDir() string
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
	Skipf(format string, args ...any)
	Logf(format string, args ...any)
}

// Probe — проба службы: каждый стенд, поднимающий службу, объявляет то, чего
// требуют её стражи старта.
//
// Без исполнителя рендера проба в конвейере ОТКАЗЫВАЕТ (там helm обязан быть), а
// на машине разработки пропускается с тем же текстом «не выполнилось» — так же,
// как остальные пробы рендера дерева.
func Probe(t T, s Service) {
	t.Helper()
	r, err := Judge(s, t.TempDir())
	if errors.Is(err, ErrNoHelm) {
		if os.Getenv("CI") != "" {
			t.Fatalf("%v: в конвейере исполнитель рендера обязан быть", err)
			return
		}
		t.Skipf("%v", err)
		return
	}
	if err != nil {
		t.Fatalf("служба %s: %v", s.Name, err)
		return
	}
	t.Logf("%s", r.CensusLine(s))
	for _, f := range r.Findings {
		t.Errorf("стенд %q (цепочка %s): с объявленным окружением служба %s НЕ ПОДНИМЕТСЯ — страж старта "+
			"отвечает:\n    %v\nОбъяви недостающее в профиле этой цепочки; отказ стража здесь верен, "+
			"неверно лишь то, что он узнавался бы подъёмом стенда",
			f.Chain.Name, strings.Join(f.Chain.Profiles, " → "), s.Name, f.Err)
	}
}

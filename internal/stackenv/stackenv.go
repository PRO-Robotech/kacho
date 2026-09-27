// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package stackenv отвечает на вопрос: с каким ОКРУЖЕНИЕМ контейнер службы
// поднимается на каждом развёртываемом стенде — и выдержит ли его страж старта
// (kacho#941).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Требование живёт в композиционном корне службы (страж старта), а объявление —
// в цепочке файлов значений умбреллы. Сверять их было некому: профиль, не
// объявивший того, что страж требует, узнавался только подъёмом стенда —
// `helm --wait`, под в перезапуске по кругу, 10–25 минут и занятый ранер. Отказ
// службы при этом верен; неверно, что узнаётся он так поздно.
//
// ─────────────────────────────────────────────────────────────────────────────
// КАК ОТВЕЧАЕТ
//
//  1. Цепочки читаются из `deploy/stacks.txt` — единственного места, где они
//     объявлены; рукописная копия цепочки уже расходилась с деревом.
//  2. Значения подчарта службы складываются так же, как их складывает helm:
//     `values.yaml` умбреллы, затем профили цепочки слева направо; от каждого
//     слоя берётся поддерево службы и `global`. Карты сливаются по ключам, прочее
//     замещается целиком.
//  3. Чарт службы РЕНДЕРИТСЯ настоящим `helm template` с этими значениями, и из
//     рендера снимается окружение основного контейнера: литералы, ссылки на
//     ConfigMap того же рендера, `envFrom`. Вторая сборка окружения по текстам
//     шаблонов разошлась бы с тем, что получает под, молча.
//  4. Значение, которое приезжает из СЕКРЕТА (`secretKeyRef`, `envFrom.secretRef`),
//     в объявлении пусто намеренно: учётные данные не живут в профиле. Такое имя
//     не считается незаполненным — оно помечается доставленным секретом и
//     получает непустую подстановку, а перепись называет их число. Наивная
//     проба, читающая только объявления, помечала бы исправные цепочки.
//
// Окружение подаётся ТОМУ ЖЕ загрузчику и ТЕМ ЖЕ стражам, что исполняет процесс
// на старте: второй предикат, сформулированный в пробе заново, разошёлся бы с
// первым там, где расхождение не видно.
package stackenv

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Chain — развёртываемый стенд: имя и профили в порядке наложения.
type Chain struct {
	Name     string
	Profiles []string
}

// ReadChains читает таблицу стендов. Нераспознанная строка и пустая таблица —
// отказ: «стендов меньше» и «предикат перестал узнавать строки» неразличимы.
func ReadChains(stacksFile string) ([]Chain, error) {
	raw, err := os.ReadFile(stacksFile)
	if err != nil {
		return nil, fmt.Errorf("таблица стендов %s не читается: %w", stacksFile, err)
	}
	var out []Chain
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, chain, ok := strings.Cut(line, ":")
		if !ok || name == "" || chain == "" {
			return nil, fmt.Errorf("%s:%d: строка таблицы стендов не разобрана: %q", stacksFile, i+1, line)
		}
		out = append(out, Chain{Name: name, Profiles: strings.Split(chain, ",")})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("в %s ни одной строки стенда", stacksFile)
	}
	return out, nil
}

// Merge накладывает src на dst так, как helm накладывает файлы значений.
func Merge(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for k, v := range src {
		if sub, ok := v.(map[string]any); ok {
			if cur, ok := dst[k].(map[string]any); ok {
				dst[k] = Merge(cur, sub)
				continue
			}
			dst[k] = Merge(map[string]any{}, sub)
			continue
		}
		dst[k] = v
	}
	return dst
}

func readYAMLMap(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("разбор %s: %w", path, err)
	}
	return m, nil
}

// SubchartValues — значения подчарта службы на стенде: поддерево key и `global`
// из `values.yaml` умбреллы и из каждого профиля цепочки, по порядку.
func SubchartValues(umbrellaDir string, chain Chain, key string) (map[string]any, error) {
	layers := append([]string{"values.yaml"}, chain.Profiles...)
	out := map[string]any{}
	for _, layer := range layers {
		tree, err := readYAMLMap(filepath.Join(umbrellaDir, layer))
		if err != nil {
			return nil, fmt.Errorf("стенд %s, слой %s: %w", chain.Name, layer, err)
		}
		if sub, ok := tree[key].(map[string]any); ok {
			out = Merge(out, sub)
		}
		if g, ok := tree["global"].(map[string]any); ok {
			out = Merge(out, map[string]any{"global": g})
		}
	}
	return out, nil
}

// Enabled — поднимает ли стенд службу. Условие подчарта `<key>.enabled` без
// значения helm не применяет: зависимость остаётся включённой.
func Enabled(values map[string]any) bool {
	v, ok := values["enabled"]
	if !ok {
		return true
	}
	b, isBool := v.(bool)
	return !isBool || b
}

// Env — окружение основного контейнера службы в рендере.
type Env struct {
	Container string
	// Values — имя → значение; у имён из секрета — подстановка SecretPlaceholder.
	Values map[string]string
	// FromSecret — имена, чьё значение приезжает секретом, а не объявлением.
	FromSecret []string
	// FromField — имена из полей пода (fieldRef/resourceFieldRef): подстановка.
	FromField []string
	// Args — аргументы основного контейнера.
	Args []string
	// Files — файлы, которые под получает из ConfigMap того же рендера
	// (смонтированный том): путь в контейнере → содержимое. Конфигурация
	// части служб приезжает именно так, а не переменными.
	Files map[string]string
}

// Flag — значение аргумента `name` (форма `name value` либо `name=value`); "" — нет.
func (e Env) Flag(name string) string {
	for i, a := range e.Args {
		if a == name && i+1 < len(e.Args) {
			return e.Args[i+1]
		}
		if v, ok := strings.CutPrefix(a, name+"="); ok {
			return v
		}
	}
	return ""
}

// Materialize кладёт смонтированные файлы под root, сохраняя их пути, и
// переписывает на новые пути всё, что на них ссылается: значения окружения и
// аргументы. Возвращает число положенных файлов.
func (e *Env) Materialize(root string) (int, error) {
	paths := make([]string, 0, len(e.Files))
	for p := range e.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		dst := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return 0, err
		}
		if err := os.WriteFile(dst, []byte(e.Files[p]), 0o600); err != nil {
			return 0, err
		}
	}
	rewrite := func(v string) string {
		for _, p := range paths {
			if v == p {
				return filepath.Join(root, p)
			}
		}
		return v
	}
	for k, v := range e.Values {
		e.Values[k] = rewrite(v)
	}
	for i, a := range e.Args {
		if name, val, ok := strings.Cut(a, "="); ok && strings.HasPrefix(name, "-") {
			e.Args[i] = name + "=" + rewrite(val)
			continue
		}
		e.Args[i] = rewrite(a)
	}
	return len(paths), nil
}

// SecretPlaceholder — непустая подстановка для значения из секрета. Только буквы,
// цифры и дефис: подстановка попадает и в строку подключения (пароль внутри
// адреса базы), и двоеточие или косая черта сломали бы её разбор — отказ пришёл
// бы от подстановки, а не от объявления.
func SecretPlaceholder(secret, key string) string {
	clean := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
				return r
			}
			return '-'
		}, s)
	}
	return "stackenv-secret-" + clean(secret) + "-" + clean(key)
}

// ErrNoHelm — исполнителя рендера нет: условие пробы не создано.
var ErrNoHelm = errors.New("НЕ ВЫПОЛНИЛОСЬ: helm не найден в PATH")

// Render рендерит чарт с данными значениями и снимает окружение контейнера,
// несущего больше всего имён с приставкой envPrefix.
func Render(chartDir string, values map[string]any, envPrefix string, workDir string) (Env, error) {
	helm, err := exec.LookPath("helm")
	if err != nil {
		return Env{}, ErrNoHelm
	}
	raw, err := yaml.Marshal(values)
	if err != nil {
		return Env{}, err
	}
	vf := filepath.Join(workDir, "stackenv-values.yaml")
	if err := os.WriteFile(vf, raw, 0o600); err != nil {
		return Env{}, err
	}
	cmd := exec.Command(helm, "template", "kacho", chartDir, "--namespace", "kacho", "-f", vf)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return Env{}, fmt.Errorf("helm template %s: %w\n%s", chartDir, err, stderr.String())
	}
	return EnvFromManifests(stdout.Bytes(), envPrefix)
}

type k8sDoc struct {
	Kind     string                `yaml:"kind"`
	Metadata struct{ Name string } `yaml:"metadata"`
	Data     map[string]string     `yaml:"data"`
	Spec     struct {
		Template struct {
			Spec struct {
				Containers []k8sContainer `yaml:"containers"`
				Volumes    []struct {
					Name      string `yaml:"name"`
					ConfigMap *struct {
						Name  string `yaml:"name"`
						Items []struct {
							Key  string `yaml:"key"`
							Path string `yaml:"path"`
						} `yaml:"items"`
					} `yaml:"configMap"`
				} `yaml:"volumes"`
			} `yaml:"spec"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

type k8sContainer struct {
	Name         string   `yaml:"name"`
	Args         []string `yaml:"args"`
	VolumeMounts []struct {
		Name      string `yaml:"name"`
		MountPath string `yaml:"mountPath"`
		SubPath   string `yaml:"subPath"`
	} `yaml:"volumeMounts"`
	Env []struct {
		Name      string  `yaml:"name"`
		Value     *string `yaml:"value"`
		ValueFrom *struct {
			SecretKeyRef    *struct{ Name, Key string } `yaml:"secretKeyRef"`
			ConfigMapKeyRef *struct{ Name, Key string } `yaml:"configMapKeyRef"`
			FieldRef        *struct {
				FieldPath string `yaml:"fieldPath"`
			} `yaml:"fieldRef"`
			ResourceFieldRef *struct {
				Resource string `yaml:"resource"`
			} `yaml:"resourceFieldRef"`
		} `yaml:"valueFrom"`
	} `yaml:"env"`
	EnvFrom []struct {
		ConfigMapRef *struct{ Name string } `yaml:"configMapRef"`
		SecretRef    *struct{ Name string } `yaml:"secretRef"`
	} `yaml:"envFrom"`
}

// EnvFromManifests — окружение основного контейнера из готового рендера.
//
// Порядок как у kubelet: сначала `envFrom` (ConfigMap целиком), затем `env`, и
// `env` перекрывает. Ссылка на ConfigMap, которого в рендере нет, — отказ:
// под с такой ссылкой не стартует.
func EnvFromManifests(manifests []byte, envPrefix string) (Env, error) {
	dec := yaml.NewDecoder(bytes.NewReader(manifests))
	configMaps := map[string]map[string]string{}
	type podContainer struct {
		c       k8sContainer
		volumes map[string]string            // имя тома → имя ConfigMap
		items   map[string]map[string]string // имя тома → ключ → путь внутри тома
	}
	var containers []podContainer
	for {
		var d k8sDoc
		err := dec.Decode(&d)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Env{}, fmt.Errorf("разбор рендера: %w", err)
		}
		switch d.Kind {
		case "ConfigMap":
			configMaps[d.Metadata.Name] = d.Data
		case "Deployment", "StatefulSet", "DaemonSet":
			vols := map[string]string{}
			items := map[string]map[string]string{}
			for _, v := range d.Spec.Template.Spec.Volumes {
				if v.ConfigMap == nil {
					continue
				}
				vols[v.Name] = v.ConfigMap.Name
				if len(v.ConfigMap.Items) > 0 {
					items[v.Name] = map[string]string{}
					for _, it := range v.ConfigMap.Items {
						items[v.Name][it.Key] = it.Path
					}
				}
			}
			for _, c := range d.Spec.Template.Spec.Containers {
				containers = append(containers, podContainer{c: c, volumes: vols, items: items})
			}
		}
	}
	best, bestScore := -1, 0
	for i, pc := range containers {
		score := 0
		for _, e := range pc.c.Env {
			if strings.HasPrefix(e.Name, envPrefix) {
				score++
			}
		}
		if score > bestScore {
			best, bestScore = i, score
		}
	}
	if best < 0 {
		return Env{}, fmt.Errorf("в рендере нет контейнера с именами %s* — основной контейнер службы не найден "+
			"(контейнеров %d, ConfigMap %d)", envPrefix, len(containers), len(configMaps))
	}
	c := containers[best].c
	out := Env{Container: c.Name, Values: map[string]string{}, Args: append([]string(nil), c.Args...),
		Files: map[string]string{}}
	for _, m := range c.VolumeMounts {
		cmName, ok := containers[best].volumes[m.Name]
		if !ok {
			continue
		}
		data, ok := configMaps[cmName]
		if !ok {
			return Env{}, fmt.Errorf("том %q ссылается на ConfigMap %q, которого в рендере нет", m.Name, cmName)
		}
		keys := containers[best].items[m.Name]
		for k, v := range data {
			rel := k
			if keys != nil {
				p, listed := keys[k]
				if !listed {
					continue
				}
				rel = p
			}
			switch {
			case m.SubPath == "":
				out.Files[filepath.Join(m.MountPath, rel)] = v
			case m.SubPath == rel:
				out.Files[m.MountPath] = v
			}
		}
	}
	for _, ef := range c.EnvFrom {
		switch {
		case ef.ConfigMapRef != nil:
			data, ok := configMaps[ef.ConfigMapRef.Name]
			if !ok {
				return Env{}, fmt.Errorf("envFrom ссылается на ConfigMap %q, которого в рендере нет", ef.ConfigMapRef.Name)
			}
			for k, v := range data {
				out.Values[k] = v
			}
		case ef.SecretRef != nil:
			return Env{}, fmt.Errorf("envFrom.secretRef %q: имена секрета целиком из объявления не выводятся — "+
				"предмет пробы здесь не определён", ef.SecretRef.Name)
		}
	}
	for _, e := range c.Env {
		switch {
		case e.Value != nil:
			out.Values[e.Name] = *e.Value
		case e.ValueFrom == nil:
			out.Values[e.Name] = ""
		case e.ValueFrom.SecretKeyRef != nil:
			out.Values[e.Name] = SecretPlaceholder(e.ValueFrom.SecretKeyRef.Name, e.ValueFrom.SecretKeyRef.Key)
			out.FromSecret = append(out.FromSecret, e.Name)
		case e.ValueFrom.ConfigMapKeyRef != nil:
			data, ok := configMaps[e.ValueFrom.ConfigMapKeyRef.Name]
			if !ok {
				return Env{}, fmt.Errorf("%s: ссылка на ConfigMap %q, которого в рендере нет", e.Name, e.ValueFrom.ConfigMapKeyRef.Name)
			}
			out.Values[e.Name] = data[e.ValueFrom.ConfigMapKeyRef.Key]
		case e.ValueFrom.FieldRef != nil:
			out.Values[e.Name] = "stackenv-field:" + e.ValueFrom.FieldRef.FieldPath
			out.FromField = append(out.FromField, e.Name)
		case e.ValueFrom.ResourceFieldRef != nil:
			out.Values[e.Name] = "1"
			out.FromField = append(out.FromField, e.Name)
		}
	}
	sort.Strings(out.FromSecret)
	sort.Strings(out.FromField)
	return out, nil
}

// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package standns

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Операторская личность внутреннего REST-слушателя края в стенде своего
// пространства имён (kacho#3151).
//
// Лист оператора выпускает умбрелла (internal-rest-operator-certificate.yaml),
// круг клиентов края — чарт края (KACHO_API_GATEWAY_INTERNAL_REST_CLIENT_SANS).
// Пока лист брал пространство личности из пространства релиза, а круг — из
// закреплённого mtls.spiffe.namespace края, в любом пространстве, кроме
// рабочего, они расходились молча: рендер и установка зелёные, а край отвергает
// лист оператора рукопожатием, и стенд проб не поднимается. Отказ здесь —
// на рендере, до первой записи в кластер.
const (
	// OperatorCertName — Certificate умбреллы с листом операторской личности.
	OperatorCertName = "kacho-internal-rest-operator-client-tls"
	// OperatorCircleKnob — переменная края с кругом клиентов внутреннего слушателя.
	OperatorCircleKnob = "KACHO_API_GATEWAY_INTERNAL_REST_CLIENT_SANS"
)

// operatorCircleFacts — что рендер говорит о личности оператора и круге края.
type operatorCircleFacts struct {
	leaves  int      // листов оператора в рендере
	uris    []string // URI-имена этих листов
	circles int      // мест, объявивших круг
	circle  map[string]bool
}

func collectOperatorCircle(docs []*doc) operatorCircleFacts {
	f := operatorCircleFacts{circle: map[string]bool{}}
	for _, d := range docs {
		if d.kind == "Certificate" && d.name == OperatorCertName {
			f.leaves++
			if uris := child(child(d.root, "spec"), "uris"); uris != nil && uris.Kind == yaml.SequenceNode {
				for _, u := range uris.Content {
					if u.Kind == yaml.ScalarNode && u.Value != "" {
						f.uris = append(f.uris, u.Value)
					}
				}
			}
		}
		walkEnv(d.root, func(name, value string) {
			if name != OperatorCircleKnob {
				return
			}
			f.circles++
			for _, s := range strings.Split(value, ",") {
				if s = strings.TrimSpace(s); s != "" {
					f.circle[s] = true
				}
			}
		})
	}
	return f
}

// walkEnv — каждая запись `env` (name + value) любого контейнера документа.
func walkEnv(n *yaml.Node, visit func(name, value string)) {
	if n == nil {
		return
	}
	if n.Kind == yaml.MappingNode {
		if env := child(n, "env"); env != nil && env.Kind == yaml.SequenceNode {
			for _, e := range env.Content {
				if name := scalar(e, "name"); name != "" {
					visit(name, scalar(e, "value"))
				}
			}
		}
	}
	for _, c := range n.Content {
		walkEnv(c, visit)
	}
}

// judgeOperatorCircle — находки о личности оператора; пустой срез — рендер годен.
// Рендер без листа оператора судить нечего (PKI выключена). Лист без круга,
// лист без имени и имя вне круга — находки: каждая значит, что оператор в
// стенд не войдёт.
func judgeOperatorCircle(f operatorCircleFacts) []string {
	if f.leaves == 0 {
		return nil
	}
	var out []string
	if f.circles == 0 || len(f.circle) == 0 {
		out = append(out, fmt.Sprintf("Certificate %s в рендере есть, а круга %s у края нет — оператору некуда войти",
			OperatorCertName, OperatorCircleKnob))
		return out
	}
	if len(f.uris) == 0 {
		out = append(out, fmt.Sprintf("Certificate %s без URI-имени — край опознаёт оператора только по нему", OperatorCertName))
	}
	circle := make([]string, 0, len(f.circle))
	for s := range f.circle {
		circle = append(circle, s)
	}
	sort.Strings(circle)
	for _, u := range f.uris {
		if !f.circle[u] {
			out = append(out, fmt.Sprintf("имя оператора %s (Certificate %s) вне круга края %s=%s: край отвергнет лист рукопожатием",
				u, OperatorCertName, OperatorCircleKnob, strings.Join(circle, ",")))
		}
	}
	return out
}

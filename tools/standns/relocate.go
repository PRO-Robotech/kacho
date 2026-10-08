// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package standns — перенос отрендеренного стенда в СВОЁ пространство имён
// общего кластера (kacho#3102, `make -C deploy stand-ns-up`).
//
// # Предмет
//
// Зонтичный чарт пишется под одно пространство имён на кластер. Три вещи в его
// рендере это пространство подразумевают, и рендер `helm -n <другое>` их не
// переносит:
//
//   - адреса соседей в значениях — `<служба>.kacho.svc[:порт]` литералами
//     (профили, умолчания подчартов, шаблоны). Стенд в другом пространстве
//     ходил бы к службам РАБОЧЕГО стенда `kacho`;
//   - общекластерные издатели внутреннего CA (`ClusterIssuer`) и корень CA в
//     пространстве ресурсов cert-manager — под ОДНИМИ именами на кластер. Второй
//     релиз упёрся бы во владение чужого, а общий корень сделал бы листы
//     тестового стенда доверенными рабочему;
//   - входы (`Ingress`) с хостами рабочего стенда: на кластере с контроллером
//     входа они делили бы хост с рабочим стендом.
//
// Перенос делает пост-обработчик рендера helm (`deploy/helm/plugins/kacho-stand-ns`)
// над РАЗОБРАННЫМ YAML: правятся узлы-значения, а не текст, и только строки.
//
// # Отказ, а не перенос
//
// Объект, который перенести нельзя, — отказ всего рендера, а не пропуск:
// любой общекластерный вид, кроме издателя CA (контроллер входа, вебхук,
// ClusterRole), объект в чужом пространстве, кроме корня CA, и сам перенос в
// `kacho`. Тестовый стенд не ставит общекластерного, он переиспользует.
package standns

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"

	"gopkg.in/yaml.v3"
)

// StandLabel — метка, которой помечается всё, что стенд заводит ВНЕ своего
// пространства имён (издатели CA, корень CA). Снятие стенда снимает и это:
// пространство имён свои объекты уносит, чужие — нет.
const StandLabel = "kacho.io/stand-ns"

// WorkingNamespace — пространство рабочего стенда. Переносить в него нечего, и
// перенос туда — отказ: рабочий стенд выкатывается своей целью.
const WorkingNamespace = "kacho"

// clusterScoped — виды, у которых пространства имён нет. Перечень закрыт
// намеренно: неизвестный вид без пространства считается namespaced (helm
// поставит его в пространство релиза), а известный общекластерный, кроме
// издателя, — отказ.
var clusterScoped = map[string]bool{
	"ClusterIssuer": true, "ClusterRole": true, "ClusterRoleBinding": true,
	"IngressClass": true, "ValidatingWebhookConfiguration": true,
	"MutatingWebhookConfiguration": true, "CustomResourceDefinition": true,
	"Namespace": true, "PriorityClass": true, "StorageClass": true,
	"PersistentVolume": true, "APIService": true, "GatewayClass": true,
	"ClusterPolicy": true, "CSIDriver": true, "RuntimeClass": true,
}

// Options — куда переносится стенд.
type Options struct {
	Namespace   string // пространство стенда
	CANamespace string // пространство ресурсов cert-manager (там рождается корень CA)
}

// Census — объём осмотренного переносом. Печатается целиком: «перенесено 0»
// обязано быть отличимо от «осмотрено 0».
type Census struct {
	Documents        int
	AddressRewrites  int
	IssuersRenamed   int
	IssuerRefs       int
	CARootsRenamed   int
	IngressesDropped int
}

func (c Census) String() string {
	return fmt.Sprintf("документов %d; адресов соседей перенесено %d; издателей CA переименовано %d, "+
		"ссылок на них %d; корней CA %d; входов снято %d",
		c.Documents, c.AddressRewrites, c.IssuersRenamed, c.IssuerRefs, c.CARootsRenamed, c.IngressesDropped)
}

var dnsName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// workingAddress — адрес соседа в пространстве рабочего стенда: `<имя>.kacho.svc`
// с необязательным `.cluster.local` и портом. Граница слева — точка (имя
// службы), справа — конец имени: `kacho.svcx` и `notkacho.svc` не совпадают.
var workingAddress = regexp.MustCompile(`\.` + WorkingNamespace + `\.svc\b`)

type doc struct {
	node *yaml.Node // DocumentNode
	root *yaml.Node // MappingNode
	kind string
	name string
	ns   string
}

// Relocate читает поток манифестов, переносит его в opts.Namespace и пишет
// результат. Ошибка — отказ всего рендера.
func Relocate(in io.Reader, out io.Writer, opts Options) (Census, error) {
	var census Census
	if !dnsName.MatchString(opts.Namespace) || len(opts.Namespace) > 63 {
		return census, fmt.Errorf("пространство имён %q не является именем DNS-1123", opts.Namespace)
	}
	if opts.Namespace == WorkingNamespace {
		return census, fmt.Errorf("перенос в %q — пространство рабочего стенда; его выкатывает своя цель, а не перенос", WorkingNamespace)
	}
	if !dnsName.MatchString(opts.CANamespace) {
		return census, fmt.Errorf("пространство ресурсов cert-manager %q не названо или не является именем DNS-1123", opts.CANamespace)
	}
	suffix := "-" + opts.Namespace

	dec := yaml.NewDecoder(in)
	var docs []*doc
	for {
		var n yaml.Node
		err := dec.Decode(&n)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return census, fmt.Errorf("рендер не разбирается как YAML: %w", err)
		}
		if len(n.Content) == 0 || n.Content[0].Kind != yaml.MappingNode {
			continue
		}
		d := &doc{node: &n, root: n.Content[0]}
		d.kind = scalar(d.root, "kind")
		meta := child(d.root, "metadata")
		d.name = scalar(meta, "name")
		d.ns = scalar(meta, "namespace")
		docs = append(docs, d)
	}
	census.Documents = len(docs)

	// Проход 1: что переименовывается. Ссылка может стоять раньше объекта.
	issuers := map[string]string{}
	caSecrets := map[string]string{}
	for _, d := range docs {
		where := fmt.Sprintf("%s/%s", d.kind, d.name)
		switch {
		case d.kind == "ClusterIssuer":
			issuers[d.name] = d.name + suffix
		case clusterScoped[d.kind]:
			return census, fmt.Errorf("%s: общекластерный объект в стенде пространства имён. Стенд проб общекластерного не ставит, "+
				"он переиспользует существующее; выключи компонент в накладке стенда", where)
		case d.ns == "" || d.ns == opts.Namespace:
		case d.ns == opts.CANamespace && d.kind == "Certificate":
			if sec := scalar(child(d.root, "spec"), "secretName"); sec != "" {
				caSecrets[sec] = sec + suffix
			}
		default:
			return census, fmt.Errorf("%s: объект в пространстве %q, а стенд — в %q. Объект в чужом пространстве снятие стенда не снимет, "+
				"а в %q — перепишет рабочий стенд", where, d.ns, opts.Namespace, WorkingNamespace)
		}
	}

	// Проход 2: перенос.
	var kept []*doc
	for _, d := range docs {
		meta := child(d.root, "metadata")
		switch {
		case d.kind == "Ingress":
			census.IngressesDropped++
			continue
		case d.kind == "ClusterIssuer":
			setScalar(meta, "name", issuers[d.name])
			label(meta, opts.Namespace)
			census.IssuersRenamed++
			if ca := child(child(d.root, "spec"), "ca"); ca != nil {
				if sec := scalar(ca, "secretName"); caSecrets[sec] != "" {
					setScalar(ca, "secretName", caSecrets[sec])
				}
			}
		case d.ns == opts.CANamespace && d.kind == "Certificate":
			spec := child(d.root, "spec")
			sec := scalar(spec, "secretName")
			setScalar(meta, "name", d.name+suffix)
			setScalar(spec, "secretName", caSecrets[sec])
			label(meta, opts.Namespace)
			// Секрет корня рождает cert-manager; метка на нём — через шаблон
			// секрета, иначе снятие стенда его не найдёт.
			tmpl := ensureMap(spec, "secretTemplate")
			label(tmpl, opts.Namespace)
			census.CARootsRenamed++
		}
		census.IssuerRefs += renameIssuerRefs(d.root, issuers)
		census.AddressRewrites += rewriteAddresses(d.root, opts.Namespace)
		kept = append(kept, d)
	}

	var buf bytes.Buffer
	for i, d := range kept {
		if i > 0 {
			buf.WriteString("---\n")
		}
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(d.node); err != nil {
			return census, fmt.Errorf("%s/%s не сериализуется: %w", d.kind, d.name, err)
		}
		_ = enc.Close()
	}
	_, err := out.Write(buf.Bytes())
	return census, err
}

// renameIssuerRefs — ссылки на переименованные издатели: карта `issuerRef`
// (имя + вид ClusterIssuer) и аннотация `cert-manager.io/cluster-issuer`.
func renameIssuerRefs(n *yaml.Node, issuers map[string]string) int {
	count := 0
	var walk func(*yaml.Node)
	walk = func(n *yaml.Node) {
		if n == nil {
			return
		}
		if n.Kind == yaml.MappingNode {
			if scalar(n, "kind") == "ClusterIssuer" {
				if to, ok := issuers[scalar(n, "name")]; ok && child(n, "apiVersion") == nil {
					setScalar(n, "name", to)
					count++
				}
			}
			for i := 0; i+1 < len(n.Content); i += 2 {
				k, v := n.Content[i], n.Content[i+1]
				if k.Value == "cert-manager.io/cluster-issuer" && v.Kind == yaml.ScalarNode {
					if to, ok := issuers[v.Value]; ok {
						v.Value = to
						count++
					}
				}
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(n)
	return count
}

// rewriteAddresses — каждое строковое значение с адресом соседа в рабочем
// пространстве переносится в пространство стенда. Ключи не трогаются.
func rewriteAddresses(n *yaml.Node, ns string) int {
	count := 0
	var walk func(n *yaml.Node, isKey bool)
	walk = func(n *yaml.Node, isKey bool) {
		if n == nil {
			return
		}
		switch n.Kind {
		case yaml.ScalarNode:
			if isKey || (n.Tag != "!!str" && n.Tag != "") {
				return
			}
			if hits := len(workingAddress.FindAllStringIndex(n.Value, -1)); hits > 0 {
				n.Value = workingAddress.ReplaceAllString(n.Value, "."+ns+".svc")
				count += hits
			}
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				walk(n.Content[i], true)
				walk(n.Content[i+1], false)
			}
		default:
			for _, c := range n.Content {
				walk(c, false)
			}
		}
	}
	walk(n, false)
	return count
}

func child(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func scalar(m *yaml.Node, key string) string {
	if v := child(m, key); v != nil && v.Kind == yaml.ScalarNode {
		return v.Value
	}
	return ""
}

func setScalar(m *yaml.Node, key, value string) {
	if v := child(m, key); v != nil && v.Kind == yaml.ScalarNode {
		v.Value = value
		v.Style = 0
		v.Tag = "!!str"
		return
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}

func ensureMap(m *yaml.Node, key string) *yaml.Node {
	if v := child(m, key); v != nil && v.Kind == yaml.MappingNode {
		return v
	}
	v := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, v)
	return v
}

func label(meta *yaml.Node, ns string) {
	setScalar(ensureMap(meta, "labels"), StandLabel, ns)
}

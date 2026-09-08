# kaname sub-chart (KAC-127 Phase 3)

Identity / Access Management control-plane service for Kachō Cloud.
Account / Project / User / ServiceAccount / Group / Role / AccessBinding +
WebAuthn/Passkey AuthN (Phase 2) + ReBAC AuthZ.

This sub-chart is owned by the `kacho-umbrella` chart (`deploy/helm/umbrella/`) and is not intended
for standalone deployment. The umbrella manages cross-cutting concerns
(NetworkPolicies) at the parent level; this sub-chart only declares the kaname
Deployment + its supporting ConfigMap / RBAC / Service objects.

## Phase 3 additions

> [!note] Наложение правил снято вместе со своим потребителем (#2141).
> Служба объявляет в собственном исходнике, что решение о доступе принимает
> единственный механизм — модель прав. Поставка при этом продолжала возить
> боковой контейнер правил, пять файлов правил, карты его настроек, ручки в
> профилях и две сетевые политики. Ни один профиль контейнер не включал, то
> есть механизм не работал ни на одной посадке, — но обещание «правила
> применяются» стояло в поставке и читалось как действующее.
>
> Ушли: контейнер и его карты, каталог `files/opa-policies/`, метка пода,
> ручка включения во всех профилях и сетевые политики выдачи пакета правил.
> Имя снятой ручки здесь намеренно не воспроизводится в обратных кавычках:
> так оно читается как живая настройка, и оператор задал бы её, не получив
> ничего.
> Отсутствие остатков держит `deploy/rules_overlay_left_no_remnant_test.go`;
> он же читает ПРЕДПОСЫЛКУ и отказывает другим текстом, если потребитель
> вернётся.

> [!note] Внешний движок отношений снят вместе со своей посадкой (S6 эпика #747).
> Решение о доступе вычисляет реляционная форма в собственной базе iam, поэтому
> из чарта ушли: подчарт начальной настройки движка, выделенная база движка,
> секрет с идентификатором модели, init-контейнер ожидания движка, переменные
> `KANAME_OPENFGA_STORE_ID` / `KANAME_OPENFGA_MODEL_ID`, ключ
> `config.extapi.openfga.*` и рубильник источника вердикта
> (`config.authz.verdictFormTypes` / `config.authz.shadowCompare`) — сравнивать
> больше не с чем. Сама МОДЕЛЬ прав (`fga_model.fga`) остаётся: она источник
> истины формы и разбирается службой, а не движком.

## Подпись пакета правил — раздела больше нет, и вот почему

Здесь стоял разбор ротации ключа подписи пакета правил на 180 дней: расписание,
процедура на день ноль, аварийный порядок при утечке приватной половины. Весь он
описывал **нереализованный замысел** и сам это оговаривал — ни ротатора, ни
подписи пакетов в коде службы не существовало ни одного пакета.

Раздел снят вместе со своим предметом (#2141): наложение правил убрано из
поставки, потому что у него не осталось потребителя. Плана без исполнителя не
держим — ban #11 не различает отсрочку в коде и отсрочку в тексте: и та и другая
переживает своё основание и читается следующим как действующая.

Понадобится подпись пакетов — она приходит вместе со своим ключом, своей
ротацией и своим механизмом, и описывается тогда же. Ключ шифрования набора
ключей (`kaname-jwks-enc-key`, раздел ниже) к этому отношения не имеет: это
другой предмет с другим сроком жизни.

## Sealed-secret integration (operator setup)

For production deployments, the 32-byte AES-GCM key
(`KANAME_JWKS_ENC_KEY`, default Secret name `kaname-jwks-enc-key`) must
be provisioned BEFORE first-deploy of this chart. Two supported patterns:

> [!important] THE VALUE IS HEX — 64 hexadecimal characters, nothing else.
> `authn.jwks-encryption-key-hex` decodes what it is given as hex and demands
> exactly 32 bytes; anything minted under another encoding is refused at startup
> with a message about the key length, so the operator who followed a wrong
> example ends up looking for the defect in the wrong place. Both examples below
> mint hex, and a gate RUNS them and feeds the result to the real resolver
> (`services/iam/internal/apps/kaname/config`,
> `TestREADMEExampleMintsAValueTheResolverAccepts`) — this page cannot drift from
> the code again without going red.

> [!important] This key WRAPS THE PRIVATE HALF of the platform's token signing
> key. The name is unchanged and the meaning is not: it used to encrypt rows of a
> key store that had no reader at all, and it now wraps the private half held in
> the iam key store the platform signs its own tokens with. The name was
> deliberately NOT changed — renaming would cost an edit in every deployment
> profile and open a window in which the old name is silently ignored, so a
> profile that kept it would look configured while the process read nothing.
> Production refuses to start without it, and what stops working when it is
> absent is now token minting.

> [!warning] This key MUST SURVIVE every re-deploy, and the service now enforces
> it. Whatever is already wrapped is readable by THIS key and by no other: rolling
> it makes every stored private half unrecoverable — there is no re-wrap path, and
> a fresh signing key generated over the unreadable ones would silently void every
> token already issued. Provision it ONCE and reuse it.
>
> Since #1062 iam proves this at startup: it reads the key set first and, if the
> presented key does not open what is stored, it REFUSES TO START naming the knob
> and the keys that did not open, instead of quietly generating a replacement.
> A stand that will not come up after a secret change is telling you the previous
> value is still the only one that opens the store — restore it.
>
> Practical consequences for the two patterns below: pin the remote value (do not
> point `refreshInterval` at a rotating source), and never regenerate the secret
> as part of re-running a bootstrap script. On the local kind stand the same rule
> is held by `deploy/scripts/dev-prod-secrets.sh`, which generates the key once
> and reuses it afterwards; the discipline is gated by
> `deploy/tests/helm/secret-material-survives-recreation-test.sh`.

### Pattern A: external-secrets-operator (recommended)

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: kaname-jwks-enc-key
  namespace: kacho-system
spec:
  # Pinned, NOT a rotating window: see "Changing the wrapping key" below. A
  # source that swaps this value on a timer replaces the whole list, and a pod
  # restarted afterwards would face a store it cannot open.
  refreshInterval: 0
  secretStoreRef:
    name: vault-backend
    kind: ClusterSecretStore
  target:
    name: kaname-jwks-enc-key
    creationPolicy: Owner
  data:
    - secretKey: enc_key
      remoteRef:
        key: kacho/prod/iam/jwks-enc-key
        # The remote property holds the hex list the resolver reads: the current
        # key first, previous keys after it, separated by commas. The property
        # NAME says hex so that whoever fills the remote store cannot mint the
        # value in another encoding by following this example.
        property: aes_gcm_hex_list
```

### Pattern B: sealed-secrets (legacy)

```bash
openssl rand -hex 32 | tr -d '\n' | \
  kubeseal --raw --namespace kacho-system --name kaname-jwks-enc-key > enc_key.sealed
```

Then in `clusters/<cluster>/overrides.yaml`:
```yaml
extraSecrets:
  - name: kaname-jwks-enc-key
    sealedData:
      enc_key: <contents of enc_key.sealed>
```

### Changing the wrapping key

The knob takes a LIST, separated by commas: the FIRST key wraps every new
private half, ALL of them are tried when opening a stored one. A single value —
what every profile carries today — is a list of one, and nothing about it
changes.

So a change is an edit, not a migration. Put the new key first and KEEP the
previous one:

```
KANAME_JWKS_ENC_KEY=<new key>,<the key that is in use today>
```

Restart iam. Nothing in the store is touched, no window is needed, and every
signing key written under the previous key keeps opening. From then on each key
the store writes is wrapped under the new one, so the store migrates itself as
keys rotate on their own lifetime (`config.authn.tokenSigning.keyLifetime`)
and retired ones are swept after the removal grace.

What this does NOT do — say it out loud, because it is the reason to read on:

- **It does not WITHDRAW a key.** A leaked key keeps opening every row that was
  wrapped with it, and dropping it from the list before the store has turned
  over costs those signing keys outright. "Superseded" and "withdrawn" are
  different things, and only the first has a path today.
- **It does not tell you when the tail is safe to drop.** The store has turned
  over once every key present at the change has been retired AND swept — one
  `key-lifetime` plus the removal grace, at the earliest. Nothing measures that
  for you yet.
- **The list only grows** until someone does the above deliberately. iam prints
  how many keys were declared at every start (`private-half wrapping keys
  declared keys=N`) precisely so that growth is visible from the outside.

### If the remote source changes the value under a running deployment

It cannot affect a RUNNING pod: the value arrives through `secretKeyRef`, which
is read once at process start, so a Secret rewritten underneath is not seen
until the pod is replaced. The next restart is where it lands, and that is the
whole reason `refreshInterval` is pinned above.

At that restart the outcome depends on ONE thing — whether the new value still
names a key that opens the store:

| The new value | What iam does |
|---|---|
| current key first, previous key still listed | starts; store opens; previously issued tokens keep verifying |
| replaced the value outright, previous key dropped | cannot open the store: every private half wrapped with the dropped key is unrecoverable and every token already issued becomes unverifiable. Restore the previous key INTO THE LIST — no other key can be substituted for it |
| unchanged | starts, unchanged |

A source that rewrites this value on a schedule therefore breaks the deployment
at the next unrelated restart — hours or days after the change, with nothing
connecting the two. Pin it, and drive changes through the list above.

## Troubleshooting

| Symptom | Likely cause | Remediation |
|---|---|---|
| `Backend gRPC returns Unavailable: "authorization service unavailable"` | решение о доступе не принято (база iam недоступна / вычисление сорвалось) | `kubectl get po -n kacho-system -l app=kaname` и его Postgres. Код `UNAVAILABLE`, а не `PERMISSION_DENIED`: не решено ничего, значит тот же вызов имеет смысл повторить. `PERMISSION_DENIED` здесь означает, что модель ОТВЕТИЛА — смотреть надо на выдачи, а не на поды. |

## See also

Здесь стояли две ссылки на приёмку и шаблоны наложения правил. Механизм снят
вместе со своим потребителем (#2141), а ссылка на снятый шаблон в обратных
кавычках читается следующим как живая координата — поэтому она не
воспроизводится даже в абзаце, объясняющем её отсутствие.

Здесь стояла вторая ссылка — на проектный документ iam из каталога сторонних
артефактов под `docs/`. Каталог удалён целиком решением владельца 2026-06-11
(коммит `28778ef4`, «сторонние артефакты superpowers-скила … восстановимо из
git-истории»); адрес не воспроизводится, потому что процитированный он читается
как живой. Кому нужен тот текст — он лежит в истории по этому коммиту.

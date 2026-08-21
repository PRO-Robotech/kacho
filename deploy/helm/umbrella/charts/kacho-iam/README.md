# kacho-iam sub-chart (KAC-127 Phase 3)

Identity / Access Management control-plane service for Kachō Cloud.
Account / Project / User / ServiceAccount / Group / Role / AccessBinding +
WebAuthn/Passkey AuthN (Phase 2) + ReBAC + OPA AuthZ (Phase 3).

This sub-chart is owned by the `kacho-deploy` umbrella and is not intended
for standalone deployment. The umbrella manages cross-cutting Phase 3 concerns
(OPA sidecar shared ConfigMap, NetworkPolicies) at the parent level; this
sub-chart only declares the kacho-iam Deployment + its supporting
ConfigMap / RBAC / Service objects.

## Phase 3 additions

| Feature | Manifestation |
|---|---|
| OPA sidecar | `templates/deployment.yaml` injects container `opa` when `opaSidecar.enabled=true`. |
| Pod label `kacho.cloud/opa-sidecar=true` | Matched by umbrella NetworkPolicy `opa-sidecar-egress-allowlist`. |

> [!note] Внешний движок отношений снят вместе со своей посадкой (S6 эпика #747).
> Решение о доступе вычисляет реляционная форма в собственной базе iam, поэтому
> из чарта ушли: подчарт начальной настройки движка, выделенная база движка,
> секрет с идентификатором модели, init-контейнер ожидания движка, переменные
> `KACHO_IAM_OPENFGA_STORE_ID` / `KACHO_IAM_OPENFGA_MODEL_ID`, ключ
> `config.extapi.openfga.*` и рубильник источника вердикта
> (`config.authz.verdictFormTypes` / `config.authz.shadowCompare`) — сравнивать
> больше не с чем. Сама МОДЕЛЬ прав (`fga_model.fga`) остаётся: она источник
> истины формы и разбирается службой, а не движком.

## Bundle signing key rotation (180d schedule)

> [!warning] Этот раздел описывает НЕреализованный замысел — читать как план, не как факт.
> **Ротатора JWKS в чарте нет** — cronjob-шаблон, который его нёс, снят как
> вестигиальный, и его имя здесь намеренно не воспроизводится: путь в обратных
> кавычках читается следующим как живая координата, даже внутри абзаца,
> объясняющего, что файла нет. **Никто не наполняет** ConfigMap `kacho-iam-jwks` —
> в нём остаётся пустой placeholder-PEM, а подписи бандлов в коде iam нет ни в
> одном пакете. Раздел оставлен как описание намерения; прежде чем на него
> опираться — реализовать подпись бандлов и её собственный key-lifecycle. Весь
> текст ниже про «rotator» относится к этому нереализованному плану.
>
> **Прежняя редакция объясняла снятие тем, что iam не владеет ключом подписи
> токенов. С задачи #897 это неверно:** платформа чеканит свои токены сама, у iam
> есть ключница подписных ключей со своим сроком, ротацией и публикуемым набором
> (`config.authn.tokenSigning.*`). На этот раздел смена ничего не переносит:
> подпись БАНДЛА и подпись ТОКЕНА — разные предметы с разными адресатами и разной
> ротацией, и ключница токенов под подпись бандлов не переиспользуется.

The OPA bundle is (per the above plan) signed with JWS ES256; the public half
would live in ConfigMap `kacho-iam-jwks`, rendered by
`templates/jwks-configmap.yaml` of this chart, which OPA sidecars across the
fleet load at startup to verify each downloaded bundle. (Прежняя редакция называла
шаблон длинным именем с префиксом сервиса и объявляла его umbrella-managed — ни такого
файла, ни такого расположения в дереве нет: шаблон лежит в этом же чарте и называется
короче. Снятое имя здесь не воспроизводится: в обратных кавычках оно читается как живая
координата — именно на этом прежняя редакция абзаца сама и попалась.)

### Rotation cadence

- **180d** is the **public-key** rotation cadence (acceptance §5.6) for the
  bundle-signing key of this unimplemented plan.
- The knob that used to be named here for setting that cadence was removed: no
  line of code ever read it. It is deliberately not reproduced — a knob name in
  backticks reads as a live setting, and an operator would set it and get
  nothing. An implementation of bundle signing declares its own cadence knob.
- The lifetime of the TOKEN signing key is a different setting for a different
  key: `config.authn.tokenSigning.keyLifetime` (see `values.yaml`). Rotation of
  that key runs inside the service, not from a CronJob.

### Rotation procedure

Day 0:
1. JWKS rotator CronJob hits `rotation-days` threshold for the current key.
2. Rotator generates a new ES256 keypair, wraps the private half and stores it.
   (The store this step used to name was dropped by a migration; a real
   implementation of bundle signing brings its own. It must NOT reuse the token
   signing key store — that one holds the keys the platform mints ITS OWN tokens
   with, and a bundle-signing key sharing that store would share its rotation,
   its published key set and its revocation, none of which are about bundles.)
3. Rotator marks the new row current, demotes the old one but keeps it usable
   for verification.
4. Rotator updates ConfigMap `kacho-iam-jwks` — both old kid and new kid
   PEM entries present.
5. `kacho-iam` bundle server would sign bundles with the new key during a
   grace window. (The knobs that carried that window were removed — nothing
   read them; a real implementation brings its own.)

Day 0 + 2h (grace expiry):
6. Rotator marks old row `valid=false`. Bundle server stops accepting old kid.
7. Sidecars that have not pulled a new bundle within the grace window fail
   signature verification → fail-closed → alert `opa_bundle_signature_failures_total`.
8. Operator's runbook: force rolling restart of all kacho-* pods. New pods
   re-load the latest PEM ConfigMap and successfully pull/verify the new
   bundle.

Day 0 + 180d (public-key audit cycle):
9. Operator reviews the bundle-signing key audit log. A key older than 180d that
   has been unusable for verification for more than 7d is safe to purge — no
   in-flight verification against it is possible any more.

### Disaster: signing key compromise

If the **private** signing key leaks (e.g., dev-cluster Secret leak):

1. Trigger immediate rotation — **CronJob'а для этого больше нет** (снят как
   вестигиальный, см. предупреждение выше); при реализации bundle-signing здесь
   должен появиться его собственный механизм ротации.
2. After rotation completes, force rolling restart of every kacho-*
   pod: `kubectl rollout restart deployment -n kacho-system -l app.kubernetes.io/part-of=kacho`.
3. Compress the rotation grace window for the duration of incident response
   (accept temporary fail-closed during sidecar pull lag).
4. Invalidate ALL existing OPA bundles in CDN/cache (force re-pull) by making
   the pod template change, so sidecars detect a new revision and re-pull.
5. Audit: list the bundle-signing keys still usable for verification and
   compare their age against the incident timestamp — anything older is suspect.

## Sealed-secret integration (operator setup)

For production deployments, the 32-byte AES-GCM key
(`KACHO_IAM_JWKS_ENC_KEY`, default Secret name `kacho-iam-jwks-enc-key`) must
be provisioned BEFORE first-deploy of this chart. Two supported patterns:

> [!important] This key WRAPS THE PRIVATE HALF of the platform's token signing
> key. The name is unchanged and the meaning is not: it used to encrypt rows of a
> key store that had no reader at all, and it now wraps the private half held in
> the iam key store the platform signs its own tokens with. The name was
> deliberately NOT changed — renaming would cost an edit in every deployment
> profile and open a window in which the old name is silently ignored, so a
> profile that kept it would look configured while the process read nothing.
> Production refuses to start without it, and what stops working when it is
> absent is now token minting.

### Pattern A: external-secrets-operator (recommended)

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: kacho-iam-jwks-enc-key
  namespace: kacho-system
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: vault-backend
    kind: ClusterSecretStore
  target:
    name: kacho-iam-jwks-enc-key
    creationPolicy: Owner
  data:
    - secretKey: enc_key
      remoteRef:
        key: kacho/prod/iam/jwks-enc-key
        property: aes_gcm_b64url
```

### Pattern B: sealed-secrets (legacy)

```bash
echo -n "$(openssl rand -base64 32 | tr -d '=')" | \
  kubeseal --raw --namespace kacho-system --name kacho-iam-jwks-enc-key > enc_key.sealed
```

Then in `clusters/<cluster>/overrides.yaml`:
```yaml
extraSecrets:
  - name: kacho-iam-jwks-enc-key
    sealedData:
      enc_key: <contents of enc_key.sealed>
```

## Troubleshooting

| Symptom | Likely cause | Remediation |
|---|---|---|
| `OPA sidecar /health returns {"bundles":{"...":{"active_revision":""}}}` | First bundle pull pending | Wait ≤90s on dev / ≤65min on prod (OPA pollMinDelaySeconds). |
| `OPA sidecar logs: signature verification failed: invalid key` | Public-key ConfigMap stale | `kubectl rollout restart deployment -n kacho-system -l app.kubernetes.io/part-of=kacho`. |
| `Backend gRPC returns Unavailable: "authorization service unavailable"` | решение о доступе не принято (база iam недоступна / вычисление сорвалось) | `kubectl get po -n kacho-system -l app=kacho-iam` и его Postgres. Код `UNAVAILABLE`, а не `PERMISSION_DENIED`: не решено ничего, значит тот же вызов имеет смысл повторить. `PERMISSION_DENIED` здесь означает, что модель ОТВЕТИЛА — смотреть надо на выдачи, а не на поды. |
| `Backend gRPC returns PermissionDenied: "policy: <msg>"` | OPA deny-rule fired (expected) | Review `<msg>` against acceptance §4.6 Rego rules. |

## See also

- `docs/specs/sub-phase-3.3-iam-authz-fga-conditions-opa-acceptance.md` — full design + GWT.
- Umbrella templates (Phase 3): `helm/umbrella/templates/opa-*.yaml`.

Здесь стояла вторая ссылка — на проектный документ iam из каталога сторонних
артефактов под `docs/`. Каталог удалён целиком решением владельца 2026-06-11
(коммит `28778ef4`, «сторонние артефакты superpowers-скила … восстановимо из
git-истории»); адрес не воспроизводится, потому что процитированный он читается
как живой. Кому нужен тот текст — он лежит в истории по этому коммиту.

# Image digest pinning (INFRA sec-hardening)

The umbrella defaults reference kacho-* images by **mutable tag** (`:main-<sha>`,
`:dev`, and — in the base `values.yaml` — `:latest` for local pulls). A mutable
tag can be re-pointed after it was tested, so a node re-pull may silently deploy
different bits. For provenance / reproducibility (SLSA, CIS Docker 4.x,
CWE-1104) production rollouts should pin images by **immutable sha256 digest**.

## Mechanism

Digest pinning is a **values override** — nothing is hardcoded, so dev/CI keep
using tags and only the environments that opt in carry digests.

| Chart | Field | Behaviour when set |
|-------|-------|--------------------|
| `kacho-iam` | `kacho-iam.image.digest` | image renders as `<repository>@<digest>` (tag ignored) — helper `kacho-iam.image` |
| `kacho-geo` | `kacho-geo.imageDigest` | trailing `:tag` of `image` stripped, `@<digest>` appended — helper `kacho-geo.image` |

Empty by default → tag-based reference (unchanged behaviour, dev/CI untouched).

Sibling control-plane charts (`vpc`, `compute`, `api-gateway`, `ui`,
`kacho-nlb`) are vendored from their own repos; pin them through the reference
their chart accepts (usually `image.tag` set to a digest-qualified value) or add
an equivalent `image.digest` field in the sibling chart.

## How to pin

1. Resolve the digest of the image you deploy:

   ```bash
   docker buildx imagetools inspect docker.io/prorobotech/kacho-iam:main-<sha> \
     --format '{{.Manifest.Digest}}'
   # or
   crane digest docker.io/prorobotech/kacho-iam:main-<sha>
   ```

2. Copy `helm/umbrella/values.digests.example.yaml` to `values.digests.yaml`,
   replace every `REPLACE_WITH_REAL_DIGEST` with the real `sha256:...`.

3. Layer it **last** so it wins over the tag defaults:

   ```bash
   helm upgrade --install kacho-umbrella ./helm/umbrella -n kacho \
     -f values.prod.yaml -f values.digests.yaml
   ```

## Verify the render

```bash
helm template kacho-umbrella ./helm/umbrella \
  -f values.dev.yaml --set kacho-iam.image.digest=sha256:<64-hex> \
  --show-only charts/kacho-iam/templates/deployment.yaml | grep 'image:'
# → image: "docker.io/prorobotech/kacho-iam@sha256:<64-hex>"
```

`tests/helm/sec-hardening-test.sh` asserts the override is honoured for both
`kacho-iam` and `kacho-geo`.

> The example file ships **placeholder** digests (`sha256:REPLACE_WITH_REAL_DIGEST`)
> on purpose — it is a template, never applied as-is. No fabricated digests are
> committed.

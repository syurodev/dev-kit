# Offline YAML schemas

These are data snapshots, bundled lazily in the YAML worker. No document text
or schema reference is sent over the network at runtime.

- `compose.json`: [Compose Specification](https://github.com/compose-spec/compose-spec/blob/main/schema/compose-spec.json), snapshot 2026-10-16. Apache-2.0.
- `liquibase.json`: [SchemaStore Liquibase 3.2 schema](https://www.schemastore.org/liquibase-3.2.json), snapshot 2026-10-16. The picker names SchemaStore deliberately; this is not a promise of every current Liquibase extension. Apache-2.0.
- `kubernetes.json`: `definitions` from [Kubernetes v1.35.0 OpenAPI](https://github.com/kubernetes/kubernetes/blob/v1.35.0/api/openapi-spec/swagger.json), plus an index built from `x-kubernetes-group-version-kind`. Apache-2.0. Unknown kinds/CRDs receive syntax checks and an explicit notice, never a claim that they conform to a bundled schema.

Compose's `restart` and Kubernetes' `imagePullPolicy` completions also offer the
values named by their upstream property descriptions. These hints don't
narrow schema validation.

To refresh Kubernetes, download the version's OpenAPI document, copy its
`definitions`, and build `kinds["<group/>version|Kind"] = definitionName` from
each definition's `x-kubernetes-group-version-kind` array. Keep the picker label
and tests in sync with the chosen version. Do not mix versioned schemas
silently. Source files retain their descriptions and local references.

The loader adapts Liquibase preConditions to accept both a sequence and a mapping,
including numeric sqlCheck.expectedResult values. The upstream snapshot stays
unchanged; this compatibility layer matches the hand-authored YAML form used
by this app’s migration files.

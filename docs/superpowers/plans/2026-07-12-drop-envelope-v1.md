# Drop Envelope V1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove V1 envelope create/read paths so DevKit only supports V2 + AAD, then update PERSONAL/base docs to match.

**Architecture:** Keep API names `SealV2` / `OpenWithAD`. Delete `Seal` / `Open` (nil-AAD). `Envelope.Validate` and `OpenWithAD` accept only version 2. Services already write V2 — no call-site changes. Docs rewrite ADR-005 and related project-state pages.

**Tech Stack:** Go crypto (`internal/crypto`, `internal/vault`), MDX docs in `/Users/syuro/Workspace/PERSONAL/base`.

**Spec:** `docs/superpowers/specs/2026-07-12-drop-envelope-v1-design.md`

## File map

| File | Responsibility |
|---|---|
| `internal/crypto/envelope.go` | Version constant + Validate (V2 only) |
| `internal/crypto/aead.go` | SealV2 / OpenWithAD only |
| `internal/crypto/aead_test.go` | V2 round-trip + reject V1 |
| `internal/crypto/envelope_test.go` | Validate V2 / reject V1 & unknown |
| `internal/vault/service.go` | Drop Seal/Open; SealV2/OpenWithAD only |
| `internal/vault/service_test.go` | Round-trip via SealV2/OpenWithAD |
| `content/projects/dev-kit/architecture-decisions.mdx` | ADR-005 V2-only |
| `content/projects/dev-kit/technical-contracts.mdx` | Envelope contract V2-only |
| `content/projects/dev-kit/security-controls.mdx` | Control row without V1 compat |

Worktrees:

- Code: `/Users/syuro/Workspace/dev-kit` (branch from `main`; create `codex/drop-envelope-v1` if not on a feature branch)
- Docs: `/Users/syuro/Workspace/PERSONAL/base` (`main` is fine for docs commits)

---

### Task 1: Crypto tests — reject V1, keep V2

**Files:**
- Modify: `internal/crypto/aead_test.go`
- Modify: `internal/crypto/envelope_test.go`

- [ ] **Step 1: Replace V1-oriented aead tests with V2-only expectations**

In `internal/crypto/aead_test.go`:

1. Replace `TestSealOpenRoundTrip` with `TestSealV2OpenWithADRoundTrip` using `SealV2` + `OpenWithAD`.
2. Replace `TestOpenFailsWithWrongKey` / `TestOpenFailsWhenTampered` to use `SealV2` + `OpenWithAD`.
3. Delete `TestOpenWithADReadsV1Envelope`.
4. Delete `TestOpenRejectsV2EnvelopeWithoutAssociatedData` (no `Open` API).
5. Add `TestOpenWithADRejectsV1Envelope` that builds a handcrafted V1-shaped envelope (version=1, valid fields) and asserts `OpenWithAD` returns an error.
6. Keep `TestOpenWithADRejectsChangedRecordID` and `TestSealV2RejectsNULInAssociatedData`.

Example reject test:

```go
func TestOpenWithADRejectsV1Envelope(t *testing.T) {
	key := newTestKey(t)
	ad := AssociatedData{RecordType: "note", RecordID: "record-1", KeyID: "note-body"}
	env := Envelope{
		Version:    1,
		Algorithm:  AlgorithmXChaCha20Poly1305,
		KeyID:      ad.KeyID,
		Nonce:      make([]byte, 24),
		Ciphertext: []byte("not-a-real-ciphertext-just-for-version-gate"),
	}
	if _, err := OpenWithAD(key, env, ad); err == nil {
		t.Fatal("OpenWithAD must reject version 1 envelopes")
	}
}
```

- [ ] **Step 2: Update envelope_test.go to V2-only Validate**

- Change accept fixture `Version` from `1` to `2` (`EnvelopeVersionV2` or literal `2`).
- Add `TestEnvelopeValidateRejectsV1` with `Version: 1` expecting error.
- Keep reject-unknown-version (3) and AES reject tests, using `Version: 2` for algorithm fixtures so the failure is about algorithm not version.

- [ ] **Step 3: Run tests — expect FAIL**

```bash
cd /Users/syuro/Workspace/dev-kit
GOCACHE=/private/tmp/devkit-go-cache go test ./internal/crypto/...
```

Expected: FAIL (implementation still accepts V1 / exports Seal+Open used by old tests already rewritten).

- [ ] **Step 4: Commit failing tests only if tree allows; prefer implement next before commit if tests cannot compile**

If tests do not compile because they still reference `Seal`/`Open` after rewrite, proceed immediately to Task 2 without committing a broken tree. Otherwise:

```bash
git add internal/crypto/aead_test.go internal/crypto/envelope_test.go
git commit -m "test: expect V2-only crypto envelopes"
```

---

### Task 2: Crypto implementation — V2 only

**Files:**
- Modify: `internal/crypto/envelope.go`
- Modify: `internal/crypto/aead.go`

- [ ] **Step 1: Restrict Validate to V2**

In `envelope.go`:

```go
const (
	EnvelopeVersionV2 uint32 = 2
)

func (e Envelope) Validate() error {
	if e.Version != EnvelopeVersionV2 {
		return errors.New("envelope version is unsupported")
	}
	// ... rest unchanged
}
```

Remove `EnvelopeVersionV1` constant.

Update `AssociatedData` comment: drop “V2 envelopes” phrasing that implies V1 still exists; say “authenticated by sealed envelopes” or “SealV2 envelopes”.

- [ ] **Step 2: Remove Seal and Open; make OpenWithAD V2-only**

In `aead.go`:

- Delete `Seal` function entirely.
- Delete `Open` function entirely.
- Rewrite `OpenWithAD`:

```go
func OpenWithAD(key []byte, envelope Envelope, associatedData AssociatedData) ([]byte, error) {
	if envelope.Version != EnvelopeVersionV2 {
		return nil, errors.New("envelope version is unsupported")
	}
	if err := validateAssociatedData(associatedData); err != nil {
		return nil, err
	}
	if associatedData.KeyID != envelope.KeyID {
		return nil, errors.New("associated data key ID does not match envelope")
	}
	return open(key, envelope, canonicalAssociatedData(associatedData))
}
```

Keep `SealV2` unchanged (still sets `Version: EnvelopeVersionV2`).

- [ ] **Step 3: Run crypto tests**

```bash
GOCACHE=/private/tmp/devkit-go-cache go test ./internal/crypto/...
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/crypto/
git commit -m "feat: drop V1 crypto envelopes; V2+AAD only"
```

---

### Task 3: Vault API — drop Seal/Open

**Files:**
- Modify: `internal/vault/service.go`
- Modify: `internal/vault/service_test.go`

- [ ] **Step 1: Update vault tests first**

Replace `TestVaultSealOpenRoundTrip` and `TestVaultSealFailsWhenLocked` to use `SealV2` / `OpenWithAD`:

```go
ad := crypto.AssociatedData{RecordType: "note", RecordID: "note-1", KeyID: "note-body"}
env, err := service.SealV2(ad, []byte("secret body"))
// ...
plain, err := service.OpenWithAD(env, ad)
```

Locked case: `service.SealV2(ad, []byte("x"))` must fail with `ErrLocked`.

- [ ] **Step 2: Remove Service.Seal and Service.Open**

Delete those methods from `service.go`. Update `OpenWithAD` comment to remove V1 compatibility language.

- [ ] **Step 3: Run vault + dependent package tests**

```bash
GOCACHE=/private/tmp/devkit-go-cache go test ./internal/vault/... ./internal/services/notes/... ./internal/services/database/... ./internal/services/ssh/...
```

Expected: PASS

- [ ] **Step 4: Full package test + vet**

```bash
GOCACHE=/private/tmp/devkit-go-cache go test ./...
GOCACHE=/private/tmp/devkit-go-cache go vet ./...
```

Expected: PASS / no issues

- [ ] **Step 5: Commit**

```bash
git add internal/vault/
git commit -m "feat: remove vault Seal/Open; SealV2 only"
```

---

### Task 4: Update DevKit docs in PERSONAL/base

**Files:**
- Modify: `/Users/syuro/Workspace/PERSONAL/base/content/projects/dev-kit/architecture-decisions.mdx`
- Modify: `/Users/syuro/Workspace/PERSONAL/base/content/projects/dev-kit/technical-contracts.mdx`
- Modify: `/Users/syuro/Workspace/PERSONAL/base/content/projects/dev-kit/security-controls.mdx`
- Possibly other DevKit MDX hits from grep

- [ ] **Step 1: Grep leftover V1 claims**

```bash
cd /Users/syuro/Workspace/PERSONAL/base
rg -n -i 'EnvelopeVersionV1|V1 envelope|reads? V1|Open\(.*V1|backward.*V1|legacy V1|V1 readability|Seal\b.*V1|OpenWithAD.*V1' content/projects/dev-kit
```

- [ ] **Step 2: Rewrite ADR-005 to V2-only**

Title can stay or become “V2 envelope with AAD binding (V1 removed)”. Content must state:

- Decision: only version 2; V1 rejected
- Context: greenfield project; no migrate
- Alternatives: keep V1 read path (rejected)
- Consequences: old V1 ciphertext unreadable; new writes SealV2
- Status: Accepted

- [ ] **Step 3: Update technical-contracts Encryption envelope section**

- Valid version: `2` only
- Remove bullets about `Open(...)` reading V1 / `OpenWithAD` reading both
- State `OpenWithAD` requires V2 + matching AAD
- Evidence paths still `internal/crypto/envelope.go`, `aead.go`

- [ ] **Step 4: Update security-controls row**

Control about envelopes: V2 AAD binding only; remaining risk must not say “Legacy V1 envelopes still readable”.

- [ ] **Step 5: Doc quality gates**

```bash
bun run types:check
bun run lint
git diff --check
```

Expected: exit 0

- [ ] **Step 6: Commit docs**

```bash
git add content/projects/dev-kit
git commit -m "docs: document V2-only DevKit envelopes"
```

---

### Task 5: Final verification

- [ ] **Step 1: Confirm no V1 API left in Go**

```bash
cd /Users/syuro/Workspace/dev-kit
grep -rn 'EnvelopeVersionV1\|func Seal\|func Open\b\|OpenWithADReadsV1' --include='*.go' internal/crypto internal/vault || echo 'clean'
```

Expected: `clean` (or only intentional error-string tests using literal `Version: 1`)

- [ ] **Step 2: Confirm docs**

```bash
cd /Users/syuro/Workspace/PERSONAL/base
rg -n -i 'reads? V1|OpenWithAD.*V1|legacy V1|V1 readability' content/projects/dev-kit || echo 'docs clean'
```

- [ ] **Step 3: Re-run full Go tests**

```bash
cd /Users/syuro/Workspace/dev-kit
GOCACHE=/private/tmp/devkit-go-cache go test ./...
```

Expected: PASS

- [ ] **Step 4: Commit any leftover fix-ups** (skip empty commit)

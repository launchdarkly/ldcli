package scanner

import (
	"testing"
)

func TestGo_DirectBoolVariation(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", `package main

import ld "github.com/launchdarkly/go-server-sdk/v7"

func check(client *ld.LDClient) {
	val, _ := client.BoolVariation("my-flag", ldCtx, false)
	_ = val
}
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "my-flag", "variation", 1)
	if len(result.References) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(result.References))
	}
	ref := result.References[0]
	if ref.Method != "BoolVariation" {
		t.Errorf("expected method BoolVariation, got %s", ref.Method)
	}
	if ref.DefaultValue != "false" {
		t.Errorf("expected default false, got %s", ref.DefaultValue)
	}
}

func TestGo_CtxVariant(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", `package main

func check(client *ld.LDClient) {
	val, _ := client.BoolVariationCtx(ctx, "ctx-flag", ldCtx, true)
	_ = val
}
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "ctx-flag", "variation", 1)
	if result.References[0].DefaultValue != "true" {
		t.Errorf("expected default true, got %s", result.References[0].DefaultValue)
	}
}

func TestGo_StringVariation(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", `package main

func check(client *ld.LDClient) {
	val, _ := client.StringVariation("string-flag", ldCtx, "default-val")
	_ = val
}
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "string-flag", "variation", 1)
}

func TestGo_AllVariationMethods(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", `package main

func check(client *ld.LDClient) {
	client.BoolVariation("f1", ldCtx, false)
	client.IntVariation("f2", ldCtx, 0)
	client.Float64Variation("f3", ldCtx, 0.0)
	client.StringVariation("f4", ldCtx, "")
	client.JSONVariation("f5", ldCtx, ldvalue.Null())
	client.BoolVariationDetail("f6", ldCtx, false)
	client.StringVariationCtx(ctx, "f7", ldCtx, "")
	client.IntVariationDetailCtx(ctx, "f8", ldCtx, 0)
}
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	for i := 1; i <= 8; i++ {
		key := "f" + string(rune('0'+i))
		assertRefCount(t, result, key, "variation", 1)
	}
}

func TestGo_DogfoodWrapper(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "handler.go", `package handler

import "github.com/launchdarkly/foundation/main/dogfood"

func check() {
	val, _ := dogfood.BoolVariation2(ctx, "dogfood-flag", ldCtx, false)
	_ = val
}
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "dogfood-flag", "variation", 1)
}

func TestGo_VariableKey_NotDetected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", `package main

func check(client *ld.LDClient) {
	key := "dynamic-flag"
	val, _ := client.BoolVariation(key, ldCtx, false)
	_ = val
}
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.References) != 0 {
		t.Errorf("expected 0 refs for variable flag key, got %d", len(result.References))
	}
}

func TestGo_CommentNotDetected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", `package main

func check(client *ld.LDClient) {
	// client.BoolVariation("commented-flag", ldCtx, false)
	/* client.StringVariation("block-commented", ldCtx, "") */
}
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.References) != 0 {
		t.Errorf("expected 0 refs in comments, got %d", len(result.References))
	}
}

func TestGo_RawStringLiteral(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\n\nfunc check(client *ld.LDClient) {\n\tclient.BoolVariation(`raw-flag`, ldCtx, false)\n}\n")

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "raw-flag", "variation", 1)
}

func TestGo_MixedWithTypeScript(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", `package main

func check(client *ld.LDClient) {
	client.BoolVariation("go-flag", ldCtx, false)
}
`)
	writeFile(t, dir, "app.ts", `
const val = client.variation('ts-flag', false);
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "go-flag", "variation", 1)
	assertRefCount(t, result, "ts-flag", "variation", 1)
	if result.Stats.FilesScanned != 2 {
		t.Errorf("expected 2 files scanned, got %d", result.Stats.FilesScanned)
	}
}

// flagfnGenerated mirrors gonfalon's generated raw_flags_auto.go: a const block
// of `<Name>FlagKey = "<key>"` plus flagfn.New* registrations that bind a struct
// method name to that const. Used by the generated-accessor tests below.
const flagfnGenerated = `package dogfood

import "github.com/launchdarkly/foundation/dogfood/flagfn"

const (
	EnableFooFlagKey = "enable-foo"
	BarLimitFlagKey  = "bar-limit"
	BazModeFlagKey   = "baz-mode"
)

func init() {
	rawFlagFnsNonDogfood.EnableFoo = flagfn.NewBool(EnableFooFlagKey, false, logDogfoodErrors)
	rawFlagFnsDogfood.EnableFoo = flagfn.NewBool(EnableFooFlagKey, false, logDogfoodErrors)
	rawFlagFnsDogfood.BarLimit = flagfn.NewInt(BarLimitFlagKey, 5, logDogfoodErrors)
	rawFlagFnsDogfood.BazMode = flagfn.NewString(BazModeFlagKey, "control", logDogfoodErrors)
}
`

// TestGo_FlagfnGeneratedAccessors covers the dominant gonfalon Go pattern:
// flagfn.New* registrations define method accessors that resolve at call sites
// via get.Flags(ctx).Method() (and a stored receiver flags.Method()).
func TestGo_FlagfnGeneratedAccessors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "internal/dogfood/raw_flags_auto.go", flagfnGenerated)
	writeFile(t, dir, "internal/web/handler.go", `package web

func handle(ctx context.Context) {
	if get.Flags(ctx).EnableFoo() {
	}
	limit := get.Flags(ctx).BarLimit()
	_ = limit
	flags := get.Flags(ctx)
	if flags.EnableFoo() {
	}
	_ = flags.BazMode()
}
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Definitions are picked up from the in-tree generated file.
	assertWrapper(t, result, "EnableFoo", "enable-foo", "false")
	assertWrapper(t, result, "BarLimit", "bar-limit", "5")
	assertWrapper(t, result, "BazMode", "baz-mode", `"control"`)

	// Call sites resolve to flag keys through the accessor method names.
	assertRefCount(t, result, "enable-foo", "wrapper-call", 2)
	assertRefCount(t, result, "bar-limit", "wrapper-call", 1)
	assertRefCount(t, result, "baz-mode", "wrapper-call", 1)
}

// TestGo_FlagfnAccessors_SeparateDefinitionsDir covers scanning a sub-package
// while the generated definitions live elsewhere (the -definitions flag).
func TestGo_FlagfnAccessors_SeparateDefinitionsDir(t *testing.T) {
	defsDir := t.TempDir()
	writeFile(t, defsDir, "raw_flags_auto.go", flagfnGenerated)

	scanDir := t.TempDir()
	writeFile(t, scanDir, "handler.go", `package web

func handle(ctx context.Context) {
	if get.Flags(ctx).EnableFoo() {
	}
}
`)

	result, err := Scan(scanDir, ScanOptions{DefinitionsDir: defsDir})
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "enable-foo", "wrapper-call", 1)
}

// TestGo_FlagfnAccessor_UnknownMethod_NotResolved guards against false positives:
// a zero-arg method call whose name isn't a registered accessor is ignored.
func TestGo_FlagfnAccessor_UnknownMethod_NotResolved(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "internal/dogfood/raw_flags_auto.go", flagfnGenerated)
	writeFile(t, dir, "internal/web/handler.go", `package web

func handle(ctx context.Context) {
	_ = somethingElse.NotAFlag()
	_ = get.Flags(ctx).EnableFoo()
}
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "enable-foo", "wrapper-call", 1)
	if c := countKind(result, "wrapper-call"); c != 1 {
		t.Errorf("expected exactly 1 wrapper-call, got %d", c)
	}
}

func countKind(result *ScanResult, kind string) int {
	n := 0
	for _, ref := range result.References {
		if ref.Kind == kind {
			n++
		}
	}
	return n
}

func TestGo_SkipsVendorDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "vendor/pkg/main.go", `package pkg

func check(client *ld.LDClient) {
	client.BoolVariation("vendored-flag", ldCtx, false)
}
`)
	writeFile(t, dir, "src/main.go", `package main

func check(client *ld.LDClient) {
	client.BoolVariation("real-flag", ldCtx, false)
}
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "real-flag", "variation", 1)
	assertRefCount(t, result, "vendored-flag", "variation", 0)
}

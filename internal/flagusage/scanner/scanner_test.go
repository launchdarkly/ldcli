package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDirectVariationCall(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app.ts", `
import { LDClient } from '@launchdarkly/js-client-sdk';
const client: LDClient = getClient();
const val = client.variation('my-flag', false);
const detail = client.boolVariation('other-flag', true);
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "my-flag", "variation", 1)
	assertRefCount(t, result, "other-flag", "variation", 1)
}

func TestDirectVariationWithDynamicKey_NotDetected(t *testing.T) {
	// Keys that aren't a static string literal (function param, runtime call)
	// can't be resolved and must be skipped — only genuinely dynamic keys.
	dir := t.TempDir()
	writeFile(t, dir, "app.ts", `
function check(flagKey: string) {
  return client.variation(flagKey, false);
}
const dynamic = getName();
client.variation(dynamic, false);
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.References) != 0 {
		t.Errorf("expected 0 refs for dynamic flag key, got %d", len(result.References))
	}
}

func TestDirectVariationWithConstLiteral_Resolved(t *testing.T) {
	// A flag key referenced through a string constant should resolve.
	dir := t.TempDir()
	writeFile(t, dir, "app.ts", `
const MY_FLAG = 'my-flag';
const val = client.variation(MY_FLAG, false);
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "my-flag", "variation", 1)
}

func TestEnumFlagKey_Resolved(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app.ts", `
enum Flags { ShowBanner = 'show-banner' }
const val = client.boolVariation(Flags.ShowBanner, false);
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "show-banner", "variation", 1)
}

func TestWrapperDefinition(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "flags.ts", `
import { createFlagFunction } from './createFlagFunction';
export const enableFoo = createFlagFunction('enable-foo', false);
export const barLimit = createFlagFunction('bar-limit', 10);
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Wrappers) != 2 {
		t.Fatalf("expected 2 wrappers, got %d", len(result.Wrappers))
	}
	assertWrapper(t, result, "enableFoo", "enable-foo", "false")
	assertWrapper(t, result, "barLimit", "bar-limit", "10")
}

func TestWrapperCallSite_WithDefinitions(t *testing.T) {
	dir := t.TempDir()

	// Definitions in a "package" subdirectory
	writeFile(t, dir, "pkg/flags.ts", `
import { createFlagFunction } from './createFlagFunction';
export const enableFoo = createFlagFunction('enable-foo', false);
export const getBarLimit = createFlagFunction('bar-limit', 10);
`)

	// Usage file imports from the package
	writeFile(t, dir, "app.ts", `
import { enableFoo, getBarLimit } from '@myapp/flags';
if (enableFoo()) {
  console.log('foo enabled, limit:', getBarLimit());
}
`)

	result, err := Scan(dir, ScanOptions{
		WrapperModules: []string{"@myapp/flags"},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "enable-foo", "wrapper-call", 1)
	assertRefCount(t, result, "bar-limit", "wrapper-call", 1)
}

func TestWrapperCallSite_WithoutDefinitions(t *testing.T) {
	// When scanning a directory that doesn't contain the definitions file,
	// wrapper calls are marked unresolved (export name is only a guess, not a real flag key).
	dir := t.TempDir()
	writeFile(t, dir, "app.ts", `
import { enableFoo } from '@myapp/flags';
if (enableFoo()) {
  console.log('foo enabled');
}
`)

	result, err := Scan(dir, ScanOptions{
		WrapperModules: []string{"@myapp/flags"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Without definitions, flag key = export name
	assertRefCount(t, result, "enableFoo", "wrapper-call-unresolved", 1)
}

func TestWrapperCallSite_WithSeparateDefinitionsDir(t *testing.T) {
	// Definitions and usage are in separate directories.
	// We scan both by passing a DefinitionsDir.
	dir := t.TempDir()

	writeFile(t, dir, "packages/flags/flags.ts", `
import { createFlagFunction } from './createFlagFunction';
export const enableFoo = createFlagFunction('enable-foo', false);
`)

	writeFile(t, dir, "app/component.ts", `
import { enableFoo } from '@myapp/flags';
if (enableFoo()) {
  doStuff();
}
`)

	result, err := Scan(filepath.Join(dir, "app"), ScanOptions{
		WrapperModules: []string{"@myapp/flags"},
		DefinitionsDir: filepath.Join(dir, "packages/flags"),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should resolve enableFoo -> enable-foo via the definitions
	assertRefCount(t, result, "enable-foo", "wrapper-call", 1)
}

func TestWrapperCallSite_AutoResolvedViaDefinitionsDir(t *testing.T) {
	// No -wrapper-modules: resolution happens purely by matching the imported
	// name against definitions loaded from -definitions (export-name discovery).
	dir := t.TempDir()

	writeFile(t, dir, "packages/flags/flags.ts", `
import { createFlagFunction } from './createFlagFunction';
export const enableFoo = createFlagFunction('enable-foo', false);
`)
	writeFile(t, dir, "app/component.ts", `
import { enableFoo } from '@myapp/flags';
if (enableFoo()) { doStuff(); }
`)

	result, err := Scan(filepath.Join(dir, "app"), ScanOptions{
		DefinitionsDir: filepath.Join(dir, "packages/flags"),
		// deliberately NO WrapperModules
	})
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "enable-foo", "wrapper-call", 1)
}

func TestWrapperCallSite_AutoResolvedInTree(t *testing.T) {
	// Whole-tree scan with NO options at all: definitions discovered in-tree
	// auto-resolve the call sites by export name.
	dir := t.TempDir()
	writeFile(t, dir, "flags.ts", `
import { createFlagFunction } from './createFlagFunction';
export const enableFoo = createFlagFunction('enable-foo', false);
`)
	writeFile(t, dir, "app.ts", `
import { enableFoo } from '@myapp/flags';
if (enableFoo()) { doStuff(); }
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "enable-foo", "wrapper-call", 1)
}

func TestWrapperCallSite_AutoDiscoverViaNodeModules(t *testing.T) {
	// Zero-config: scanner resolves import module via node_modules symlink,
	// scans the package's source dir for definitions, and resolves call sites.
	dir := t.TempDir()

	// Simulate a monorepo: packages/flags has definitions, app imports them,
	// and node_modules/@myapp/flags symlinks to packages/flags.
	writeFile(t, dir, "packages/flags/package.json", `{
  "name": "@myapp/flags",
  "exports": { ".": "./src/flags.ts" }
}`)
	writeFile(t, dir, "packages/flags/src/flags.ts", `
import { createFlagFunction } from './createFlagFunction';
export const enableFoo = createFlagFunction('enable-foo', false);
export const barLimit = createFlagFunction('bar-limit', 10);
`)

	// Create symlink: app/node_modules/@myapp/flags -> ../../packages/flags
	nmDir := filepath.Join(dir, "app", "node_modules", "@myapp")
	if err := os.MkdirAll(nmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "packages", "flags"), filepath.Join(nmDir, "flags")); err != nil {
		t.Fatal(err)
	}

	writeFile(t, dir, "app/src/component.ts", `
import { enableFoo, barLimit } from '@myapp/flags';
if (enableFoo()) {
  console.log(barLimit());
}
`)

	result, err := Scan(filepath.Join(dir, "app", "src"))
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "enable-foo", "wrapper-call", 1)
	assertRefCount(t, result, "bar-limit", "wrapper-call", 1)
}

// TestWrapperCallSite_WrongDefinitionsDir_DoesNotSuppressAutoDiscovery is a
// regression guard: passing a -definitions dir that contains no matching wrapper
// definitions (a common agent mistake — e.g. pointing at the Go flagfn dir for a
// TS scan) must NOT suppress node_modules auto-discovery. The wrong dir should
// contribute nothing and the wrappers should still resolve.
func TestWrapperCallSite_WrongDefinitionsDir_DoesNotSuppressAutoDiscovery(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, dir, "packages/flags/package.json", `{
  "name": "@myapp/flags",
  "exports": { ".": "./src/flags.ts" }
}`)
	writeFile(t, dir, "packages/flags/src/flags.ts", `
import { createFlagFunction } from './createFlagFunction';
export const enableFoo = createFlagFunction('enable-foo', false);
`)
	nmDir := filepath.Join(dir, "app", "node_modules", "@myapp")
	if err := os.MkdirAll(nmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "packages", "flags"), filepath.Join(nmDir, "flags")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "app/src/component.ts", `
import { enableFoo } from '@myapp/flags';
if (enableFoo()) { doStuff(); }
`)

	// A non-empty definitions dir with NOTHING relevant in it (the agent's mistake).
	writeFile(t, dir, "wrong-defs/unrelated.go", `package x

func main() {}
`)

	result, err := Scan(filepath.Join(dir, "app", "src"), ScanOptions{
		DefinitionsDir: filepath.Join(dir, "wrong-defs"),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Resolves via node_modules auto-discovery despite the wrong -definitions.
	assertRefCount(t, result, "enable-foo", "wrapper-call", 1)
}

func TestWrapperCallSite_BarrelFileDefinitionsDir(t *testing.T) {
	// Regression: gonfalon shape — barrel file in a separate definitions dir
	// with many exports, multiple consuming files across subdirs, no -wrapper-modules.
	dir := t.TempDir()

	writeFile(t, dir, "packages/flags/flags.ts", `
import { createFlagFunction } from './createFlagFunction';
export const enableAlpha = createFlagFunction('enable-alpha', false);
export const betaLimit = createFlagFunction('beta-limit', 10);
export const gammaMode = createFlagFunction<'on' | 'off'>('gamma-mode', 'off');
`)

	writeFile(t, dir, "app/src/components/Alpha.tsx", `
import { enableAlpha } from '@myapp/flags';
export function Alpha() {
  const enabled = enableAlpha();
  return enabled ? <div>Alpha</div> : null;
}
`)
	writeFile(t, dir, "app/src/hooks/useBeta.ts", `
import { betaLimit } from '@myapp/flags';
export function useBeta() {
  return betaLimit();
}
`)
	writeFile(t, dir, "app/src/pages/Gamma.tsx", `
import { gammaMode, enableAlpha } from '@myapp/flags';
export function Gamma() {
  const mode = gammaMode();
  const alpha = enableAlpha();
  return <div>{mode}{alpha}</div>;
}
`)

	result, err := Scan(filepath.Join(dir, "app"), ScanOptions{
		DefinitionsDir: filepath.Join(dir, "packages/flags"),
	})
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "enable-alpha", "wrapper-call", 2)
	assertRefCount(t, result, "beta-limit", "wrapper-call", 1)
	assertRefCount(t, result, "gamma-mode", "wrapper-call", 1)

	if result.Stats.UniqueFlags != 3 {
		t.Errorf("expected 3 unique flags, got %d", result.Stats.UniqueFlags)
	}
}

func TestAliasedImport(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, dir, "pkg/flags.ts", `
import { createFlagFunction } from './createFlagFunction';
export const enableFoo = createFlagFunction('enable-foo', false);
`)

	writeFile(t, dir, "app.ts", `
import { enableFoo as isFooEnabled } from '@myapp/flags';
const x = isFooEnabled();
`)

	result, err := Scan(dir, ScanOptions{
		WrapperModules: []string{"@myapp/flags"},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "enable-foo", "wrapper-call", 1)
}

func TestFunctionWrapperDefinition_ArrowBody(t *testing.T) {
	// A thin function wrapper (not the createFlagFunction factory) should be
	// detected generically, and its remote call sites resolved.
	dir := t.TempDir()
	writeFile(t, dir, "flags.ts", `
import { client } from './client';
export const isFooEnabled = () => client.boolVariation('foo-flag', false);
`)
	writeFile(t, dir, "app.ts", `
import { isFooEnabled } from '@myapp/flags';
if (isFooEnabled()) { go(); }
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	assertWrapper(t, result, "isFooEnabled", "foo-flag", "false")
	assertRefCount(t, result, "foo-flag", "wrapper-call", 1)
}

func TestFunctionWrapperDefinition_ReturnBody(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "flags.ts", `
export function isFooEnabled() {
  return client.boolVariation('foo-flag', false);
}
`)
	writeFile(t, dir, "app.ts", `
import { isFooEnabled } from '@myapp/flags';
isFooEnabled();
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "foo-flag", "wrapper-call", 1)
}

func TestComponentUsingFlag_NotAWrapper(t *testing.T) {
	// A component that consumes a flag internally and returns something else
	// must NOT be registered as a flag wrapper.
	dir := t.TempDir()
	writeFile(t, dir, "flags.ts", `
export function Banner() {
  const show = client.boolVariation('show-banner', false);
  return show;
}
`)
	writeFile(t, dir, "app.ts", `
import { Banner } from '@myapp/flags';
Banner();
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	// show-banner is still found as a direct call, but Banner() is not a wrapper.
	assertRefCount(t, result, "show-banner", "variation", 1)
	assertRefCount(t, result, "show-banner", "wrapper-call", 0)
	assertRefCount(t, result, "show-banner", "wrapper-call-unresolved", 0)
}

func TestReactHook(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "component.tsx", `
import { useBoolVariation } from '@launchdarkly/react-client-sdk';
function MyComponent() {
  const show = useBoolVariation('show-banner', false);
  return show ? <Banner /> : null;
}
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "show-banner", "hook", 1)
}

func TestCommentNotDetected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app.ts", `
// client.variation('commented-out-flag', false);
/* client.boolVariation('block-commented', true); */
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.References) != 0 {
		t.Errorf("expected 0 refs in comments, got %d", len(result.References))
	}
}

func TestMultipleCallSitesSameFlag(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.ts", `
import { enableFoo } from '@myapp/flags';
enableFoo();
`)
	writeFile(t, dir, "b.ts", `
import { enableFoo } from '@myapp/flags';
if (enableFoo()) { doStuff(); }
`)

	result, err := Scan(dir, ScanOptions{
		WrapperModules: []string{"@myapp/flags"},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "enableFoo", "wrapper-call-unresolved", 2)
}

func TestAllVariationMethods(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app.ts", `
const a = client.variation('f1', false);
const b = client.boolVariation('f2', false);
const c = client.stringVariation('f3', 'default');
const d = client.intVariation('f4', 0);
const e = client.numberVariation('f5', 0);
const f = client.jsonVariation('f6', {});
const g = client.variationDetail('f7', false);
const h = client.boolVariationDetail('f8', true);
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

func TestAllReactHooks(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "component.tsx", `
const a = useVariation('h1', false);
const b = useBoolVariation('h2', false);
const c = useStringVariation('h3', '');
const d = useNumberVariation('h4', 0);
const e = useJsonVariation('h5', {});
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	for i := 1; i <= 5; i++ {
		key := "h" + string(rune('0'+i))
		assertRefCount(t, result, key, "hook", 1)
	}
}

func TestTemplateLiteralFlagKey(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app.ts", "const v = client.variation(`template-key`, false);\n")

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "template-key", "variation", 1)
}

func TestTemplateLiteralWithInterpolation_NotDetected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app.ts", "const v = client.variation(`${prefix}-flag`, false);\n")

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.References) != 0 {
		t.Errorf("expected 0 refs for interpolated template, got %d", len(result.References))
	}
}

func TestWrapperNotCalledAsFunction_NotDetected(t *testing.T) {
	// Passing a wrapper as a callback without calling it should not count
	dir := t.TempDir()
	writeFile(t, dir, "pkg/flags.ts", `
import { createFlagFunction } from './createFlagFunction';
export const enableFoo = createFlagFunction('enable-foo', false);
`)
	writeFile(t, dir, "app.ts", `
import { enableFoo } from '@myapp/flags';
const ref = enableFoo;
someArray.filter(enableFoo);
`)

	result, err := Scan(dir, ScanOptions{
		WrapperModules: []string{"@myapp/flags"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should not have wrapper-call refs — enableFoo is referenced but not called
	assertRefCount(t, result, "enable-foo", "wrapper-call", 0)
}

func TestMultipleWrapperModules(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pkg-a/flags.ts", `
import { createFlagFunction } from './createFlagFunction';
export const flagA = createFlagFunction('flag-a', false);
`)
	writeFile(t, dir, "pkg-b/flags.ts", `
import { createFlagFunction } from './createFlagFunction';
export const flagB = createFlagFunction('flag-b', false);
`)
	writeFile(t, dir, "app.ts", `
import { flagA } from '@pkg/a';
import { flagB } from '@pkg/b';
flagA();
flagB();
`)

	result, err := Scan(dir, ScanOptions{
		WrapperModules: []string{"@pkg/a", "@pkg/b"},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "flag-a", "wrapper-call", 1)
	assertRefCount(t, result, "flag-b", "wrapper-call", 1)
}

func TestNestedCallExpression(t *testing.T) {
	// Flag call nested inside another expression
	dir := t.TempDir()
	writeFile(t, dir, "app.ts", `
const result = someFunction(client.boolVariation('nested-flag', false));
const ternary = client.variation('ternary-flag', false) ? 'a' : 'b';
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "nested-flag", "variation", 1)
	assertRefCount(t, result, "ternary-flag", "variation", 1)
}

func TestDefaultValueCapture(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app.ts", `
const a = client.variation('bool-flag', false);
const b = client.variation('string-flag', 'hello');
const c = client.variation('number-flag', 42);
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	defaults := map[string]string{
		"bool-flag":   "false",
		"string-flag": "'hello'",
		"number-flag": "42",
	}
	for _, ref := range result.References {
		if expected, ok := defaults[ref.FlagKey]; ok {
			if ref.DefaultValue != expected {
				t.Errorf("flag %q: expected default %q, got %q", ref.FlagKey, expected, ref.DefaultValue)
			}
		}
	}
}

func TestSkipsNodeModules(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "node_modules/pkg/index.ts", `
const v = client.variation('should-skip', false);
`)
	writeFile(t, dir, "src/app.ts", `
const v = client.variation('should-find', false);
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "should-find", "variation", 1)
	assertRefCount(t, result, "should-skip", "variation", 0)
}

func TestJSXFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "component.tsx", `
import { useBoolVariation } from '@launchdarkly/react-client-sdk';
export function Banner() {
  const show = useBoolVariation('show-banner', false);
  return show ? <div className="banner">Hello</div> : null;
}
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	assertRefCount(t, result, "show-banner", "hook", 1)
}

func TestSurroundingCodeCaptured(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app.ts", `
function check() {
  const val = client.variation('my-flag', false);
  return val;
}
`)

	result, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.References) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(result.References))
	}
	ref := result.References[0]
	if ref.SurroundingCode == "" {
		t.Error("expected surrounding code to be captured")
	}
	if ref.Line != 3 {
		t.Errorf("expected line 3, got %d", ref.Line)
	}
}

// --- helpers ---

func assertRefCount(t *testing.T, result *ScanResult, flagKey, kind string, expected int) {
	t.Helper()
	count := 0
	for _, ref := range result.References {
		if ref.FlagKey == flagKey && ref.Kind == kind {
			count++
		}
	}
	if count != expected {
		t.Errorf("expected %d %s refs for %q, got %d", expected, kind, flagKey, count)
	}
}

func assertWrapper(t *testing.T, result *ScanResult, exportName, flagKey, defaultVal string) {
	t.Helper()
	for _, w := range result.Wrappers {
		if w.ExportName == exportName {
			if w.FlagKey != flagKey {
				t.Errorf("wrapper %s: expected flagKey %q, got %q", exportName, flagKey, w.FlagKey)
			}
			if w.DefaultValue != defaultVal {
				t.Errorf("wrapper %s: expected default %q, got %q", exportName, defaultVal, w.DefaultValue)
			}
			return
		}
	}
	t.Errorf("wrapper %q not found", exportName)
}

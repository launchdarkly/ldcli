package scanner

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// This is a characterization ("golden") test: it locks in the exact set of
// references the scanner emits today across every detection kind for both
// languages, so the shared-traversal refactor can be proven behavior-preserving.
// It is intentionally broad rather than deep — the per-pattern edge cases live in
// scanner_test.go / scanner_go_test.go; this one guards the *aggregate* output of
// every walk closure at once.

// refProjection is the stable, location-normalized shape we assert on. Column and
// SurroundingCode are omitted (cosmetic / whitespace-sensitive); everything that
// identifies a detection is kept.
type refProjection struct {
	File          string
	Line          int
	Kind          string
	FlagKey       string
	Method        string
	DefaultValue  string
	WrapperName   string
	VariationType string
}

func projectRefs(t *testing.T, dir string, refs []FlagReference) []refProjection {
	t.Helper()
	out := make([]refProjection, 0, len(refs))
	for _, r := range refs {
		file := strings.TrimPrefix(r.FilePath, dir+"/")
		out = append(out, refProjection{
			File:          file,
			Line:          r.Line,
			Kind:          r.Kind,
			FlagKey:       r.FlagKey,
			Method:        r.Method,
			DefaultValue:  r.DefaultValue,
			WrapperName:   r.WrapperName,
			VariationType: r.VariationType,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.FlagKey < b.FlagKey
	})
	return out
}

func TestScannerCharacterization(t *testing.T) {
	dir := t.TempDir()

	// --- TypeScript: direct calls, hook, const, enum, detail ---
	writeFile(t, dir, "ts_app.ts", `
import { useBoolVariation } from 'launchdarkly-react-client-sdk';
const client = getClient();
const a = client.boolVariation('ts-direct', false);
const b = useBoolVariation('ts-hook', true);
const K = 'ts-const-key';
const c = client.variation(K, 0);
enum Flags { Banner = 'ts-enum-key' }
const d = client.stringVariation(Flags.Banner, '');
const e = client.boolVariationDetail('ts-detail', false);
`)

	// --- TypeScript: wrapper factory defs + a thin wrapper ---
	writeFile(t, dir, "ts_flags.ts", `
import { createFlagFunction } from './createFlagFunction';
export const enableFoo = createFlagFunction('ts-wrapper-foo', false);
export const barLimit = createFlagFunction('ts-wrapper-bar', 10);
export const showBanner = () => client.boolVariation('ts-thin', false);
`)

	// --- TypeScript: wrapper call sites (resolved + unresolved) ---
	writeFile(t, dir, "ts_use.ts", `
import { enableFoo, showBanner } from './flags';
import { mysteryFlag } from '@acme/flags';
function render() {
  if (enableFoo()) return null;
  if (showBanner()) return null;
  return mysteryFlag();
}
`)

	// --- Go: flagfn wrapper registrations ---
	writeFile(t, dir, "go_flags.go", `package flags

const EnableBarFlagKey = "go-wrapper-bar"

func register(rawFlags *RawFlags) {
	rawFlags.EnableBar = flagfn.NewBool(EnableBarFlagKey, false)
	rawFlags.MaxItems = flagfn.NewInt("go-wrapper-max", 5)
}
`)

	// --- Go: direct calls, Ctx variant, dogfood, const, wrapper calls ---
	writeFile(t, dir, "go_app.go", `package app

const goKey = "go-const-key"

func check(client *ld.LDClient, ctx context.Context, ldCtx ldcontext.Context) {
	client.BoolVariation("go-direct", ldCtx, false)
	client.BoolVariationCtx(ctx, "go-ctx", ldCtx, false)
	dogfood.BoolVariation2(ctx, "go-dogfood", ldCtx, false)
	client.IntVariation(goKey, ldCtx, 0)
	_ = get.Flags(ctx).EnableBar()
	_ = get.Flags(ctx).MaxItems()
}
`)

	result, err := Scan(dir, ScanOptions{WrapperModules: []string{"@acme/flags"}})
	if err != nil {
		t.Fatal(err)
	}

	got := projectRefs(t, dir, result.References)

	want := []refProjection{
		{File: "go_app.go", Line: 6, Kind: "variation", FlagKey: "go-direct", Method: "BoolVariation", DefaultValue: "false"},
		{File: "go_app.go", Line: 7, Kind: "variation", FlagKey: "go-ctx", Method: "BoolVariationCtx", DefaultValue: "false"},
		{File: "go_app.go", Line: 8, Kind: "variation", FlagKey: "go-dogfood", Method: "BoolVariation2", DefaultValue: "false"},
		{File: "go_app.go", Line: 9, Kind: "variation", FlagKey: "go-const-key", Method: "IntVariation", DefaultValue: "0"},
		{File: "go_app.go", Line: 10, Kind: "wrapper-call", FlagKey: "go-wrapper-bar", Method: "EnableBar()", WrapperName: "EnableBar"},
		{File: "go_app.go", Line: 11, Kind: "wrapper-call", FlagKey: "go-wrapper-max", Method: "MaxItems()", WrapperName: "MaxItems"},
		{File: "go_flags.go", Line: 6, Kind: "wrapper-definition", FlagKey: "go-wrapper-bar", Method: "NewBool", DefaultValue: "false", WrapperName: "EnableBar", VariationType: "bool"},
		{File: "go_flags.go", Line: 7, Kind: "wrapper-definition", FlagKey: "go-wrapper-max", Method: "NewInt", DefaultValue: "5", WrapperName: "MaxItems", VariationType: "int"},
		{File: "ts_app.ts", Line: 4, Kind: "variation", FlagKey: "ts-direct", Method: "boolVariation", DefaultValue: "false"},
		{File: "ts_app.ts", Line: 5, Kind: "hook", FlagKey: "ts-hook", Method: "useBoolVariation", DefaultValue: "true"},
		{File: "ts_app.ts", Line: 7, Kind: "variation", FlagKey: "ts-const-key", Method: "variation", DefaultValue: "0"},
		{File: "ts_app.ts", Line: 9, Kind: "variation", FlagKey: "ts-enum-key", Method: "stringVariation", DefaultValue: "''"},
		{File: "ts_app.ts", Line: 10, Kind: "variation", FlagKey: "ts-detail", Method: "boolVariationDetail", DefaultValue: "false"},
		{File: "ts_flags.ts", Line: 3, Kind: "wrapper-definition", FlagKey: "ts-wrapper-foo", Method: "createFlagFunction", DefaultValue: "false", WrapperName: "enableFoo"},
		{File: "ts_flags.ts", Line: 4, Kind: "wrapper-definition", FlagKey: "ts-wrapper-bar", Method: "createFlagFunction", DefaultValue: "10", WrapperName: "barLimit"},
		{File: "ts_flags.ts", Line: 5, Kind: "variation", FlagKey: "ts-thin", Method: "boolVariation", DefaultValue: "false"},
		{File: "ts_flags.ts", Line: 5, Kind: "wrapper-definition", FlagKey: "ts-thin", Method: "boolVariation", DefaultValue: "false", WrapperName: "showBanner"},
		{File: "ts_use.ts", Line: 5, Kind: "wrapper-call", FlagKey: "ts-wrapper-foo", Method: "enableFoo()", WrapperName: "enableFoo"},
		{File: "ts_use.ts", Line: 6, Kind: "wrapper-call", FlagKey: "ts-thin", Method: "showBanner()", WrapperName: "showBanner"},
		// ts_use.ts:7 `mysteryFlag()` is intentionally absent: once ANY definition is
		// in scope the map is non-empty, so an unknown accessor is dropped rather than
		// emitted as wrapper-call-unresolved. The isolated test below covers the
		// emit-when-map-empty branch.
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("scanner output changed.\n--- got (%d refs) ---\n%s\n--- want (%d refs) ---\n%s",
			len(got), dumpProjections(got), len(want), dumpProjections(want))
	}
}

// TestScannerCharacterization_UnresolvedWrapperCall locks the other branch of
// findWrapperCalls: when no definitions are in scope at all, a call to an
// accessor imported from a declared wrapper module is emitted as
// wrapper-call-unresolved (flag key = raw export name).
func TestScannerCharacterization_UnresolvedWrapperCall(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "u.ts", `
import { mysteryFlag } from '@acme/flags';
export function render() { return mysteryFlag(); }
`)

	result, err := Scan(dir, ScanOptions{WrapperModules: []string{"@acme/flags"}})
	if err != nil {
		t.Fatal(err)
	}

	got := projectRefs(t, dir, result.References)
	want := []refProjection{
		{File: "u.ts", Line: 3, Kind: "wrapper-call-unresolved", FlagKey: "mysteryFlag", Method: "mysteryFlag()", WrapperName: "mysteryFlag"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scanner output changed.\n--- got (%d refs) ---\n%s\n--- want (%d refs) ---\n%s",
			len(got), dumpProjections(got), len(want), dumpProjections(want))
	}
}

// dumpProjections renders projections as copy-pasteable Go literals, so a
// legitimate behavior change (or the first run) is easy to re-baseline.
func dumpProjections(ps []refProjection) string {
	var b strings.Builder
	for _, p := range ps {
		b.WriteString(fmt.Sprintf("\t\t{File: %q, Line: %d, Kind: %q, FlagKey: %q, Method: %q, DefaultValue: %q, WrapperName: %q, VariationType: %q},\n",
			p.File, p.Line, p.Kind, p.FlagKey, p.Method, p.DefaultValue, p.WrapperName, p.VariationType))
	}
	return b.String()
}

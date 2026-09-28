package r8index

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleMapping = `# compiler: R8
com.example.app.UserService -> a.b.c:
    1:3:com.example.app.User loadUser(java.lang.String):40:42 -> a
    5:5:void validate():60:60 -> b
    void helper() -> d
com.example.app.MainActivity -> a.b.d:
    10:10:void onCreate(android.os.Bundle):100:100 -> a
    20:20:void handleClick():5:5 -> b
    20:20:void com.example.app.Analytics.track():15:15 -> b
`

// Verbatim R8 v2.2 output (e2e/android release build) for the class a Kotlin lambda
// desugars into. Three details this locks in: the class-level synthesized marker, R8
// repeating the marker only on the *first* occurrence of a synthetic member (so 52:64
// arrives unmarked), and the residual "g5." package R8 writes on the synthetic
// class's own member lines.
const syntheticLambdaMapping = `com.example.androidobservability.MainActivityKt$$ExternalSyntheticLambda10 -> g5.g:
# {"id":"sourceFile","fileName":"R8$$SyntheticClass"}
# {"id":"com.android.tools.r8.synthesized"}
    int g5.MainActivityKt$$ExternalSyntheticLambda10.$r8$classId -> g
      # {"id":"com.android.tools.r8.synthesized"}
    1:1:void g5.MainActivityKt$$ExternalSyntheticLambda10.<init>(g5.MainActivityViewModel,int):0:0 -> <init>
      # {"id":"com.android.tools.r8.synthesized"}
    47:49:java.lang.Object g5.MainActivityKt$$ExternalSyntheticLambda10.invoke():0 -> invoke
      # {"id":"com.android.tools.r8.synthesized"}
    52:64:int com.example.androidobservability.CartPricing.computeTotal(java.lang.String):19:19 -> invoke
    52:64:int com.example.androidobservability.CartPricing.priceOrder(java.lang.String):16 -> invoke
    52:64:int com.example.androidobservability.CheckoutDemo.startCheckout(java.lang.String):12 -> invoke
    52:64:kotlin.Unit com.example.androidobservability.MainActivityKt.ErrorButtons$lambda$59$lambda$58(com.example.androidobservability.MainActivityViewModel):452 -> invoke
    52:64:java.lang.Object g5.MainActivityKt$$ExternalSyntheticLambda10.invoke():0 -> invoke
`

func parsedSampleMapping(t *testing.T) *Mapping {
	t.Helper()
	m := Parse([]byte(sampleMapping))
	require.NotNil(t, m)
	require.Equal(t, 2, m.Classes())
	return m
}

func TestParseClasses(t *testing.T) {
	m := parsedSampleMapping(t)
	assert.Contains(t, m.classes, "a.b.c")
	assert.Contains(t, m.classes, "a.b.d")
	assert.Equal(t, "com.example.app.UserService", m.classes["a.b.c"].originalName)
	assert.Equal(t, "com.example.app.MainActivity", m.classes["a.b.d"].originalName)
}

// A file that is not a mapping parses into nothing rather than failing, which is
// what a caller checks before uploading or symbolicating.
func TestParseRejectsNonMapping(t *testing.T) {
	for _, in := range []string{"", "not a mapping", "# compiler: R8\n", "\n\n"} {
		m := Parse([]byte(in))
		require.NotNil(t, m)
		assert.Zero(t, m.Classes(), "%q", in)
	}
}

func TestRetraceLinearRange(t *testing.T) {
	m := parsedSampleMapping(t)
	// obf line 2 in range 1:3 -> original 40 + (2-1) = 41.
	frames := m.Retrace("a.b.c", "a", 2)
	assert.Len(t, frames, 1)
	assert.Equal(t, "com.example.app.UserService", frames[0].Class)
	assert.Equal(t, "loadUser", frames[0].Method)
	assert.Equal(t, 41, frames[0].Line)
}

func TestRetraceCollapsedRange(t *testing.T) {
	m := parsedSampleMapping(t)
	// 5:5 -> 60:60 maps to the single original line regardless of input.
	frames := m.Retrace("a.b.c", "b", 5)
	assert.Len(t, frames, 1)
	assert.Equal(t, "validate", frames[0].Method)
	assert.Equal(t, 60, frames[0].Line)
}

func TestRetraceInlineExpansion(t *testing.T) {
	m := parsedSampleMapping(t)
	// obf a.b.d.b at line 20 has two entries in the same range: the enclosing
	// handleClick and the inlined Analytics.track.
	frames := m.Retrace("a.b.d", "b", 20)
	assert.Len(t, frames, 2)
	assert.Equal(t, "com.example.app.MainActivity", frames[0].Class)
	assert.Equal(t, "handleClick", frames[0].Method)
	assert.Equal(t, 5, frames[0].Line)
	// Inlined frame keeps its own (original) class.
	assert.Equal(t, "com.example.app.Analytics", frames[1].Class)
	assert.Equal(t, "track", frames[1].Method)
	assert.Equal(t, 15, frames[1].Line)
}

func TestRetraceUnknownClassAndMethod(t *testing.T) {
	m := parsedSampleMapping(t)
	// Unknown class -> nil (frame passes through unchanged).
	assert.Nil(t, m.Retrace("x.y.z", "a", 1))
	// Known class, method not in mapping -> class deobfuscated, method kept.
	frames := m.Retrace("a.b.c", "zzz", 1)
	assert.Len(t, frames, 1)
	assert.Equal(t, "com.example.app.UserService", frames[0].Class)
	assert.Equal(t, "zzz", frames[0].Method)
}

func TestRetraceNameOnlyFallback(t *testing.T) {
	m := parsedSampleMapping(t)
	// "d" only has a name mapping (helper, no line range); line is preserved.
	frames := m.Retrace("a.b.c", "d", 99)
	assert.Len(t, frames, 1)
	assert.Equal(t, "helper", frames[0].Method)
	assert.Equal(t, 99, frames[0].Line)
}

// Real R8 v2.2 output (from the e2e/android release build): the whole CheckoutDemo
// chain was inlined into a synthetic lambda's `invoke`, so a single runtime frame
// "g5.g.invoke(SourceFile:57)" must expand to the three original frames. Locks the
// parser + inline retrace against actual R8 output.
func TestRetraceRealInlinedOutput(t *testing.T) {
	mapping := "com.example.androidobservability.MainActivityKt$$ExternalSyntheticLambda10 -> g5.g:\n" +
		"    52:64:int com.example.androidobservability.CartPricing.computeTotal(java.lang.String):19:19 -> invoke\n" +
		"    52:64:int com.example.androidobservability.CartPricing.priceOrder(java.lang.String):16 -> invoke\n" +
		"    52:64:int com.example.androidobservability.CheckoutDemo.startCheckout(java.lang.String):12 -> invoke\n"

	frames := Parse([]byte(mapping)).Retrace("g5.g", "invoke", 57)
	assert.Len(t, frames, 3)
	assert.Equal(t, "com.example.androidobservability.CartPricing", frames[0].Class)
	assert.Equal(t, "computeTotal", frames[0].Method)
	assert.Equal(t, 19, frames[0].Line)
	assert.Equal(t, "priceOrder", frames[1].Method)
	assert.Equal(t, 16, frames[1].Line)
	assert.Equal(t, "com.example.androidobservability.CheckoutDemo", frames[2].Class)
	assert.Equal(t, "startCheckout", frames[2].Method)
	assert.Equal(t, 12, frames[2].Line)
}

// R8 renames a throwable's class like any other, so a reported exception type
// arrives obfuscated even though that class need not appear in any frame.
// androidx.core.Kept stands in for a class R8 chose to leave alone.
const exceptionTypeMapping = `# compiler: R8
com.example.app.PaymentFailedException -> g5.a:
com.example.app.CheckoutActivity -> a.b.e:
    12:12:void pay():88:88 -> a
androidx.core.Kept -> androidx.core.Kept:
`

func TestRetraceClass(t *testing.T) {
	m := Parse([]byte(exceptionTypeMapping))
	require.NotNil(t, m)

	original, ok := m.RetraceClass("g5.a")
	assert.True(t, ok)
	assert.Equal(t, "com.example.app.PaymentFailedException", original)

	_, ok = m.RetraceClass("java.lang.IllegalStateException")
	assert.False(t, ok, "a class outside the mapping has nothing to retrace to")
}

// --- R8 sourceFile metadata ---

func TestParseSourceFileComment(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"kotlin file", `# {"id":"sourceFile","fileName":"SymbolicationDemo.kt"}`, "SymbolicationDemo.kt"},
		{"java file", `# {"id":"sourceFile","fileName":"UserService.java"}`, "UserService.java"},
		// R8's own placeholders name no real file, so the derived guess is no worse.
		{"synthetic class", `# {"id":"sourceFile","fileName":"R8$$SyntheticClass"}`, ""},
		{"renamed", `# {"id":"sourceFile","fileName":"SourceFile"}`, ""},
		{"other metadata", `# {"id":"com.android.tools.r8.synthesized"}`, ""},
		{"not json", `# compiler: R8`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, parseSourceFileComment(c.in))
		})
	}
}

// The metadata comment sits under the *obfuscated* class header but describes the
// original class, which is the name a retraced frame reports.
func TestParseReadsSourceFileMetadata(t *testing.T) {
	m := Parse([]byte(`# compiler: R8
com.example.app.CheckoutDemo -> a.b.c:
# {"id":"sourceFile","fileName":"SymbolicationDemo.kt"}
    1:3:int startCheckout(java.lang.String):12:12 -> a
com.example.app.Synthetic -> a.b.d:
# {"id":"sourceFile","fileName":"R8$$SyntheticClass"}
    1:1:void run():0:0 -> a
`))

	assert.Equal(t, "SymbolicationDemo.kt", m.SourceFile("com.example.app.CheckoutDemo"))
	assert.Empty(t, m.SourceFile("com.example.app.Synthetic"), "placeholder is not recorded")
}

// --- R8 synthesized (compiler-generated) frames ---

func TestParseSynthesizedComment(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"marker", `# {"id":"com.android.tools.r8.synthesized"}`, true},
		{"source file", `# {"id":"sourceFile","fileName":"Checkout.kt"}`, false},
		{"residual signature", `# {"id":"com.android.tools.r8.residualsignature","signature":"(I)V"}`, false},
		{"header", `# compiler: R8`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, parseSynthesizedComment(c.in))
		})
	}
}

// A comment describes the entry above it, so where the marker lands depends on what
// was last read. The field case is the one that bites: ordinary classes carry
// synthesized fields, and attributing one to the class would declare the whole class
// compiler-generated.
func TestParseSynthesizedScope(t *testing.T) {
	m := Parse([]byte(`com.example.app.Lambda$$ExternalSyntheticLambda0 -> a.b.c:
# {"id":"com.android.tools.r8.synthesized"}
    1:1:void run():0:0 -> a
com.example.app.Checkout -> a.b.d:
    int field -> f
      # {"id":"com.android.tools.r8.synthesized"}
    1:3:void pay():40:40 -> a
    5:7:void bridge():0:0 -> b
      # {"id":"com.android.tools.r8.synthesized"}
`))

	assert.True(t, m.classes["a.b.c"].synthesized, "class-level marker")
	assert.False(t, m.classes["a.b.d"].synthesized, "a synthesized field must not mark its class")
	assert.False(t, m.classes["a.b.d"].members["a"][0].synthesized, "unmarked member")
	assert.True(t, m.classes["a.b.d"].members["b"][0].synthesized, "member-level marker")
}

// The frame the device reports is the synthetic lambda's own `invoke`, expanded by R8
// into the five real frames it inlined plus itself. Only that last one is
// scaffolding, and it is reported under the package from the class header rather than
// the residual "g5." R8 wrote on the member line.
func TestRetraceMarksSyntheticFrame(t *testing.T) {
	frames := Parse([]byte(syntheticLambdaMapping)).Retrace("g5.g", "invoke", 57)

	require.Len(t, frames, 5)
	for i, want := range []string{
		"com.example.androidobservability.CartPricing.computeTotal",
		"com.example.androidobservability.CartPricing.priceOrder",
		"com.example.androidobservability.CheckoutDemo.startCheckout",
		"com.example.androidobservability.MainActivityKt.ErrorButtons$lambda$59$lambda$58",
	} {
		assert.Equal(t, want, frames[i].Class+"."+frames[i].Method)
		assert.False(t, frames[i].Synthetic, "%s is code someone wrote", want)
	}

	synthetic := frames[4]
	assert.True(t, synthetic.Synthetic)
	assert.Equal(t, "com.example.androidobservability.MainActivityKt$$ExternalSyntheticLambda10", synthetic.Class)
	assert.Equal(t, "invoke", synthetic.Method)
	assert.False(t, synthetic.Inlined, "the synthetic class's own frame is the physical one")
}

// The lookups have to be safe on a mapping that was never loaded, since that is what
// a build with no symbols uploaded looks like.
func TestNilMappingAnswersNothing(t *testing.T) {
	var m *Mapping
	assert.Nil(t, m.Retrace("a", "b", 1))
	_, ok := m.RetraceClass("a")
	assert.False(t, ok)
	assert.Empty(t, m.SourceFile("a"))
	assert.Zero(t, m.Classes())
}

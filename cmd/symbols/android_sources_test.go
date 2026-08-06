package symbols

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/symbols/srcbundle"
)

func writeFile(t *testing.T, dir, rel, content string) string {
	t.Helper()
	full := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	return full
}

func TestJavaPackageOf(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"java", "package com.example.app;\n\nclass Foo {}\n", "com.example.app"},
		{"kotlin no semicolon", "package com.example.app\n\nclass Foo\n", "com.example.app"},
		{"after license header", "/*\n * (c) 2026\n */\npackage com.example;\n", "com.example"},
		{"after line comment", "// generated\npackage com.example\n", "com.example"},
		{"after file annotation", "@file:JvmName(\"Utils\")\npackage com.example\n", "com.example"},
		{"trailing comment", "package com.example; // note\n", "com.example"},
		{"default package", "class Foo {}\n", ""},
		{"import first means none", "import java.util.List;\nclass Foo {}\n", ""},
		{"empty", "", ""},

		// A block comment's continuation lines need no leading "*", so they look
		// exactly like source to a per-line rule. This is the shape of most
		// license headers.
		{
			"license header without leading stars",
			"/*\n   Copyright 2026 Example Inc.\n\n   Licensed under the Apache License, Version 2.0.\n   http://www.apache.org/licenses/LICENSE-2.0\n*/\npackage com.example;\n",
			"com.example",
		},
		{"block comment on the package line", "/* header */ package com.example;\n", "com.example"},
		{"block comment before the keyword", "/*\n c\n*/ package com.example\n", "com.example"},
		{"comment between annotation and package", "@file:JvmName(\"Utils\")\n/*\n note\n*/\npackage com.example\n", "com.example"},
		{"nested block comment", "/* outer /* inner */ still outer */\npackage com.example;\n", "com.example"},
		{"comment closes and declares on one line", "/* c */ class Foo {}\n", ""},

		// "//" inside an annotation's string argument is not a comment.
		{"url in annotation argument", "@file:Suppress(\"http://x\")\npackage com.example\n", "com.example"},

		// An identifier that merely starts with the keyword is not the keyword.
		{"packageName identifier", "packageName = 1\n", ""},
		{"tab after keyword", "package\tcom.example;\n", "com.example"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, javaPackageOf([]byte(c.src)))
		})
	}
}

// The bundle key is the package-relative path, which is what the backend
// reconstructs from a retraced frame's fully-qualified class name.
func TestAndroidSourceKey(t *testing.T) {
	assert.Equal(t, "com/example/app/MainActivity.kt",
		androidSourceKey([]byte("package com.example.app\n"), "MainActivity.kt"))
	assert.Equal(t, "com/example/Foo.java",
		androidSourceKey([]byte("package com.example;\n"), "Foo.java"))
	// No package declaration: the JVM default package, keyed by bare name.
	assert.Equal(t, "Foo.java", androidSourceKey([]byte("class Foo {}\n"), "Foo.java"))
	// A license header must not cost the file its package: a bare "Foo.java" key
	// would never match the "com/example/Foo.java" the backend looks up.
	assert.Equal(t, "com/example/Foo.java", androidSourceKey(
		[]byte("/*\n   Copyright 2026 Example Inc.\n*/\npackage com.example;\n"), "Foo.java"))
}

// The key comes from the package declaration, not the directory layout, because
// Kotlin does not require the two to agree.
func TestBuildAndroidSourceBundleKeysByPackageNotLayout(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app/src/main/kotlin/MainActivity.kt", "package com.example.app\n\nclass MainActivity\n")
	writeFile(t, dir, "app/src/main/java/com/example/util/Helper.java", "package com.example.util;\n\nclass Helper {}\n")

	data, count, err := buildAndroidSourceBundle(dir)
	require.NoError(t, err)
	require.NotNil(t, data)
	assert.Equal(t, 2, count)

	bundle, err := srcbundle.Open(data)
	require.NoError(t, err)

	// Declared package wins over the flat directory it happened to live in.
	_, content, _, ok := bundle.Window("com/example/app/MainActivity.kt", 3, 1)
	require.True(t, ok)
	assert.Equal(t, "class MainActivity\n", content)

	_, ok = bundle.File("com/example/util/Helper.java")
	assert.True(t, ok)

	_, ok = bundle.File("app/src/main/kotlin/MainActivity.kt")
	assert.False(t, ok, "on-disk path must not be a key")
}

func TestBuildAndroidSourceBundleSkipsBuildDirs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app/src/main/java/com/example/Kept.java", "package com.example;\nclass Kept {}\n")
	writeFile(t, dir, "app/build/generated/com/example/Generated.java", "package com.example;\nclass Generated {}\n")
	writeFile(t, dir, "node_modules/pkg/Vendored.java", "package vendored;\nclass Vendored {}\n")
	writeFile(t, dir, "app/src/main/res/values/strings.xml", "<resources/>\n")

	data, count, err := buildAndroidSourceBundle(dir)
	require.NoError(t, err)
	require.NotNil(t, data)
	assert.Equal(t, 1, count, "only the hand-written source is bundled")

	bundle, err := srcbundle.Open(data)
	require.NoError(t, err)
	_, ok := bundle.File("com/example/Kept.java")
	assert.True(t, ok)
	_, ok = bundle.File("com/example/Generated.java")
	assert.False(t, ok, "build output must be skipped")
	_, ok = bundle.File("vendored/Vendored.java")
	assert.False(t, ok, "node_modules must be skipped")
}

func TestAndroidSourceSetOf(t *testing.T) {
	cases := []struct {
		rel  string
		want string
	}{
		{"app/src/main/java/com/example/Foo.java", "main"},
		{"app/src/debug/kotlin/com/example/Foo.kt", "debug"},
		{"features/login/src/free/java/com/example/Foo.java", "free"},
		{"src/main/Foo.kt", "main"},
		// Not the src/<set>/ layout: a bare tree, or "src" with nothing under it.
		{"com/example/Foo.java", ""},
		{"src/Foo.java", ""},
		// The innermost "src" names the set, so a module called "src" upstream of
		// the real one does not.
		{"src/vendor/app/src/main/java/com/example/Foo.java", "main"},
	}
	for _, c := range cases {
		t.Run(c.rel, func(t *testing.T) {
			assert.Equal(t, c.want, androidSourceSetOf(filepath.FromSlash(c.rel)))
		})
	}
}

func TestIsAndroidTestSourceSet(t *testing.T) {
	for _, name := range []string{"test", "androidTest", "testDebug", "androidTestFreeRelease", "testFixtures"} {
		assert.True(t, isAndroidTestSourceSet(name), name)
	}
	// A flavor is not a test source set because its name opens with the same
	// letters ("testing" is a perfectly good flavor name).
	for _, name := range []string{"main", "debug", "release", "testing", "androidTesting", "latest", ""} {
		assert.False(t, isAndroidTestSourceSet(name), name)
	}
}

// Defining a class per build variant is ordinary Gradle practice, so two files
// legitimately claim one bundle key. The main source set has to win: "debug"
// sorts before "main", so taking the first file the walk reaches would ship the
// debug copy as the source for every retraced frame in that class.
func TestBuildAndroidSourceBundlePrefersMainSourceSet(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app/src/debug/java/com/example/Config.java", "package com.example;\nclass Config { boolean debug = true; }\n")
	writeFile(t, dir, "app/src/main/java/com/example/Config.java", "package com.example;\nclass Config { boolean debug = false; }\n")

	data, count, err := buildAndroidSourceBundle(dir)
	require.NoError(t, err)
	require.NotNil(t, data)
	assert.Equal(t, 1, count, "one key, one file")

	bundle, err := srcbundle.Open(data)
	require.NoError(t, err)
	_, content, _, ok := bundle.Window("com/example/Config.java", 2, 0)
	require.True(t, ok)
	assert.Equal(t, "class Config { boolean debug = false; }\n", content, "main wins over the variant")
}

// Ranks tie between two flavors overriding the same class. Whichever is chosen,
// it must be the same one every run, or successive uploads of an unchanged tree
// would disagree about a frame's source.
func TestBuildAndroidSourceBundleResolvesFlavorTieDeterministically(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app/src/paid/java/com/example/Config.java", "package com.example;\nclass Config { int tier = 2; }\n")
	writeFile(t, dir, "app/src/free/java/com/example/Config.java", "package com.example;\nclass Config { int tier = 1; }\n")

	first, count, err := buildAndroidSourceBundle(dir)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	second, _, err := buildAndroidSourceBundle(dir)
	require.NoError(t, err)
	assert.Equal(t, first, second, "same tree, same bundle bytes")

	bundle, err := srcbundle.Open(first)
	require.NoError(t, err)
	_, content, _, ok := bundle.Window("com/example/Config.java", 2, 0)
	require.True(t, ok)
	assert.Equal(t, "class Config { int tier = 1; }\n", content, "walk order settles the tie")
}

// Test code is compiled into neither the app nor its mapping.txt, so no retraced
// frame can point at it — and a test-only class sharing a production class's
// package and file name would otherwise take its key.
func TestBuildAndroidSourceBundleSkipsTestSourceSets(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app/src/main/java/com/example/Config.java", "package com.example;\nclass Config { int tier = 1; }\n")
	writeFile(t, dir, "app/src/androidTest/java/com/example/Config.java", "package com.example;\nclass Config { int tier = 99; }\n")
	writeFile(t, dir, "app/src/test/java/com/example/ConfigTest.java", "package com.example;\nclass ConfigTest {}\n")
	writeFile(t, dir, "app/src/testDebug/java/com/example/Fixtures.java", "package com.example;\nclass Fixtures {}\n")

	data, count, err := buildAndroidSourceBundle(dir)
	require.NoError(t, err)
	require.NotNil(t, data)
	assert.Equal(t, 1, count, "only the production source is bundled")

	bundle, err := srcbundle.Open(data)
	require.NoError(t, err)
	_, content, _, ok := bundle.Window("com/example/Config.java", 2, 0)
	require.True(t, ok)
	assert.Equal(t, "class Config { int tier = 1; }\n", content)
	_, ok = bundle.File("com/example/ConfigTest.java")
	assert.False(t, ok, "unit tests must be skipped")
	_, ok = bundle.File("com/example/Fixtures.java")
	assert.False(t, ok, "variant-specific test sets must be skipped too")
}

// A --source-path aimed straight at a test source set asks for it by name, so the
// prune must not empty the bundle.
func TestBuildAndroidSourceBundleHonoursTestSourceSetAsRoot(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app/src/androidTest/java/com/example/ConfigTest.java", "package com.example;\nclass ConfigTest {}\n")

	data, count, err := buildAndroidSourceBundle(filepath.Join(dir, "app", "src", "androidTest"))
	require.NoError(t, err)
	require.NotNil(t, data)
	assert.Equal(t, 1, count)
}

func TestBuildAndroidSourceBundleSkipsOversizeFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "com/example/Small.java", "package com.example;\nclass Small {}\n")
	big := make([]byte, maxSourceFileBytes+1)
	copy(big, []byte("package com.example;\n"))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Big.java"), big, 0o644))

	data, count, err := buildAndroidSourceBundle(dir)
	require.NoError(t, err)
	require.NotNil(t, data)
	assert.Equal(t, 1, count)
}

func TestBuildAndroidSourceBundleNoSources(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "# nothing to see\n")

	data, count, err := buildAndroidSourceBundle(dir)
	require.NoError(t, err)
	assert.Nil(t, data, "no sources yields no bundle rather than an error")
	assert.Zero(t, count)
}

func TestBuildAndroidSourceBundleRejectsBadPath(t *testing.T) {
	_, _, err := buildAndroidSourceBundle(filepath.Join(t.TempDir(), "missing"))
	assert.Error(t, err)

	file := writeFile(t, t.TempDir(), "a.java", "package a;\n")
	_, _, err = buildAndroidSourceBundle(file)
	assert.Error(t, err, "a file is not a source root")
}

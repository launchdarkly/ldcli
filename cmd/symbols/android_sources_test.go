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

package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// projectShape is a real-world project layout reduced to the files detection reads.
// want.EntryPoint is relative to the materialized directory and joined before the
// comparison.
type projectShape struct {
	name    string
	files   map[string]string
	dirs    []string
	want    DetectResult
	wantErr bool
}

// pkgJSON builds a package.json listing deps as dependencies; a dep prefixed with
// "dev:" goes to devDependencies instead.
func pkgJSON(deps ...string) string {
	prod, dev := "", ""
	for _, d := range deps {
		if name, ok := cutDevPrefix(d); ok {
			dev += `"` + name + `":"1.0.0",`
			continue
		}
		prod += `"` + d + `":"1.0.0",`
	}
	return `{"dependencies":{` + trimComma(prod) + `},"devDependencies":{` + trimComma(dev) + `}}`
}

func cutDevPrefix(d string) (string, bool) {
	if len(d) > 4 && d[:4] == "dev:" {
		return d[4:], true
	}
	return "", false
}

func trimComma(s string) string {
	if s == "" {
		return s
	}
	return s[:len(s)-1]
}

// TestFileDetector_ProjectShapes asserts the whole DetectResult for each layout, so a
// field the detector stops populating fails here even when the SDK id stays right.
func TestFileDetector_ProjectShapes(t *testing.T) {
	shapes := []projectShape{
		// --- JavaScript / Node ---
		{
			name: "next app router",
			files: map[string]string{
				"package.json":   pkgJSON("next", "react"),
				"next.config.ts": "export default {}",
				"app/layout.tsx": "export default function Layout() {}",
				"app/page.tsx":   "export default function Page() {}",
				"tsconfig.json":  "{}",
				"next-env.d.ts":  "",
			},
			// A page module may be browser-bundled, which would ship the SDK key.
			want: DetectResult{Language: "JavaScript", Framework: "Next.js", PackageManager: "npm", SDKID: "node-server", EntryPoint: "instrumentation.ts"},
		},
		{
			name: "next src dir",
			files: map[string]string{
				"package.json":       pkgJSON("next", "react"),
				"src/app/page.tsx":   "export default function Page() {}",
				"src/app/layout.tsx": "export default function Layout() {}",
			},
			want: DetectResult{Language: "JavaScript", Framework: "Next.js", PackageManager: "npm", SDKID: "node-server", EntryPoint: "instrumentation.ts"},
		},
		{
			name: "next src instrumentation",
			files: map[string]string{
				"package.json":           pkgJSON("next"),
				"src/instrumentation.ts": "export function register() {}",
				"src/app/page.tsx":       "export default function Page() {}",
			},
			want: DetectResult{Language: "JavaScript", Framework: "Next.js", PackageManager: "npm", SDKID: "node-server", EntryPoint: "src/instrumentation.ts", EntryPointExists: true},
		},
		{
			name: "next root instrumentation",
			files: map[string]string{
				"package.json":       pkgJSON("next"),
				"instrumentation.ts": "export function register() {}",
				"app/page.tsx":       "export default function Page() {}",
			},
			want: DetectResult{Language: "JavaScript", Framework: "Next.js", PackageManager: "npm", SDKID: "node-server", EntryPoint: "instrumentation.ts", EntryPointExists: true},
		},
		{
			name:  "next pages router",
			files: map[string]string{"package.json": pkgJSON("next", "react"), "pages/index.tsx": "export default function Home() {}"},
			want:  DetectResult{Language: "JavaScript", Framework: "Next.js", PackageManager: "npm", SDKID: "node-server", EntryPoint: "instrumentation.ts"},
		},
		{
			name:  "next bare",
			files: map[string]string{"package.json": pkgJSON("next")},
			want:  DetectResult{Language: "JavaScript", Framework: "Next.js", PackageManager: "npm", SDKID: "node-server", EntryPoint: "instrumentation.ts"},
		},
		{
			name:  "node bun",
			files: map[string]string{"package.json": pkgJSON("hono"), "bun.lockb": ""},
			want:  DetectResult{Language: "JavaScript", PackageManager: "bun", SDKID: "node-server", EntryPoint: "index.js"},
		},
		{
			name:  "node npm",
			files: map[string]string{"package.json": pkgJSON("express"), "package-lock.json": "{}", "index.js": "// entry"},
			want:  DetectResult{Language: "JavaScript", PackageManager: "npm", SDKID: "node-server", EntryPoint: "index.js", EntryPointExists: true},
		},
		{
			name:  "node pnpm typescript",
			files: map[string]string{"package.json": pkgJSON("express"), "pnpm-lock.yaml": "", "src/index.ts": "// entry"},
			want:  DetectResult{Language: "JavaScript", PackageManager: "pnpm", SDKID: "node-server", EntryPoint: "src/index.ts", EntryPointExists: true},
		},
		{
			name:  "nest bootstraps from src/main.ts",
			files: map[string]string{"package.json": pkgJSON("@nestjs/core", "@nestjs/common"), "src/main.ts": "bootstrap()"},
			want:  DetectResult{Language: "JavaScript", PackageManager: "npm", SDKID: "node-server", EntryPoint: "src/main.ts", EntryPointExists: true},
		},
		{
			name:  "node prefers src/index over src/main",
			files: map[string]string{"package.json": pkgJSON("express"), "src/index.ts": "// entry", "src/main.ts": "// other"},
			want:  DetectResult{Language: "JavaScript", PackageManager: "npm", SDKID: "node-server", EntryPoint: "src/index.ts", EntryPointExists: true},
		},
		{
			name:  "node yarn server file",
			files: map[string]string{"package.json": pkgJSON("fastify"), "yarn.lock": "", "server.js": "// entry"},
			want:  DetectResult{Language: "JavaScript", PackageManager: "yarn", SDKID: "node-server", EntryPoint: "server.js", EntryPointExists: true},
		},
		{
			name:  "react vite mount point only",
			files: map[string]string{"package.json": pkgJSON("react", "dev:vite"), "src/main.tsx": "createRoot()"},
			want:  DetectResult{Language: "JavaScript", Framework: "React", PackageManager: "npm", SDKID: "react-client-sdk", EntryPoint: "src/main.tsx", EntryPointExists: true},
		},
		{
			name:  "react vite yarn",
			files: map[string]string{"package.json": pkgJSON("react", "dev:vite"), "yarn.lock": "", "src/App.tsx": "// App"},
			want:  DetectResult{Language: "JavaScript", Framework: "React", PackageManager: "yarn", SDKID: "react-client-sdk", EntryPoint: "src/App.tsx", EntryPointExists: true},
		},
		{
			name: "react vite full scaffold prefers App over mount",
			files: map[string]string{
				"package.json":   pkgJSON("react", "react-dom", "dev:vite", "dev:@vitejs/plugin-react"),
				"index.html":     "<div id=root></div>",
				"vite.config.ts": "export default {}",
				"src/App.tsx":    "// App",
				"src/main.tsx":   "createRoot()",
				"src/index.css":  "",
			},
			want: DetectResult{Language: "JavaScript", Framework: "React", PackageManager: "npm", SDKID: "react-client-sdk", EntryPoint: "src/App.tsx", EntryPointExists: true},
		},
		{
			name:  "react native",
			files: map[string]string{"package.json": pkgJSON("react", "react-native"), "App.tsx": "// App", "index.js": "AppRegistry.registerComponent()"},
			want:  DetectResult{Language: "JavaScript", Framework: "React Native", PackageManager: "npm", SDKID: "react-native", EntryPoint: "App.tsx", EntryPointExists: true},
		},
		{
			name:  "vue",
			files: map[string]string{"package.json": pkgJSON("vue"), "src/main.ts": "createApp()"},
			want:  DetectResult{Language: "JavaScript", Framework: "Vue", PackageManager: "npm", SDKID: "js-client-sdk", EntryPoint: "src/main.ts", EntryPointExists: true},
		},
		{
			name:  "svelte",
			files: map[string]string{"package.json": pkgJSON("svelte"), "src/main.ts": "new App()"},
			want:  DetectResult{Language: "JavaScript", Framework: "Svelte", PackageManager: "npm", SDKID: "js-client-sdk", EntryPoint: "src/main.ts", EntryPointExists: true},
		},

		// --- Go ---
		{
			name:  "go single main",
			files: map[string]string{"go.mod": "module example.com/app\n\ngo 1.22\n", "main.go": "package main\n"},
			want:  DetectResult{Language: "Go", PackageManager: "go", SDKID: "go-server-sdk", EntryPoint: "main.go", EntryPointExists: true},
		},
		{
			name:  "go single cmd binary",
			files: map[string]string{"go.mod": "module example.com/app\n\ngo 1.22\n", "cmd/server/main.go": "package main\n"},
			want:  DetectResult{Language: "Go", PackageManager: "go", SDKID: "go-server-sdk", EntryPoint: "main.go"},
		},
		{
			name: "go several cmd binaries",
			files: map[string]string{
				"go.mod":             "module example.com/app\n\ngo 1.22\n",
				"cmd/server/main.go": "package main\n",
				"cmd/worker/main.go": "package main\n",
			},
			want: DetectResult{Language: "Go", PackageManager: "go", SDKID: "go-server-sdk", EntryPoint: "main.go"},
		},

		// --- Python ---
		{
			name:  "python pipenv",
			files: map[string]string{"Pipfile": "[packages]\n", "main.py": "# main"},
			want:  DetectResult{Language: "Python", PackageManager: "pipenv", SDKID: "python-server-sdk", EntryPoint: "main.py", EntryPointExists: true},
		},
		{
			name:  "python poetry",
			files: map[string]string{"pyproject.toml": "[tool.poetry]\nname = \"app\"\n", "app.py": "# app"},
			want:  DetectResult{Language: "Python", PackageManager: "poetry", SDKID: "python-server-sdk", EntryPoint: "app.py", EntryPointExists: true},
		},
		{
			name:  "python uv lockfile",
			files: map[string]string{"pyproject.toml": "[project]\nname = \"app\"\n", "uv.lock": "version = 1\n"},
			want:  DetectResult{Language: "Python", PackageManager: "uv", SDKID: "python-server-sdk", EntryPoint: "main.py"},
		},
		{
			name:  "python requirements",
			files: map[string]string{"requirements.txt": "flask\n", "src/main.py": "# main"},
			want:  DetectResult{Language: "Python", PackageManager: "pip", SDKID: "python-server-sdk", EntryPoint: "src/main.py", EntryPointExists: true},
		},

		// --- Ruby ---
		{
			name:  "ruby bundler rack",
			files: map[string]string{"Gemfile": "source 'https://rubygems.org'\n", "Gemfile.lock": "", "config.ru": "run App"},
			want:  DetectResult{Language: "Ruby", PackageManager: "bundle", SDKID: "ruby-server-sdk", EntryPoint: "config.ru", EntryPointExists: true},
		},
		{
			name:  "ruby gemspec only",
			files: map[string]string{"mygem.gemspec": "Gem::Specification.new\n"},
			want:  DetectResult{Language: "Ruby", PackageManager: "gem", SDKID: "ruby-server-sdk", EntryPoint: "main.rb"},
		},

		// --- Java / Android ---
		{
			name:  "java maven",
			files: map[string]string{"pom.xml": "<project/>", "src/main/java/com/example/app/Application.java": "class Application {}"},
			want:  DetectResult{Language: "Java", PackageManager: "mvn", SDKID: "java-server-sdk", EntryPoint: "src/main/java/com/example/app/Application.java", EntryPointExists: true},
		},
		{
			name:  "java gradle",
			files: map[string]string{"build.gradle": "plugins { id 'java' }", "src/main/java/com/example/Main.java": "class Main {}"},
			want:  DetectResult{Language: "Java", PackageManager: "gradle", SDKID: "java-server-sdk", EntryPoint: "src/main/java/com/example/Main.java", EntryPointExists: true},
		},
		{
			name: "android app module kotlin",
			files: map[string]string{
				"build.gradle.kts":                                    "plugins { id(\"com.android.application\") }",
				"settings.gradle.kts":                                 "",
				"app/src/main/AndroidManifest.xml":                    "<manifest/>",
				"app/src/main/java/com/example/myapp/MainActivity.kt": "class MainActivity",
			},
			want: DetectResult{Language: "Java", PackageManager: "gradle", SDKID: "android", EntryPoint: "app/src/main/java/com/example/myapp/MainActivity.kt", EntryPointExists: true},
		},
		{
			name: "android single module java",
			files: map[string]string{
				"build.gradle":                                "plugins { id 'com.android.application' }",
				"src/main/AndroidManifest.xml":                "<manifest/>",
				"src/main/java/com/example/MainActivity.java": "class MainActivity {}",
			},
			want: DetectResult{Language: "Java", PackageManager: "gradle", SDKID: "android", EntryPoint: "src/main/java/com/example/MainActivity.java", EntryPointExists: true},
		},

		// --- Swift ---
		{
			name:  "swift package single target",
			files: map[string]string{"Package.swift": "// swift-tools-version:5.9", "Sources/MyTool/MyTool.swift": "print(1)", "Tests/MyToolTests/MyToolTests.swift": ""},
			want:  DetectResult{Language: "Swift", PackageManager: "spm", SDKID: "swift-client-sdk", EntryPoint: "Sources/MyTool/MyTool.swift", EntryPointExists: true},
		},
		{
			name:  "swift package sources without target dir",
			files: map[string]string{"Package.swift": "// swift-tools-version:5.9", "Sources/main.swift": "print(1)"},
			want:  DetectResult{Language: "Swift", PackageManager: "spm", SDKID: "swift-client-sdk", EntryPoint: "App.swift"},
		},
		{
			name: "swift package several targets",
			files: map[string]string{
				"Package.swift":              "// swift-tools-version:5.9",
				"Sources/Alpha/Helper.swift": "struct Helper {}",
				"Sources/Beta/main.swift":    "print(1)",
			},
			want: DetectResult{Language: "Swift", PackageManager: "spm", SDKID: "swift-client-sdk", EntryPoint: "App.swift"},
		},
		{
			name:  "swift xcode project",
			files: map[string]string{"MyApp/MyAppApp.swift": "@main struct MyAppApp {}", "MyApp/ContentView.swift": "struct ContentView {}"},
			dirs:  []string{"MyApp.xcodeproj"},
			want:  DetectResult{Language: "Swift", PackageManager: "spm", SDKID: "swift-client-sdk", EntryPoint: "MyApp/MyAppApp.swift", EntryPointExists: true},
		},
		{
			name:  "swift cocoapods",
			files: map[string]string{"Podfile": "platform :ios, '14.0'"},
			want:  DetectResult{Language: "Swift", PackageManager: "cocoapods", SDKID: "swift-client-sdk", EntryPoint: "App.swift"},
		},

		// --- C# ---
		{
			name:  "dotnet csproj",
			files: map[string]string{"MyApp.csproj": "<Project/>", "Program.cs": "// entry"},
			want:  DetectResult{Language: "C#", PackageManager: "dotnet", SDKID: "dotnet-server-sdk", EntryPoint: "Program.cs", EntryPointExists: true},
		},
		{
			name:  "dotnet solution with nested project",
			files: map[string]string{"MyApp.sln": "", "src/MyApp/MyApp.csproj": "<Project/>", "src/MyApp/Program.cs": "// entry"},
			want:  DetectResult{Language: "C#", PackageManager: "dotnet", SDKID: "dotnet-server-sdk", EntryPoint: "Program.cs"},
		},

		// --- Polyglot: a root package.json is usually build tooling ---
		{
			name: "rails with jsbundling",
			files: map[string]string{
				"Gemfile":      "source 'https://rubygems.org'\ngem 'rails'\n",
				"config.ru":    "run Rails.application",
				"package.json": pkgJSON("esbuild"),
			},
			want: DetectResult{Language: "Ruby", PackageManager: "bundle", SDKID: "ruby-server-sdk", EntryPoint: "config.ru", EntryPointExists: true},
		},
		{
			name: "django with tailwind",
			files: map[string]string{
				"requirements.txt": "Django==5.0\n",
				"manage.py":        "# manage",
				"package.json":     pkgJSON("dev:tailwindcss"),
			},
			want: DetectResult{Language: "Python", PackageManager: "pip", SDKID: "python-server-sdk", EntryPoint: "manage.py", EntryPointExists: true},
		},
		{
			name: "go binary published to npm",
			files: map[string]string{
				"go.mod":       "module example.com/app\n\ngo 1.22\n",
				"main.go":      "package main\n",
				"package.json": `{"name":"app-cli"}`,
			},
			want: DetectResult{Language: "Go", PackageManager: "go", SDKID: "go-server-sdk", EntryPoint: "main.go", EntryPointExists: true},
		},
		{
			name: "next app carrying a Gemfile",
			files: map[string]string{
				"package.json": pkgJSON("next"),
				"Gemfile":      "source 'https://rubygems.org'\ngem 'rubocop'\n",
			},
			// Accepted cost of preferring the backend manifest. The wizard lets the
			// user override the SDK, and --sdk-id exists.
			want: DetectResult{Language: "Ruby", PackageManager: "bundle", SDKID: "ruby-server-sdk", EntryPoint: "main.rb"},
		},
		{
			name: "next app carrying a ruff config",
			files: map[string]string{
				"package.json":   pkgJSON("next"),
				"pyproject.toml": "[tool.ruff]\nline-length = 100\n",
			},
			want: DetectResult{Language: "Python", PackageManager: "pip", SDKID: "python-server-sdk", EntryPoint: "main.py"},
		},

		// --- No manifest at all ---
		{name: "empty directory", wantErr: true},
		{name: "malformed package.json", files: map[string]string{"package.json": "not json {{{"}, wantErr: true},
	}

	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			dir := materialize(t, shape)

			result, err := FileDetector{}.Detect(dir)

			if shape.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "could not detect")
				return
			}
			require.NoError(t, err)
			want := shape.want
			want.EntryPoint = filepath.Join(dir, want.EntryPoint)
			assert.Equal(t, want, *result)
		})
	}
}

func materialize(t *testing.T, shape projectShape) string {
	t.Helper()
	dir := t.TempDir()
	for _, d := range shape.dirs {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, d), 0755))
	}
	for name, content := range shape.files {
		writeDetectFile(t, dir, name, content)
	}
	return dir
}

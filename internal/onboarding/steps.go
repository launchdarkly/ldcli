package onboarding

import "fmt"

// buildSteps returns the ordered list of onboarding steps for the agent to follow.
func buildSteps(baseURI, project, environment string) []Step {
	return []Step{
		buildDetectStep(),
		buildPlanStep(),
		buildApplyStep(),
		buildRunStep(),
		buildValidateStep(baseURI, project, environment),
		buildFirstFlagStep(baseURI, project, environment),
		buildRecoverStep(),
	}
}

func buildDetectStep() Step {
	return Step{
		ID:    "detect",
		Title: "Detect Repository Stack",
		Instructions: `Inspect the repository to identify the language, framework, package manager, and entrypoints.

1. List the files in the project root directory.
2. Look for these key indicator files to determine the language/framework:
   - package.json → Node.js / JavaScript / TypeScript (check for react, vue, next, express, etc.)
   - go.mod → Go
   - requirements.txt / pyproject.toml / setup.py / Pipfile → Python
   - pom.xml / build.gradle / build.gradle.kts → Java
   - Gemfile → Ruby
   - *.csproj / *.sln → .NET
   - composer.json → PHP
   - Cargo.toml → Rust
   - pubspec.yaml → Flutter/Dart
   - Package.swift / Podfile / *.xcodeproj → iOS/Swift
   - AndroidManifest.xml → Android
   - rebar.config / mix.exs → Erlang/Elixir
3. If package.json exists, read it to check for framework-specific dependencies:
   - "react" or "react-dom" → React client-side SDK
   - "react-native" → React Native mobile SDK
   - "vue" → Vue client-side SDK
   - "electron" → Node client-side SDK
   - "express", "fastify", "koa", "nestjs" → Node server-side SDK
4. Search for existing LaunchDarkly SDK imports in the codebase:
   - Look for patterns: "launchdarkly", "ldclient", "go-server-sdk", "LaunchDarkly"
   - If found, note which SDK is already in use and whether it appears properly initialized.
5. Identify candidate targets:
   - frontend: client-side SDKs (React, Vue, JS, etc.)
   - backend: server-side SDKs (Node, Go, Python, Java, etc.)
   - mobile: mobile SDKs (Swift, Android, Flutter, React Native)
6. Report your findings as structured data: language, framework, package_manager, sdk_already_present, candidate_targets.`,
		Tools:           []string{"readFile", "listDirectory", "searchPattern"},
		SuccessCriteria: "Language, framework, and package manager identified. Existing SDK presence determined.",
		Next:            "plan",
		OnFailure:       "recover",
	}
}

func buildPlanStep() Step {
	return Step{
		ID:    "plan",
		Title: "Generate Integration Plan",
		Instructions: `Based on the detection results, choose the correct SDK and generate a minimal integration plan.

1. Match the detected stack to an SDK recipe from the sdk_recipes list in this plan.
   - For server-side apps: use the server SDK for the detected language.
   - For client-side web apps: use the appropriate client SDK (React, Vue, or JS).
   - For mobile apps: use the mobile SDK (Swift, Android, Flutter, or React Native).
2. If multiple targets exist (e.g., a Next.js app with both server and client), choose the server-side SDK first.
3. If an SDK is already present and initialized:
   - Skip to the "validate" step to check if it's working.
   - If validation fails, proceed with the plan to fix the initialization.
4. Generate an integration plan with these details:
   - SDK to install (name and install command)
   - Files to modify (entrypoint files where SDK initialization should be added)
   - The import statement to add
   - The initialization code snippet
   - The key type needed: SDK key (server), client-side ID (client), or mobile key (mobile)
5. Present the plan to the user for confirmation before proceeding to the Apply step.`,
		Tools:           []string{"readFile", "searchPattern"},
		SuccessCriteria: "SDK selected and integration plan generated with specific files and code changes identified.",
		Next:            "apply",
		OnFailure:       "recover",
	}
}

func buildApplyStep() Step {
	return Step{
		ID:    "apply",
		Title: "Install Dependencies and Apply Code Changes",
		Instructions: `Execute the integration plan by installing the SDK and adding initialization code.

1. Install the SDK dependency using the package manager:
   - Run the install command from the chosen SDK recipe.
   - Verify the installation succeeded by checking the exit code and package lock file.
2. Add the SDK initialization code to the identified entrypoint file(s):
   - Add the import statement at the top of the file with other imports.
   - Add the initialization code near the application startup logic.
   - For server-side SDKs: initialize early in the app lifecycle (before request handling).
   - For client-side SDKs: wrap the app in the SDK provider component.
   - Use environment variables for the SDK key (e.g., LAUNCHDARKLY_SDK_KEY).
3. Add a simple feature flag evaluation to demonstrate the integration:
   - Add a boolean flag check with a hardcoded flag key (will be updated in the First Flag step).
   - Log the flag value to stdout so it's visible when the app runs.
4. If file edits are not permitted:
   - Present the exact code changes as copy/paste instructions.
   - Clearly indicate which file and line numbers to modify.
5. Commit the changes with a descriptive message.`,
		Tools:           []string{"writeFile", "runCommand", "readFile"},
		SuccessCriteria: "SDK dependency installed and initialization code added to the application entrypoint.",
		Next:            "run",
		OnFailure:       "recover",
	}
}

func buildRunStep() Step {
	return Step{
		ID:    "run",
		Title: "Start the Application",
		Instructions: `Attempt to start the application and confirm it can initialize the LaunchDarkly SDK.

1. Determine the correct start command for the application:
   - Check package.json scripts for "start", "dev", or "serve" commands.
   - Check for Makefiles, Procfiles, or docker-compose.yml.
   - For Go: "go run ." or "go run main.go"
   - For Python: check for manage.py (Django), or run the main script directly.
2. Ensure the SDK key environment variable is set:
   - The user must provide a valid LaunchDarkly SDK key.
   - Set LAUNCHDARKLY_SDK_KEY (server), or configure the client-side ID / mobile key in code.
3. Start the application and watch the output for:
   - "SDK successfully initialized" or similar success messages.
   - Connection errors or authentication failures.
   - The feature flag evaluation log line added in the Apply step.
4. If the app fails to start:
   - Check for missing dependencies or build errors.
   - Check that the SDK key is valid and has the correct permissions.
   - Trigger the recover step with relevant error details.
5. If the app starts but the SDK doesn't initialize:
   - Check network connectivity to LaunchDarkly.
   - Verify the SDK key matches the expected environment.
   - Check for firewall or proxy issues.
6. Leave the application running for the Validate step.`,
		Tools:           []string{"runCommand", "readFile"},
		SuccessCriteria: "Application is running and the LaunchDarkly SDK has initialized successfully.",
		Next:            "validate",
		OnFailure:       "recover",
	}
}

func buildValidateStep(baseURI, project, environment string) Step {
	return Step{
		ID:    "validate",
		Title: "Validate SDK Connection",
		Instructions: fmt.Sprintf(`Confirm that the LaunchDarkly API sees the SDK connection.

1. Wait 15-30 seconds after the application starts to allow the SDK to report activity.
2. Check the SDK active status using the LaunchDarkly API:
   - Run: ldcli environments get-sdk-active --project %s --environment %s --base-uri %s
   - Or call the API directly: GET %s/api/v2/projects/%s/environments/%s/sdk-active
3. If the response shows "active": true, the SDK is successfully connected.
4. If the response shows "active": false:
   - Verify the application is still running.
   - Check that the SDK key used in the application matches the environment being queried.
   - Wait another 30 seconds and retry (the signal can take up to 60 seconds).
   - If still not active after 2 minutes, trigger the recover step.
5. Once validated, report success and proceed to the First Flag step.`, project, environment, baseURI, baseURI, project, environment),
		Tools:           []string{"runCommand"},
		SuccessCriteria: "LaunchDarkly API confirms SDK is active for the target environment.",
		Next:            "first-flag",
		OnFailure:       "recover",
	}
}

func buildFirstFlagStep(baseURI, project, environment string) Step {
	return Step{
		ID:    "first-flag",
		Title: "Create Your First Feature Flag",
		Instructions: fmt.Sprintf(`Now that the SDK is verified, create and evaluate a feature flag.

1. Create a new boolean feature flag:
   - Run: ldcli flags create --project %s --data '{"name": "my-first-flag", "key": "my-first-flag", "clientSideAvailability": {"usingEnvironmentId": true, "usingMobileKey": true}}'
   - Or use the MCP tool: createFlag with project key and flag details.
2. Update the application code to evaluate this flag:
   - Replace any placeholder flag key with "my-first-flag".
   - Ensure the evaluation uses the correct context (e.g., user key, kind).
3. Verify the flag evaluates correctly:
   - With the flag OFF (default), the evaluation should return false.
   - Toggle the flag ON in LaunchDarkly:
     Run: ldcli flags toggle-on --access-token <token> --project %s --environment %s --flag my-first-flag
   - The application should now show the flag evaluating to true.
4. Demonstrate the flag toggle:
   - Toggle the flag off: ldcli flags toggle-off --access-token <token> --project %s --environment %s --flag my-first-flag
   - Observe the real-time update in the application output.
5. Congratulate the user on their first feature flag integration!
   - Suggest next steps: creating more flags, using targeting rules, setting up environments.
   - Point to documentation: https://docs.launchdarkly.com`, project, project, environment, project, environment),
		Tools:           []string{"runCommand", "writeFile", "readFile"},
		SuccessCriteria: "Feature flag created, evaluated successfully, and toggled on/off with visible results.",
		Next:            "",
		OnFailure:       "recover",
	}
}

func buildRecoverStep() Step {
	return Step{
		ID:    "recover",
		Title: "Recovery: Diagnose and Resume",
		Instructions: `When any step fails, follow this recovery procedure.

1. Identify the failed step and the error message.
2. Present the user with ranked recovery options:
   a. Retry Current Step: Re-attempt after reviewing the error.
   b. Verify Credentials: Check the access token is valid (ldcli environments list --project <project>).
   c. Check Network: Verify connectivity to LaunchDarkly endpoints.
   d. Manual Install: Provide copy/paste SDK installation instructions.
   e. Try Alternative SDK: Re-run detection or let the user pick a different SDK.
   f. Skip Step: If the failure is non-critical, move to the next step.
   g. Abort: Stop onboarding and provide manual setup documentation links.
3. Based on the user's choice, resume the workflow from the appropriate step.
4. If the same step fails 3 times, automatically suggest skipping or aborting.
5. Keep a log of all attempted actions and errors for debugging.`,
		Tools:           []string{"runCommand", "readFile"},
		SuccessCriteria: "User has chosen a recovery action and the workflow has resumed or been gracefully terminated.",
		Next:            "",
		OnFailure:       "",
	}
}

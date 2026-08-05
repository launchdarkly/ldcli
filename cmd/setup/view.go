package setup

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"

	"github.com/launchdarkly/ldcli/internal/setup"
)

var quitHint = "\n" + mutedStyle.Render("Press q to quit.") + "\n"

// copyHint labels the copy action next to a code block, or confirms the copy once
// it has happened. The block is drawn with a left gutter bar and the wizard owns the
// alternate screen, so selecting the code by hand picks up the gutter characters.
func (m wizardModel) copyHint() string {
	_, label, ok := m.copyableContent()
	if !ok {
		return ""
	}
	switch m.copyState {
	case copyDone:
		return mutedStyle.Render(fmt.Sprintf("Copied the %s to your clipboard.", label)) + "\n"
	case copyRequested:
		return mutedStyle.Render(fmt.Sprintf("Asked your terminal to copy the %s.", label)) + "\n"
	}
	return mutedStyle.Render(fmt.Sprintf("Press c to copy the %s.", label)) + "\n"
}

func (m wizardModel) View() string {
	if m.quitting {
		return ""
	}

	if m.err != nil {
		return titleStyle.Render("Error") + "\n\n" + m.err.Error() + "\n\nPress ctrl+c to quit."
	}

	switch m.step {
	case stepSelectProject:
		if !m.projectsLoaded {
			return m.spinner.View() + " Loading projects..."
		}
		if len(m.projects) == 0 {
			return titleStyle.Render("No projects available") + "\n\n" +
				m.wrap("This access token can't see any projects. Create a project in LaunchDarkly, or use a token with access to one, then run this command again.") + "\n" +
				quitHint
		}
		return m.projectList.View() + "\n" + mutedStyle.Render("esc quit")

	case stepSelectEnvironment:
		if !m.envsLoaded {
			return m.spinner.View() + " Loading environments..."
		}
		if len(m.environments) == 0 {
			return titleStyle.Render("No environments available") + "\n\n" +
				m.wrap(fmt.Sprintf("Project %q has no environments this access token can see. Press ← to pick another project.", m.selectedProject)) + "\n" +
				mutedStyle.Render("← back · q quit") + "\n"
		}
		return m.envList.View() + "\n" + mutedStyle.Render("← back · esc quit")

	case stepDetect:
		return m.spinner.View() + " Detecting project type..."

	case stepSelectSDK:
		return m.sdkSelectView()

	case stepPlan:
		return m.planView()

	case stepInstall:
		return m.spinner.View() + " Installing SDK..."

	case stepCreateFlag:
		return m.spinner.View() + " Creating feature flag..."

	case stepInit:
		return m.spinner.View() + " Injecting initialization code..."

	case stepWaitForApp:
		return titleStyle.Render("Start your application") + "\n\n" +
			"SDK initialization code has been injected into:\n" +
			"  " + m.initResult.FilePath + "\n\n" +
			"Please start your application now, then press Enter to verify the connection.\n"

	case stepVerify:
		return m.spinner.View() + " Waiting for SDK to connect..."

	case stepDone:
		if m.installResult != nil && m.installResult.Failed {
			body := titleStyle.Render("Manual install needed") + "\n\n" +
				m.wrap("The SDK couldn't be installed automatically.") + "\n\n"
			if m.installResult.FailureReason != "" {
				body += m.wrap("Reason: "+m.installResult.FailureReason) + "\n\n"
			}
			// The installer only supplies a command when one exists and failed to
			// run. When it declines up front, its reason above carries the command
			// to use instead, so don't contradict it with a broken one.
			if m.installResult.Command != "" {
				body += m.wrap("Install it yourself with:") + "\n\n" +
					code(m.installResult.Command) + "\n\n"
			}
			if m.initResult != nil && m.initResult.Success {
				body += m.wrap(fmt.Sprintf("Initialization code was added to %s.", m.initResult.FilePath)) + "\n"
			} else if m.initResult != nil && m.initResult.Snippet != "" {
				body += m.wrap(fmt.Sprintf("Then add this initialization code to %s:", m.initResult.FilePath)) +
					"\n\n" + code(m.initResult.Snippet) + "\n"
			}
			body += "\n" + m.wrap(fmt.Sprintf("Flag %q was created in project %q.", m.flagKey, m.selectedProject)) + "\n"
			return body + "\n" + m.copyHint() + quitHint
		}
		if m.initResult != nil && !m.initResult.Success {
			body := titleStyle.Render("Manual SDK setup required") + "\n\n"
			if m.initResult.Snippet != "" {
				body += m.wrap(fmt.Sprintf("Add the following %s initialization code to %s:", m.initResult.SDKID, m.initResult.FilePath)) +
					"\n\n" + code(m.initResult.Snippet) + "\n\n"
			} else {
				body += fmt.Sprintf("No initialization template is available for %s.\n", m.initResult.SDKID)
			}
			return body +
				fmt.Sprintf("Follow the setup guide at: %s\n\n", m.initResult.DocsURL) +
				fmt.Sprintf("Flag %q has been created in project %q.\n", m.flagKey, m.selectedProject) +
				"Once you've initialized the SDK manually, your flag will be ready to use.\n\n" +
				m.copyHint() +
				quitHint
		}
		if m.verifyResult != nil && m.verifyResult.Active && m.detectResult != nil {
			appHost := strings.TrimRight(m.auth.BaseURI, "/")
			return titleStyle.Render("Setup complete!") + "\n\n" +
				fmt.Sprintf("Your %s SDK is connected to LaunchDarkly.\n", m.detectResult.SDKID) +
				fmt.Sprintf("Flag %q is ready to use.\n\n", m.flagKey) +
				fmt.Sprintf("You can now toggle your flag at %s/projects/%s/flags/%s/targeting?env=%s\n", appHost, m.selectedProject, m.flagKey, m.selectedEnv) +
				quitHint
		}
		return titleStyle.Render("Verification timed out") + "\n\n" +
			"The SDK did not report as active within the timeout period.\n" +
			"Make sure your application is running and try again.\n" +
			quitHint
	}

	return ""
}

// findKnownSDK returns the sdkItem for the given SDK id, if it is one we know.
func findKnownSDK(id string) (sdkItem, bool) {
	for _, sdk := range setup.KnownSDKs {
		if sdk.ID == id {
			return sdkItem{id: sdk.ID, language: sdk.Language, name: sdk.Name}, true
		}
	}
	return sdkItem{}, false
}

// sdkItemsExcept returns all known SDKs as list items, omitting the given id.
func sdkItemsExcept(exclude string) []list.Item {
	items := make([]list.Item, 0, len(setup.KnownSDKs))
	for _, sdk := range setup.KnownSDKs {
		if sdk.ID == exclude {
			continue
		}
		items = append(items, sdkItem{id: sdk.ID, language: sdk.Language, name: sdk.Name})
	}
	return items
}

// sdkBoxWidth is the shared width for the detected panel and the SDK list box,
// so both areas line up.
func (m wizardModel) sdkBoxWidth() int {
	w := m.width - 4
	if w > 72 {
		w = 72
	}
	if w < 20 { // never wider than a very narrow terminal can show
		w = 20
	}
	return w
}

// listHeight is the height available to a full-screen list. It never returns a
// value below a usable minimum, because a WindowSizeMsg may not have arrived yet
// and m.height-4 would then be negative.
func (m wizardModel) listHeight() int {
	h := m.height - 4
	if h < 3 {
		h = 3
	}
	return h
}

// wrap reflows prose to the terminal width so it doesn't overflow narrow
// terminals. Code snippets are rendered raw (not passed through here).
func (m wizardModel) wrap(s string) string {
	return wrapText(s, m.width)
}

// sdkDelegate returns the list row renderer. When the list isn't the focused
// area, the selected row is styled like a normal row so it doesn't look active
// while the detected-SDK panel holds focus.
func sdkDelegate(focused bool) list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	if !focused {
		d.Styles.SelectedTitle = d.Styles.NormalTitle
		d.Styles.SelectedDesc = d.Styles.NormalDesc
	}
	return d
}

// newSDKList builds the list model for the SDK selection screen.
func (m wizardModel) newSDKList(items []list.Item, title string, focused bool) list.Model {
	h := m.height - 12
	if h < 3 {
		h = 3
	}
	l := list.New(items, sdkDelegate(focused), m.sdkBoxWidth()-2, h)
	l.Title = title
	l.Styles.Title = headerStyle // match the detected-SDK panel header, not the default title bar
	l.SetShowStatusBar(false)
	l.SetShowHelp(false) // we render a single key hint inside the box instead
	return l
}

// sdkSelectView renders the SDK selection screen. When an SDK was auto-detected
// it shows two areas: an "identified" panel on top and the list of other SDKs
// below; the focused area is highlighted. When detection failed, only the list
// is shown.
func (m wizardModel) sdkSelectView() string {
	hint := mutedStyle.Render("↑/↓ move · enter select · ← back · esc quit")
	catalog := mutedStyle.Render("Don't see your language? All LaunchDarkly SDKs: https://launchdarkly.com/docs/sdk")

	if m.detectedSDK == nil {
		listBox := box(true, m.sdkBoxWidth()).Render(m.sdkList.View() + "\n" + hint)
		return listBox + "\n" + catalog
	}

	boxW := m.sdkBoxWidth()
	panelStyle := box(m.sdkFocus == 0, boxW)
	listStyle := box(m.sdkFocus == 1, boxW)

	// Point to the detected SDK when its panel is focused, matching the list's cursor.
	label := fmt.Sprintf("%s  (%s)", m.detectedSDK.name, m.detectedSDK.language)
	if setup.RequiresManualInstall(m.detectedSDK.id) {
		label += " — manual install"
	}
	pointer := "  "
	if m.sdkFocus == 0 {
		pointer, label = selectedStyle.Render("❯ "), selectedStyle.Render(label)
	}
	panel := panelStyle.Render(
		headerStyle.Render("We identified this as your SDK") + "\n" +
			pointer + label + "\n" +
			mutedStyle.Render("Press Enter to use it"))

	listBox := listStyle.Render(m.sdkList.View() + "\n" + hint)

	return panel + "\n\n" + listBox + "\n" + catalog
}

// planView lists the steps setup will take, before any of them run, so the user
// knows what's about to happen and can confirm.
func (m wizardModel) planView() string {
	if m.detectResult == nil {
		return ""
	}
	name := m.detectResult.SDKID
	if nm, ok := findKnownSDK(m.detectResult.SDKID); ok {
		name = nm.name
	}

	var steps []string
	add := func(s string) {
		steps = append(steps, selectedStyle.Render(fmt.Sprintf("%d.", len(steps)+1))+" "+s)
	}

	switch {
	case m.planAlready:
		add(fmt.Sprintf("Install the %s SDK — %s", name, mutedStyle.Render("already installed, will skip")))
	case m.planInstallCmd != "":
		add(fmt.Sprintf("Install the %s SDK — %s", name, mutedStyle.Render(m.planInstallCmd)))
	default:
		add(fmt.Sprintf("Add the %s SDK %s", name, mutedStyle.Render("(manual install)")))
	}
	add(fmt.Sprintf("Create a feature flag in %s / %s", m.selectedProject, m.selectedEnv))
	if setup.InjectsInPlace(m.detectResult.SDKID) {
		// Say when the entry file does not exist yet: a file we create is not loaded
		// by the project, so the user needs the chance to back out and point us at
		// the real entry point.
		if m.detectResult.EntryPointExists {
			add(fmt.Sprintf("Add initialization code to %s", m.detectResult.EntryPoint))
		} else {
			add(fmt.Sprintf("Create %s with initialization code %s",
				m.detectResult.EntryPoint,
				mutedStyle.Render("(no entry file found — check this is where your app starts)")))
		}
		add("Verify the SDK connects to LaunchDarkly")
	} else {
		add("Show initialization code for you to add")
	}

	return headerStyle.Render("Here's what setup will do:") + "\n\n" +
		strings.Join(steps, "\n") + "\n\n" +
		mutedStyle.Render("Enter continue · ← back · esc quit")
}

// Commands that perform async work. Each is a thin tea.Cmd adapter over the
// orchestration service: it calls a step method and maps the result or error
// onto a wizard message. All API/filesystem work and business rules live in
// internal/setup.Service.

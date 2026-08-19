// Package render is the third pipeline stage (scan → enrich → render): it turns
// enriched flag data into the human-facing terminal view — a single box-drawing
// table fit to the terminal width. The machine-readable contract is `-format
// json` (handled in main); this package is display-only.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/launchdarkly/ldcli/internal/flagusage/enrich"
)

// stateOrder is the lifecycle sort order (most actionable first). Its index is the
// primary sort key for the combined table.
var stateOrder = []string{"orphanedReference", "readyToArchive", "readyForCodeRemoval", "inactive", "launched", "active", "unknown", "archived"}

func lifecycleRank(state string) int {
	for i, s := range stateOrder {
		if s == state {
			return i
		}
	}
	return len(stateOrder) // unknown states sort last
}

// lifecycleLabel maps the internal staleState to its display label. The JSON
// staleState value is unchanged (the machine contract); this is display-only.
func lifecycleLabel(state string) string {
	if state == "readyForCodeRemoval" {
		return "code removable"
	}
	return state
}

// Enriched renders all flags as a single box-drawing table with a leftmost
// Lifecycle column, sorted by lifecycle (stateOrder) then flag key, fit to
// maxWidth (0 = unconstrained).
func Enriched(w io.Writer, details []enrich.FlagDetail, windowLabel string, envs []string, maxWidth int) {
	if len(details) == 0 {
		return
	}

	sorted := append([]enrich.FlagDetail(nil), details...)
	sort.Slice(sorted, func(i, j int) bool {
		if ri, rj := lifecycleRank(sorted[i].StaleState), lifecycleRank(sorted[j].StaleState); ri != rj {
			return ri < rj
		}
		return sorted[i].Key < sorted[j].Key
	})

	// Uniq column appears only with -exposures data.
	hasUniq := false
	for _, d := range sorted {
		for _, envKey := range envs {
			if env, ok := d.Environments[envKey]; ok && env.UniqueContexts != nil {
				hasUniq = true
			}
		}
	}

	headers := []string{"Lifecycle", "Flag", "Env", "On", "Status", "Last Eval", "Evals/" + windowLabel}
	if hasUniq {
		headers = append(headers, "Uniq/"+windowLabel)
	}
	headers = append(headers, "Targeting")

	var rows [][]string
	for i, d := range sorted {
		if i > 0 {
			rows = append(rows, nil) // separator between flags
		}
		rows = append(rows, flagRows(d, envs, hasUniq)...)
	}
	fmt.Fprintln(w, renderTable(headers, rows, maxWidth))
}

// flagRows builds the rows for one flag (one per present env). The Lifecycle label
// and Flag key sit on the first env row; the age·callsites meta on the second (or
// its own row for a single-env flag). Env keys are shown via their display alias.
// A recommended hardcode value, when present, is appended at the bottom of the
// Lifecycle cell (below "code removable").
func flagRows(d enrich.FlagDetail, envs []string, hasUniq bool) [][]string {
	meta := flagMetaLine(d)

	// assemble builds one row in header order, honoring the optional Uniq column.
	assemble := func(lifecycle, flag, env, on, status, lastEval, evals, uniq, targeting string) []string {
		cells := []string{lifecycle, flag, env, on, status, lastEval, evals}
		if hasUniq {
			cells = append(cells, uniq)
		}
		return append(cells, targeting)
	}

	var present []string
	for _, envKey := range envs {
		if _, ok := d.Environments[envKey]; ok {
			present = append(present, envKey)
		}
	}

	var rows [][]string
	if len(present) == 0 {
		// No env data (e.g. orphaned): a dashed row + a meta continuation row.
		rows = append(rows,
			assemble(lifecycleLabel(d.StaleState), d.Key, "—", "—", "—", "—", "—", "—", "—"),
			assemble("", meta, "", "", "", "", "", "", ""),
		)
	} else {
		for i, envKey := range present {
			env := d.Environments[envKey]
			lifecycle, flag := "", ""
			switch i {
			case 0:
				lifecycle, flag = lifecycleLabel(d.StaleState), d.Key
			case 1:
				flag = meta
			}
			rows = append(rows, assemble(lifecycle, flag, aliasEnv(envKey), onOffCell(env), dashIfEmpty(env.FlagStatus),
				lastEvalCell(env), evalsCell(env), uniqCell(env), targetingCell(env, d.Variations)))
		}
		// A single-env flag never got the meta row (it would live on env row #2).
		if len(present) == 1 {
			rows = append(rows, assemble("", meta, "", "", "", "", "", "", ""))
		}
	}

	// Recommended hardcode value goes at the bottom of the Lifecycle cell — on the
	// flag's last existing row, not a new one. (Env-bearing flags always have ≥2
	// rows, so this never clobbers the label on row 0.)
	if d.RecommendedValue != nil && len(rows) > 0 {
		b, _ := json.Marshal(d.RecommendedValue)
		rows[len(rows)-1][0] = "hardcode " + string(b)
	}
	return rows
}

func flagMetaLine(d enrich.FlagDetail) string {
	age := "?"
	if !d.CreationDate.IsZero() {
		age = fmt.Sprintf("%dd", int(time.Since(d.CreationDate).Hours()/24))
	}
	meta := fmt.Sprintf("%s · %d×", age, d.CallSites)
	if d.Temporary {
		meta += " · temp"
	}
	return meta
}

func onOffCell(env enrich.EnvStatus) string {
	if env.On {
		return "ON"
	}
	return "OFF"
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// lastEvalCell shows how long ago the flag was last evaluated (e.g. "5d", "3h"),
// not the absolute date.
func lastEvalCell(env enrich.EnvStatus) string {
	if env.LastEvaluation.IsZero() {
		return "—"
	}
	d := time.Since(env.LastEvaluation)
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func evalsCell(env enrich.EnvStatus) string {
	if env.Evaluations.Total7d > 0 {
		return formatCount(env.Evaluations.Total7d)
	}
	return "—"
}

func uniqCell(env enrich.EnvStatus) string {
	if env.UniqueContexts == nil {
		return "—"
	}
	var parts []string
	for kind, stat := range env.UniqueContexts.ByContextKind {
		s := kind + "=" + formatCount(stat.Count)
		if stat.IsSampled {
			s += "~" // sampled estimate
		}
		parts = append(parts, s)
	}
	sort.Strings(parts)
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, ", ")
}

func targetingCell(env enrich.EnvStatus, variations []enrich.Variation) string {
	if !env.On {
		return "off"
	}
	if env.Targeting.IsSimpleToggle {
		if env.Targeting.FallthroughVariation != nil && *env.Targeting.FallthroughVariation >= 0 &&
			*env.Targeting.FallthroughVariation < len(variations) {
			return fmt.Sprintf("serves %v", variations[*env.Targeting.FallthroughVariation].Value)
		}
		return "on (simple)"
	}
	var parts []string
	if env.Targeting.RuleCount > 0 {
		parts = append(parts, fmt.Sprintf("%d rules", env.Targeting.RuleCount))
	}
	if env.Targeting.TargetCount > 0 {
		parts = append(parts, fmt.Sprintf("%d targets", env.Targeting.TargetCount))
	}
	if env.Targeting.ContextTargetCount > 0 {
		parts = append(parts, fmt.Sprintf("%d ctx-targets", env.Targeting.ContextTargetCount))
	}
	if env.Targeting.PrerequisiteCount > 0 {
		parts = append(parts, fmt.Sprintf("%d prereqs", env.Targeting.PrerequisiteCount))
	}
	if len(parts) == 0 {
		return "on"
	}
	return strings.Join(parts, ", ")
}

// maxCellWidth caps any single cell so one long value (e.g. a targeting summary)
// can't blow out the table when there's no width limit (piped/redirected output).
const maxCellWidth = 60

// minColWidth is the floor a column is shrunk to when fitting a narrow terminal.
const minColWidth = 6

// clipTo truncates s (by rune) to at most w columns, using an ellipsis.
func clipTo(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= w {
		return s
	}
	r := []rune(s)
	if w == 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}

// fitWidths shrinks the widest columns (one at a time, down to minColWidth) until
// the rendered table fits maxWidth. Table overhead is the bars and per-cell
// padding: n+1 vertical bars plus 2 padding spaces per column.
func fitWidths(width []int, maxWidth int) {
	n := len(width)
	overhead := (n + 1) + 2*n
	for {
		total := overhead
		for _, w := range width {
			total += w
		}
		if total <= maxWidth {
			return
		}
		widest, wi := minColWidth, -1
		for i, w := range width {
			if w > widest {
				widest, wi = w, i
			}
		}
		if wi == -1 {
			return // every column already at the floor; can't shrink further
		}
		width[wi]--
	}
}

// renderTable draws a box-drawing table, fitting maxWidth (0 = unconstrained) by
// shrinking the widest columns and truncating their cells with an ellipsis. A nil
// row renders as a horizontal separator (used between flags within a group).
func renderTable(headers []string, rows [][]string, maxWidth int) string {
	n := len(headers)

	// Natural width per column, bounded by maxCellWidth so one huge cell can't
	// dominate before we even consider the terminal.
	width := make([]int, n)
	note := func(i int, s string) {
		if l := utf8.RuneCountInString(s); l > width[i] {
			width[i] = l
			if width[i] > maxCellWidth {
				width[i] = maxCellWidth
			}
		}
	}
	for i := range headers {
		note(i, headers[i])
	}
	for _, row := range rows {
		for i := 0; i < n && i < len(row); i++ {
			note(i, row[i])
		}
	}

	if maxWidth > 0 {
		fitWidths(width, maxWidth)
	}

	var b strings.Builder
	border := func(left, mid, right string) {
		b.WriteString(left)
		for i, w := range width {
			b.WriteString(strings.Repeat("─", w+2))
			if i < n-1 {
				b.WriteString(mid)
			} else {
				b.WriteString(right)
			}
		}
		b.WriteString("\n")
	}
	rowLine := func(cells []string) {
		b.WriteString("│")
		for i, w := range width {
			cell := ""
			if i < len(cells) {
				cell = clipTo(cells[i], w)
			}
			b.WriteString(" " + cell + strings.Repeat(" ", w-utf8.RuneCountInString(cell)) + " │")
		}
		b.WriteString("\n")
	}

	border("┌", "┬", "┐")
	rowLine(headers)
	border("├", "┼", "┤")
	for _, row := range rows {
		if row == nil {
			border("├", "┼", "┤")
		} else {
			rowLine(row)
		}
	}
	border("└", "┴", "┘")
	return b.String()
}

func formatCount(n int64) string {
	switch {
	case n >= 1_000_000_000_000:
		return fmt.Sprintf("%.1fT", float64(n)/1e12)
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	default:
		return fmt.Sprintf("%d", n)
	}
}

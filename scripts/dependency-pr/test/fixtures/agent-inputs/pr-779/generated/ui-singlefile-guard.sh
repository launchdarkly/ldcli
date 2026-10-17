#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"
trap restore_tree EXIT
ensure_ui_deps || incomplete "npm ci failed, so the build did not run"
cd "$WT/$UI_DIR_REL" || incomplete "UI directory missing"
run npm run build >"$ARTIFACTS/build.out" 2>&1 || { tail -n 20 "$ARTIFACTS/build.out"; fail "npm run build fails"; }
html=dist/index.html
[ -f "$html" ] || fail "the build wrote no dist/index.html"
files=$(cd dist && find . -type f | sed 's#^\./##' | sort | paste -sd' ' -)
size=$(wc -c <"$html")
ref=$(git -C "$BASE_WT" show HEAD:"$UI_DIR_REL/dist/index.html" | wc -c)
ext=$(grep -oE '<script[^>]*\ssrc=[^>]*>|<link[^>]*rel="(stylesheet|modulepreload)"[^>]*>' "$html" | head -n3)
detail "dist files: \`$files\`; index.html $size bytes (base $ref bytes)"
problems=()
[ "$files" = "favicon-osmo-prod.svg index.html" ] || problems+=("dist/ has other files: $files")
[ -z "$ext" ] || problems+=("index.html loads external assets: $(printf '%s' "$ext" | paste -sd' ' -)")
grep -q '<div id="root">' "$html" || problems+=("index.html has no <div id=\"root\">")
grep -q '<script type="module"' "$html" || problems+=("index.html has no inline module script")
pct=$(( (size - ref) * 100 / ref ))
[ ${pct#-} -le 20 ] || problems+=("index.html size changed by ${pct}% against base")
[ ${#problems[@]} -eq 0 ] && pass "single-file dist/index.html with inline module script and #root ($size bytes, ${pct}% against base)"
for p in "${problems[@]}"; do detail "- $p"; done
fail "$(join_by '; ' "${problems[@]}")"

# run_one for verify.sh. Source after common.sh, with OUT, STATE, LOGS,
# BASE_WT, PR_WT, PROFILE, and VERIFY_SECRETS (unexported) set.

# run_one <kind> <id> <script> <side> <timeout>; writes <state>/<kind>/<id>/<side>/result.json
run_one() {
  local kind="$1" id="$2" script="$3" side="$4" timeout="$5"
  local wt="$PR_WT"
  [ "$side" = base ] && wt="$BASE_WT"
  local art="$STATE/$kind/$id/$side" logf="$LOGS/$side/$id.log"
  [ "$kind" = generated ] && logf="$LOGS/$side/generated-$id.log"
  rm -rf "$art"
  mkdir -p "$art" "$(dirname "$logf")"
  local envargs=()
  mapfile -t envargs < <(hermetic_env_args "$art/sandbox")
  if [ "$kind" = generated ]; then
    mapfile -t -O "${#envargs[@]}" envargs < <(generated_env_args "$art/sandbox")
  fi
  local start rc
  start=$(now_s)
  (
    cd "$wt" &&
      exec env "${envargs[@]}" \
        WT="$wt" SIDE="$side" BASE_WT="$BASE_WT" PR_WT="$PR_WT" ARTIFACTS="$art" \
        CLASSIFICATION="$STATE/classification.json" PR_META="$STATE/pr.json" \
        PROFILE="$PROFILE" VERIFY_ROOT="$VERIFY_ROOT" \
        timeout --kill-after=30 "$timeout" bash "$script"
  ) >"$logf" 2>&1 </dev/null &
  local pid=$!
  wait "$pid"
  rc=$?
  # timeout leads its own process group. A server that the check started and
  # did not stop stays in that group; it holds ports and inherited lock files.
  kill -- -"$pid" 2>/dev/null
  local files=()
  # The log and the top-level evidence files, which result.json and the comment read.
  mapfile -t files < <(find "$art" -maxdepth 1 -type f)
  scrub_files "$logf" "${files[@]}"
  local status summary
  status=$(cat "$art/status" 2>/dev/null || echo error)
  summary=$(cat "$art/summary" 2>/dev/null || true)
  if [ "$status" = error ]; then
    if [ "$rc" -eq 124 ] || [ "$rc" -eq 137 ]; then
      summary="timed out after ${timeout}s"
    else
      summary="exited with code $rc without reporting a status (see log)"
    fi
  fi
  jq -n --arg status "$status" --arg summary "$summary" \
    --arg log "${logf#"$OUT"/}" --argjson duration "$(($(now_s) - start))" \
    --arg question "$(cat "$art/question" 2>/dev/null)" \
    --slurpfile fix <(cat "$art/fix.json" 2>/dev/null || echo null) \
    --arg fingerprint "$(cat "$art/fingerprint" 2>/dev/null)" \
    --rawfile findings <(cat "$art/findings.list" 2>/dev/null) \
    --rawfile details <(cat "$art/details.md" 2>/dev/null) \
    --rawfile recs <(cat "$art/recommendations" 2>/dev/null) '
    {status: $status, summary: $summary, log: $log, duration_s: $duration,
     question: (if $question == "" then null else $question end),
     fix: $fix[0],
     fingerprint: (if $fingerprint == "" then null else $fingerprint end),
     findings: (if $findings == "" then null else ($findings | split("\n") | map(select(length > 0))) end),
     details: (if $details == "" then null else $details end),
     recommendations: ($recs | split("\n") | map(select(length > 0)))}' >"$art/result.json"
  log "  $id ($side): $status${summary:+ — $summary} [$(($(now_s) - start))s]"
}

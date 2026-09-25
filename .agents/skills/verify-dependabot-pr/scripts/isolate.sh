# Sets up an isolated environment for running ldcli during verification.
# Source it from bash; don't execute it:
#
#   source scripts/isolate.sh
#
# ldcli otherwise sends usage analytics to LaunchDarkly, checks GitHub for
# updates, and reads and writes the real config file and dev-server databases
# under the user's XDG directories.

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  echo "isolate.sh sets variables in your shell, so source it instead: source $0" >&2
  exit 1
fi

export LD_ANALYTICS_OPT_OUT=true
export LD_UPDATE_CHECK_OPT_OUT=true

if [ -z "${SMOKE_DIR:-}" ] || [ ! -d "$SMOKE_DIR" ]; then
  SMOKE_DIR="$(mktemp -d "${TMPDIR:-/tmp}/ldcli-verify.XXXXXX")"
fi
export SMOKE_DIR
export XDG_STATE_HOME="$SMOKE_DIR/state"
export XDG_CONFIG_HOME="$SMOKE_DIR/config"
mkdir -p "$XDG_STATE_HOME" "$XDG_CONFIG_HOME"

# 8765 is the dev-server's default port, and a contributor may already be using it.
_isolate_port_free() {
  ! (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null
}
if [ -z "${SMOKE_PORT:-}" ] || ! _isolate_port_free "$SMOKE_PORT"; then
  SMOKE_PORT=""
  for _isolate_port in $(seq 18765 18864); do
    if _isolate_port_free "$_isolate_port"; then
      SMOKE_PORT="$_isolate_port"
      break
    fi
  done
  unset _isolate_port
fi
unset -f _isolate_port_free
if [ -z "$SMOKE_PORT" ]; then
  echo "isolate.sh: no free port between 18765 and 18864" >&2
  return 1
fi
export SMOKE_PORT

echo "Isolated ldcli environment:"
echo "  LD_ANALYTICS_OPT_OUT=$LD_ANALYTICS_OPT_OUT"
echo "  LD_UPDATE_CHECK_OPT_OUT=$LD_UPDATE_CHECK_OPT_OUT"
echo "  SMOKE_DIR=$SMOKE_DIR"
echo "  XDG_STATE_HOME=$XDG_STATE_HOME"
echo "  XDG_CONFIG_HOME=$XDG_CONFIG_HOME"
echo "  SMOKE_PORT=$SMOKE_PORT"

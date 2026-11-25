# jq module: version parsing and update classification.
# Use with: jq -L "$VERIFY_ROOT/lib" 'include "semver"; ...'

# Accepts "v1.2.3", "1.2", "release-secrets-v1.2.0", Go pseudo-versions,
# and npm/semver prerelease suffixes. Returns null when no number is found.
def vparse:
  tostring as $raw
  | ($raw | sub("^[^0-9]*"; "")) as $s
  | ($s | capture("^(?<core>[0-9]+(\\.[0-9]+)*)(?<rest>.*)$")) // null
  | if . == null then null
    else {
      raw: $raw,
      nums: ((.core | split(".") | map(tonumber)) + [0, 0, 0])[0:3],
      pre: .rest,
      pseudo: (.rest | test("[0-9]{14}-[0-9a-f]{12}$"))
    }
    end;

# One of: added, removed, none, major, minor, patch, prerelease, pseudo,
# downgrade, unknown.
def semver_class($from; $to):
  if $from == null then "added"
  elif $to == null then "removed"
  else
    ($from | vparse) as $f | ($to | vparse) as $t
    | if $f == null or $t == null then (if $from == $to then "none" else "unknown" end)
      elif $f.nums == $t.nums then
        (if $f.pre == $t.pre then "none"
         elif $f.pseudo or $t.pseudo then "pseudo"
         else "prerelease" end)
      elif $f.nums > $t.nums then "downgrade"
      elif $f.nums[0] != $t.nums[0] then "major"
      elif $f.nums[1] != $t.nums[1] then "minor"
      else "patch"
      end
  end;

# Semver allows breaking changes on a major bump, and on a minor bump while
# the major version is 0.
def is_breaking($from; $to):
  semver_class($from; $to) as $c
  | if $c == "major" then true
    elif $c == "minor" then (($from | vparse).nums[0] == 0)
    else false
    end;

def tier_rank: {"low": 0, "medium": 1, "high": 2}[.] // 0;
def max_tier($a; $b): if ($a | tier_rank) >= ($b | tier_rank) then $a else $b end;

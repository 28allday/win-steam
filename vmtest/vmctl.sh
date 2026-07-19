#!/bin/bash
# vmctl.sh — drive the test VM through the QEMU human monitor.
#   ./vmctl.sh shot [out.png]      screenshot
#   ./vmctl.sh click X Y           left-click at pixel coords (1280x800)
#   ./vmctl.sh dblclick X Y
#   ./vmctl.sh key <k> [k…]        sendkey names (e.g. ret esc meta_l-r)
#   ./vmctl.sh type <text>         type ASCII text (letters/digits/. : / - _)
#   ./vmctl.sh raw <monitor cmd>
set -euo pipefail
cd "$(dirname "$0")"
W=1280 H=800
mon() { echo "$*" | socat - unix-connect:monitor.sock >/dev/null 2>&1; }

case "${1:?usage: vmctl.sh shot|click|dblclick|key|type|raw}" in
  shot)
    mon "screendump $PWD/shot.ppm"; sleep 0.7
    magick "$PWD/shot.ppm" "${2:-$PWD/shot.png}"
    echo "${2:-$PWD/shot.png}"
    ;;
  click|dblclick)
    # True absolute positioning needs QMP input-send-event (HMP mouse_move
    # is relative-only and useless with the tablet).
    x=$(( ${2:?x} * 32767 / W )); y=$(( ${3:?y} * 32767 / H ))
    move='{"execute":"input-send-event","arguments":{"events":[{"type":"abs","data":{"axis":"x","value":'$x'}},{"type":"abs","data":{"axis":"y","value":'$y'}}]}}'
    down='{"execute":"input-send-event","arguments":{"events":[{"type":"btn","data":{"down":true,"button":"left"}}]}}'
    up='{"execute":"input-send-event","arguments":{"events":[{"type":"btn","data":{"down":false,"button":"left"}}]}}'
    seq=("$move" "$down" "$up")
    [[ $1 == dblclick ]] && seq+=("$down" "$up")
    { echo '{"execute":"qmp_capabilities"}'; sleep 0.3
      for m in "${seq[@]}"; do echo "$m"; sleep 0.15; done
      sleep 0.3
    } | socat - unix-connect:qmp.sock >/dev/null 2>&1
    ;;
  key)
    shift
    for k in "$@"; do mon "sendkey $k"; sleep 0.18; done
    ;;
  type)
    shift
    txt="$*"
    for (( i=0; i<${#txt}; i++ )); do
      c="${txt:$i:1}"
      case "$c" in
        [a-z0-9]) k="$c" ;;
        [A-Z])    k="shift-$(tr '[:upper:]' '[:lower:]' <<<"$c")" ;;
        " ") k=spc ;;  ".") k=dot ;;  "/") k=slash ;;  "-") k=minus ;;
        "_") k=shift-minus ;;  ":") k=shift-semicolon ;;  ",") k=comma ;;
        "\\") k=less ;;  # UK layout: backslash is the 102nd key
        "|") k=shift-less ;;  "'") k=apostrophe ;;  '"') k=shift-2 ;;
        "=") k=equal ;;  ";") k=semicolon ;;  "(") k=shift-9 ;;
        ")") k=shift-0 ;;  "$") k=shift-4 ;;  "%") k=shift-5 ;;
        "~") k=shift-numbersign ;;  # UK: ~ is shift on the # key
        *) echo "unmapped char: $c" >&2; exit 1 ;;
      esac
      mon "sendkey $k"; sleep 0.12
    done
    ;;
  raw)
    shift; mon "$*"
    ;;
esac

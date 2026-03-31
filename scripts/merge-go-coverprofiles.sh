#!/bin/sh

set -eu

if [ "$#" -lt 3 ]; then
	echo "usage: $0 OUTPUT INPUT..." >&2
	exit 1
fi

output=$1
shift

first_input=$1
mode=$(sed -n '1s/^mode: //p' "$first_input")

if [ -z "$mode" ]; then
	echo "missing coverage mode in $first_input" >&2
	exit 1
fi

{
	echo "mode: $mode"
	tail -q -n +2 "$@" | awk '
		{
			key = $1 " " $2
			counts[key] += $3
		}
		END {
			for (key in counts) {
				print key " " counts[key]
			}
		}
	' | sort
} >"$output"

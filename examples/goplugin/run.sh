#!/bin/sh
# Builds every plugin of the go-plugin examples and runs each example's
# scenario, checking its output. Run from the repository root (make goplugin).
set -eu

root=$(pwd)
gig="$root/go-in-go"
ex="$root/examples/goplugin"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

for p in basic/plugin kv/plugin-plain kv/plugin-base64 bidirectional/plugin negotiated/plugin streaming/plugin; do
	"$gig" build "$ex/$p" 2>/dev/null
done
for e in basic kv bidirectional negotiated streaming; do
	go build -o "$work/$e" "./examples/goplugin/$e"
done
cd "$work"

expect() { # expect NAME WANT GOT
	if [ "$2" != "$3" ]; then
		printf '%s: FAIL\n--- want\n%s\n--- got\n%s\n' "$1" "$2" "$3"
		exit 1
	fi
	printf '%-14s ok\n' "$1"
}

expect basic "Hello!" "$(./basic "$ex/basic/plugin/greeter.gig" 2>/dev/null | tail -1)"

export KV_PLUGIN="$ex/kv/plugin-plain/kv-plain.gig"
./kv put hello world
expect kv/plain "world

Written from plugin-plain" "$(./kv get hello)"
export KV_PLUGIN="$ex/kv/plugin-base64/kv-base64.gig"
./kv put hello world
expect kv/base64 "world

Written from plugin-base64" "$(./kv get hello)"

export COUNTER_PLUGIN="$ex/bidirectional/plugin/counter.gig"
./bidirectional put hello 1
./bidirectional put hello 1
expect bidirectional 2 "$(./bidirectional get hello)"

export KV_PLUGIN="$ex/negotiated/plugin/kv.gig"
KV_PROTO=3 ./negotiated put hello world 2>/dev/null
expect negotiated "world

Written from plugin version 3
Read by plugin version 2" "$(KV_PROTO=2 ./negotiated get hello 2>/dev/null)"
expect negotiated/max "using protocol version 3" "$(KV_PROTO=2,3 ./negotiated get hello 2>&1 >/dev/null)"
expect negotiated/old "using protocol version 2" "$(./negotiated get hello 2>&1 >/dev/null)"

STREAMER_PLUGIN="$ex/streaming/plugin/streamer.gig" ./streaming myfile 2>/dev/null
expect streaming "Lorem ipsum dolor sit amet" "$(cat myfile)"

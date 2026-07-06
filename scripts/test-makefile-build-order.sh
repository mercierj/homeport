#!/bin/sh
set -eu

output="$(make -n build-with-web)"
target_line="$(awk '/^build-with-web:/ { print; exit }' Makefile)"

line_number() {
	printf '%s\n' "$output" | awk -v pattern="$1" 'index($0, pattern) { print NR; exit }'
}

deps_line="$(line_number 'go mod download')"
npm_install_line="$(line_number 'cd web && npm install')"
npm_build_line="$(line_number 'cd web && npm run build')"
copy_line="$(line_number 'cp -r web/dist/* internal/api/static/')"
go_build_line="$(line_number 'go build ')"

if [ -z "$deps_line" ] || [ -z "$npm_install_line" ] || [ -z "$npm_build_line" ] || [ -z "$copy_line" ] || [ -z "$go_build_line" ]; then
	printf '%s\n' "$output"
	printf '%s\n' "missing expected build command in make -n build-with-web output" >&2
	exit 1
fi

if [ "$deps_line" -gt "$npm_install_line" ]; then
	printf '%s\n' "$output"
	printf '%s\n' "go dependency preparation must run before web/node_modules is installed" >&2
	exit 1
fi

if [ "$npm_build_line" -gt "$copy_line" ] || [ "$copy_line" -gt "$go_build_line" ]; then
	printf '%s\n' "$output"
	printf '%s\n' "web assets must be built and copied before the final Go binary build" >&2
	exit 1
fi

case " $target_line " in
	*" build-cli "*)
		printf '%s\n' "$target_line"
		printf '%s\n' "build-cli must not be a direct build-with-web prerequisite; parallel make can build stale embedded assets" >&2
		exit 1
		;;
esac

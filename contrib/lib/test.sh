#!/bin/bash

set -euo pipefail

jb() {
  go tool github.com/jsonnet-bundler/jsonnet-bundler/cmd/jb "$@"
}

jsonnet() {
  go tool github.com/google/go-jsonnet/cmd/jsonnet "$@"
}

jb install

for test in *_test.jsonnet; do
  echo "Running ./$test"
  jsonnet -S -J vendor -e '(import "github.com/bastjan/jsonnunit/lib/runner.libsonnet").run(std.extVar("t"))' --ext-code-file t="$test"
done

echo "Manually testing errors.libsonnet#result.unwrap"

if out=$(jsonnet -e '(import "errors.libsonnet").err("some error").unwrap()' 2>&1); then
  echo "Expected error, got success"
  exit 1
else
  grep -q "RUNTIME ERROR: some error" <<< "$out" || {
    echo "Expected error message to contain 'RUNTIME ERROR: some error', got: $out"
    exit 1
  }
fi

echo "PASSED"

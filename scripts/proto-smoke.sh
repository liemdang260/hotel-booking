#!/usr/bin/env sh
set -eu

rm -rf gen/go
buf lint
buf generate

test -f gen/go/smoke/v1/smoke.pb.go
test -f gen/go/smoke/v1/smoke_grpc.pb.go
test -f gen/go/common/v1/error.pb.go

echo "protobuf smoke check passed"

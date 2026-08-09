#!/usr/bin/env sh
set -eu

rm -rf gen/go
buf lint
buf generate

test -f gen/go/hotelbooking/smoke/v1/smoke.pb.go
test -f gen/go/hotelbooking/smoke/v1/smoke_grpc.pb.go
test -f gen/go/hotelbooking/common/v1/error.pb.go

echo "protobuf smoke check passed"

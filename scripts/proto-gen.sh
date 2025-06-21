#!/usr/bin/env bash

protoc --go_out=./pkg/grpc/proto --go_opt=paths=source_relative --go-grpc_out=./pkg/grpc/proto --go-grpc_opt=paths=source_relative ./proxy.proto

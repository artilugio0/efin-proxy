#!/usr/bin/env bash

protoc --go_out=./internal/grpc/proto --go_opt=paths=source_relative --go-grpc_out=./internal/grpc/proto --go-grpc_opt=paths=source_relative ./proxy.proto

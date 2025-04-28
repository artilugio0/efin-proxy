#!/usr/bin/env bash

export CGO_ENABLED=0
go build -o efin-proxy ./cmd/proxy

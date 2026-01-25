#!/bin/bash
git fetch -p; git merge origin/main
go build && ./recorder > recorder.log 2>&1
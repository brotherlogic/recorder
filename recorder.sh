#!/bin/bash
git fetch -p; git merge origin/main
go build && ./recorder
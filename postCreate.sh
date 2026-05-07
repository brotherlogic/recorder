#!/bin/zsh

export GOPATH=/go
export PATH=$PATH:/usr/local/go/bin:$GOPATH/bin

sudo apt update
sudo apt install -y  protobuf-compiler xdg-utils flac
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest 
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Account for Ghostty
tic -x ghostty.terminfo

# Install tmux and emacs
sudo apt-get update && sudo apt-get install -y tmux emacs

# Auto-start tmux in zsh
if ! grep -q "tmux attach-session" ~/.zshrc; then
    echo -e "\n# Auto-start tmux\nif [[ -z \"\$TMUX\" && -o interactive ]]; then\n    tmux attach-session -t default || tmux new-session -s default\nfi" >> ~/.zshrc
fi
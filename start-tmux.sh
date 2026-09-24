#!/bin/bash

# Ensure the 'eurorack' session exists
if ! tmux has-session -t eurorack 2>/dev/null; then
  # Create a new session named 'eurorack', detached
  cd /workspaces/seraphine
  tmux new-session -d -s eurorack
  
  # Split the window horizontally (-h)
  # The left pane will remain a terminal
  # The right pane will run 'gh dash'
  tmux split-window -h -t eurorack "gh dash"
fi

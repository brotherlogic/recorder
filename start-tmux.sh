#!/bin/bash

# Ensure the 'recorder' session exists
if ! tmux has-session -t recorder 2>/dev/null; then
  # Create a new session named 'recorder', detached
  cd /workspaces/recorder
  tmux new-session -d -s recorder
  
  # Split the window horizontally (-h)
  # The left pane will remain a terminal
  # The right pane will run 'gh dash'
  tmux split-window -h -t recorder "gh dash"
fi

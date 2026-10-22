#!/usr/bin/env bash

set -e

PROJECT_HOME="$(pwd)"

# Delete previous session if it exists
tmux kill-session -t workspace 2>/dev/null || true

# Shorten every showed path to 2 parents
SHELL_CMD='export PROMPT_DIRTRIM=2; exec bash -i'

tmux new-session -d \
    -s workspace \
    -n test \
    -n system \
    -c "$PROJECT_HOME/cmd/controller" \
    "$SHELL_CMD"

tmux split-window -t workspace:system -h \
    -c "$PROJECT_HOME/cmd/api" \
    "$SHELL_CMD"

tmux split-window -t workspace:system -v \
    -c "$PROJECT_HOME/cmd/client" \
    "$SHELL_CMD"

tmux split-window -t workspace:system -h \
    -c "$PROJECT_HOME/cmd/client" \
    "$SHELL_CMD"

tmux new-window -t workspace \
    -n control \
    -c "$PROJECT_HOME" \
    "$SHELL_CMD"

tmux split-window -t workspace:control -h \
    -c "$PROJECT_HOME" \
    "$SHELL_CMD"

tmux new-window -t workspace \
    -n test \
    -c "$PROJECT_HOME/cmd/test" \
    "$SHELL_CMD"

tmux select-window -t workspace:system

tmux attach-session -t workspace
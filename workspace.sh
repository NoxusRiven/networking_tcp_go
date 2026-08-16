#!/usr/bin/env bash

set -e

PROJECT_HOME="$(pwd)"

# Usuń poprzednią sesję jeśli istnieje
tmux kill-session -t workspace 2>/dev/null || true

# Komenda startowa dla każdego pane'a:
# PROMPT_DIRTRIM=2 skraca ścieżkę w bashowym promptcie
SHELL_CMD='export PROMPT_DIRTRIM=2; exec bash -i'

tmux new-session -d \
    -s workspace \
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

tmux select-window -t workspace:system

tmux attach-session -t workspace
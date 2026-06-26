#!/bin/bash

tmux new-session -d \
    -s workspace \
    -n system \
    -c "./cmd/controller"

tmux split-window -t workspace:system -h \
    -c "./cmd/api"

tmux split-window -t workspace:system -v \
    -c "./cmd/client"

tmux split-window -t workspace:system -h \
    -c "./cmd/client"

tmux new-window -t workspace \
    -n control \

tmux split-window -t workspace:control -h \

tmux select-window -t workspace:system

tmux attach-session -t workspace
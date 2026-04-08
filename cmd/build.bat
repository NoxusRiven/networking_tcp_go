@echo off
setlocal

go build -o microservice\service.exe .\microservice
go build -o loadbalancer\lb.exe .\loadbalancer
go build -o agent\agent.exe .\agent

echo Finished building nodes!
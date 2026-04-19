@echo off
setlocal


go build -o microservice\service.exe .\microservice
echo finished buliding microservice
go build -o loadbalancer\lb.exe .\loadbalancer
echo finished buliding loadbalancer 
go build -o agent\agent.exe .\agent
echo finished buliding agent 

echo all nodes were built!
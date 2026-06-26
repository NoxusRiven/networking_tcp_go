@echo off
setlocal


go build -o bin\service.exe .\cmd\service
echo finished buliding microservice

go build -o bin\lb.exe .\cmd\lb
echo finished buliding loadbalancer 

go build -o bin\agent.exe .\cmd\agent
echo finished buliding agent 

echo all nodes were built!
# NetworkingGO
System that will be able to finish given tasks while communicating with eachother through tcp connection and dynamicly handling errors in system.

Also learning GO lang while making this project


# Usage
- build agent, loadbalancer and microservice nodes via build.bat/.sh
- run files in order:
    * cmd/controller
    * cmd/api
    * cmd/cli

This system allows for multiple cli instances, servers will work concurrently

Supported features are: 
- Ping, pings the server, server responds with current timestamp
- Idle work, stream of 10 messages sent by server with intervals of 2 seconds

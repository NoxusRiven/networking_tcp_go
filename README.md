# NetworkingGO
System that will be able to finish given tasks while communicating with eachother through tcp connection and dynamicly handling errors in system.

Also learning GO lang while making this project


# Usage
- configure nodes using [config file](#config-file)
- build agent, loadbalancer and microservice nodes via build.bat/.sh
- run files in order:
    * cmd/controller
    * cmd/api
    * cmd/cli

This system allows for multiple cli instances, servers will work concurrently.

Supported features are: 
- Ping, pings the server, server responds with current timestamp
- Idle work, stream of 10 messages sent by server with intervals of 2 seconds

# Config file
Config file is in json format. File should have name 'system.config.json' it will contain data of each system node, following data is:
- "node" - name of the node, names are predefined as follows:
    * "api"
    * "controller"
    * "agent"
    * "loadbalancer"
    * "microservice"
- "port" - port of the listener on which node will listen, port specified is base port that will be incremented with each new instance
    (for now only for ms)
- "host" - ip of the computer that executes program, only field that is optional, default value is "localhost"

For now host only will work for API and Controller. If config has to be present, with at least "node" (hast to be valid name) and "port"

Check example file "system.config.json.example".


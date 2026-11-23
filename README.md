# Project premise
System that will be able to finish tasks given by user, using nodes communicating with eachother and working together to complete work. Errors should be delt with dynamicly by the system itself, with as little administrator intervernience as possible

Also learning GO lang while making this project

# Usage
- configure nodes using [config file](#config-file)
- build agent, loadbalancer and microservice nodes via build.bat/.sh
- run files in order:
    * cmd/controller
    * cmd/api
    * cmd/cli - before running cli check [key files](#key-files)

This system allows for multiple cli instances, servers will work concurrently.

Supported features are: 
- Ping, pings the server, server responds with current timestamp
- Idle work, stream of 10 messages sent by server with intervals of 2 seconds

# Key files
API Gateway in first launch will generate public and private key files if they are not present in its main folder (default folder cmd/api).
After generating those files API public key should be coppied and renamed to client main file (default cmd/client) as 'api_pub.pem', this step is needed for encrypting and decrypting contentns between CLI and API, and otherwise client app wont launch

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


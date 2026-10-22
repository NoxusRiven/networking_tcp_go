package config

import (
	"fmt"
	"net"
	json "networking/tcp/internal/json_helper"
	"os"
)

type ConfigNodeName string
type CNN ConfigNodeName

const (
	CONFIG_FILENAME = "system.config.json"

	//config nodenames
	API_CONF        CNN = "api"
	CONTROLLER_CONF CNN = "controller"
	AGENT_CONF      CNN = "agent"
	LB_CONF         CNN = "loadbalancer"
	MS_CONF         CNN = "microservice"
)

type Config struct {
	Nodes []ConfigNode `json:"config"`
}

type ConfigNode struct {
	Name string `json:"node"`
	Port string `json:"port"`
	Host string `json:"host"`
}

func ReadConfigFile() (*Config, error) {

	//read file
	filepath := string("../../" + CONFIG_FILENAME)
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, err
	}

	//convert to json obj
	dataJson := string(data)

	config, err := json.FromJSON[*Config](dataJson)
	if err != nil {
		return nil, err
	}

	//return obj
	return config, nil
}

func (conf *Config) FindNodeByName(nodeName CNN) (*ConfigNode, error) {
	var nodeFound *ConfigNode = nil
	for _, node := range conf.Nodes {
		if node.Name == string(nodeName) {
			nodeFound = &node
			break
		}
	}

	if nodeFound == nil {
		return nil, fmt.Errorf("Node '%v' was not found in config file\n", nodeName)
	}

	return nodeFound, nil
}

func (conf *Config) FindAddrByNodeName(nodeName CNN) (string, error) {
	//find node with this name
	nodeFound, err := conf.FindNodeByName(nodeName)
	if err != nil {
		return "", err
	}

	//return joined host:port
	host := "localhost"

	if nodeFound.Port != "" {

		if nodeFound.Host != "" {
			host = nodeFound.Host
		}

		return net.JoinHostPort(host, nodeFound.Port), nil
	}

	return "", fmt.Errorf("Node '%v' has no field 'port' defined in config file\n", nodeName)
}

package utils

import (
	"strings"

	"github.com/Sirupsen/logrus"
	"github.com/pkg/errors"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/vishvananda/netlink"
)

const (
	hostLabelKeyword     = "__host_label__"
	hostInterfaceKeyword = "__host_interface__"
)

// UpdateCNIConfigByKeywords takes in the given CNI config, replaces the rancher
// specific keywords with the appropriate values.
func UpdateCNIConfigByKeywords(config interface{}, host metadata.Host) interface{} {
	switch props := config.(type) {
	case []interface{}:
		for i, v := range props {
			tempValue := UpdateCNIConfigByKeywords(v, host)
			props[i] = tempValue
		}
		return props
	case map[string]interface{}:
		for aKey, aValue := range props {
			if v, isString := aValue.(string); isString {
				if strings.HasPrefix(v, hostLabelKeyword) {
					props[aKey] = ""
					splits := strings.SplitN(v, ":", 2)
					if len(splits) > 1 {
						label := strings.TrimSpace(splits[1])
						labelValue := host.Labels[label]
						if labelValue != "" {
							props[aKey] = labelValue
						}
					}
				}
				if strings.HasPrefix(v, hostInterfaceKeyword) {
					props[aKey] = ""
					splits := strings.SplitN(v, ":", 2)
					if len(splits) > 1 {
						iface := strings.TrimSpace(splits[1])
						ipstr, err := getIPFromIface(iface, 0)
						if err != nil {
							logrus.Errorf("Got error from UpdateCNIConfigByKeywords: %v", err)
							break
						}
						props[aKey] = ipstr
					}
				}
			} else {
				tempValue := UpdateCNIConfigByKeywords(aValue, host)
				props[aKey] = tempValue
			}
		}
		return props
	default:
		return config
	}
}

func getIPFromIface(iface string, index int) (string, error) {
	ipstr := ""
	l, err := netlink.LinkByName(iface)
	if err != nil {
		return ipstr, errors.Wrap(err, "Failed to get bridge link")
	}

	addrs, err := netlink.AddrList(l, netlink.FAMILY_V4)
	if err != nil {
		return ipstr, errors.Wrap(err, "Failed to get bridge address")
	}

	if len(addrs) > index {
		ipstr = strings.Split(addrs[index].IPNet.String(), "/")[0]
	}

	if ipstr == "" {
		return ipstr, errors.Errorf("Get no ip address from %s", iface)
	}

	return ipstr, nil
}

// GetBridgeInfo is used to figure out the bridge information from the
// CNI config of the network specified
func GetBridgeInfo(network metadata.Network, host metadata.Host) (bridge string, bridgeSubnet string) {
	conf, _ := network.Metadata["cniConfig"].(map[string]interface{})
	for _, file := range conf {
		file = UpdateCNIConfigByKeywords(file, host)
		props, _ := file.(map[string]interface{})
		cniType, _ := props["type"].(string)
		checkBridge, _ := props["bridge"].(string)
		bridgeSubnet, _ = props["bridgeSubnet"].(string)

		if cniType == "rancher-bridge" && checkBridge != "" {
			bridge = checkBridge
			break
		}
	}
	return bridge, bridgeSubnet
}

// GetLocalNetworksAndRouters fetches networks and network containers
// related to that networks running in the current environment
func GetLocalNetworksAndRouters(networks []metadata.Network, host metadata.Host, services []metadata.Service) ([]metadata.Network, map[string]metadata.Container) {
	localRouters := map[string]metadata.Container{}
	for _, service := range services {
		// Trick to select the primary service of the network plugin
		// stack
		// TODO: Need to check if it's needed for Calico?
		if !(service.Kind == "networkDriverService" &&
			service.Name == service.PrimaryServiceName) {
			continue
		}

		for _, aContainer := range service.Containers {
			if aContainer.HostUUID == host.UUID {
				localRouters[aContainer.NetworkUUID] = aContainer
			}
		}
	}

	localNetworks := []metadata.Network{}
	for _, aNetwork := range networks {
		if aNetwork.EnvironmentUUID != host.EnvironmentUUID {
			continue
		}
		_, ok := aNetwork.Metadata["cniConfig"].(map[string]interface{})
		if !ok {
			continue
		}
		localNetworks = append(localNetworks, aNetwork)
	}

	return localNetworks, localRouters
}

// GetLocalNetworksAndRoutersFromMetadata is used to fetch networks local to the current environment
func GetLocalNetworksAndRoutersFromMetadata(mc metadata.Client) ([]metadata.Network, map[string]metadata.Container, error) {
	networks, err := mc.GetNetworks()
	if err != nil {
		return nil, nil, err
	}

	host, err := mc.GetSelfHost()
	if err != nil {
		return nil, nil, err
	}

	services, err := mc.GetServices()
	if err != nil {
		return nil, nil, err
	}

	networks, routers := GetLocalNetworksAndRouters(networks, host, services)

	return networks, routers, nil
}

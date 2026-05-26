package utils

import (
	"net"
	"os"
	"strings"
)

func GetHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return ""
	}
	return hostname
}

func GetLocalIP() string {
	if ip := strings.TrimSpace(os.Getenv("HOMECTL_AGENT_IP")); ip != "" {
		return ip
	}
	if iface := strings.TrimSpace(os.Getenv("HOMECTL_AGENT_INTERFACE")); iface != "" {
		if ip := GetLocalIPForInterface(iface); ip != "" {
			return ip
		}
	}
	return GetLocalIPInSubnet("192.168.1")
}

func GetLocalIPForInterface(interfaceName string) string {
	iface, err := net.InterfaceByName(interfaceName)
	if err != nil || iface.Flags&net.FlagUp == 0 {
		return ""
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ip4 := ipnet.IP.To4(); ip4 != nil {
				return ip4.String()
			}
		}
	}
	return ""
}

func GetLocalIPInSubnet(subnetPrefix string) string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ip4 := ipnet.IP.To4(); ip4 != nil {
					ipStr := ip4.String()
					if len(subnetPrefix) == 0 || hasPrefix(ipStr, subnetPrefix+".") {
						return ipStr
					}
				}
			}
		}
	}
	return ""
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

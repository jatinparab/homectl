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
	return GetLocalIPInSubnet("192.168.1")
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
					if len(subnetPrefix) == 0 || strings.HasPrefix(ipStr, subnetPrefix+".") {
						return ipStr
					}
				}
			}
		}
	}
	return ""
}

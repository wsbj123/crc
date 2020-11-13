// +build linux

package preflight

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/code-ready/crc/pkg/crc/logging"
	"github.com/code-ready/crc/pkg/crc/systemd"
	"github.com/code-ready/crc/pkg/crc/systemd/states"
	crcos "github.com/code-ready/crc/pkg/os"
)

var nmPreflightChecks = [...]Check{
	{
		configKeySuffix:  "check-systemd-networkd-running",
		checkDescription: "Checking if systemd-networkd is running",
		check:            checkSystemdNetworkdIsNotRunning,
		fixDescription:   "network configuration with systemd-networkd is not supported",
		flags:            NoFix,
	},
	{
		configKeySuffix:  "check-network-manager-installed",
		checkDescription: "Checking if NetworkManager is installed",
		check:            checkNetworkManagerInstalled,
		fixDescription:   "NetworkManager is required and must be installed manually",
		flags:            NoFix,
	},
	{
		configKeySuffix:  "check-network-manager-running",
		checkDescription: "Checking if NetworkManager service is running",
		check:            checkNetworkManagerIsRunning,
		fixDescription:   "NetworkManager is required. Please make sure it is installed and running manually",
		flags:            NoFix,
	},
}

var dnsmasqPreflightChecks = [...]Check{
	{
		configKeySuffix:    "check-network-manager-config",
		checkDescription:   "Checking if /etc/NetworkManager/conf.d/crc-nm-dnsmasq.conf exists",
		check:              checkCrcNetworkManagerConfig,
		fixDescription:     "Writing Network Manager config for crc",
		fix:                fixCrcNetworkManagerConfig,
		cleanupDescription: "Removing /etc/NetworkManager/conf.d/crc-nm-dnsmasq.conf file",
		cleanup:            removeCrcNetworkManagerConfig,
	},
	{
		configKeySuffix:    "check-crc-dnsmasq-file",
		checkDescription:   "Checking if /etc/NetworkManager/dnsmasq.d/crc.conf exists",
		check:              checkCrcDnsmasqConfigFile,
		fixDescription:     "Writing dnsmasq config for crc",
		fix:                fixCrcDnsmasqConfigFile,
		cleanupDescription: "Removing /etc/NetworkManager/dnsmasq.d/crc.conf file",
		cleanup:            removeCrcDnsmasqConfigFile,
	},
}

var (
	crcNetworkManagerRootPath = filepath.Join(string(filepath.Separator), "etc", "NetworkManager")

	crcDnsmasqConfigPath = filepath.Join(crcNetworkManagerRootPath, "dnsmasq.d", "crc.conf")
	crcDnsmasqConfig     = `server=/apps-crc.testing/192.168.130.11
server=/crc.testing/192.168.130.11
`

	crcNetworkManagerConfigPath = filepath.Join(crcNetworkManagerRootPath, "conf.d", "crc-nm-dnsmasq.conf")
	crcNetworkManagerConfig     = `[main]
dns=dnsmasq
`

	crcNetworkManagerDispatcherPath   = filepath.Join(crcNetworkManagerRootPath, "dispatcher.d", "pre-up.d", "99-crc.sh")
	crcNetworkManagerDispatcherConfig = `#!/bin/sh
# This is a NetworkManager dispatcher script to configure split DNS for
# the 'crc' libvirt network.
# The corresponding crc bridge is recreated each time the system reboots, so
# it cannot be configured permanently through NetworkManager.
# Changing DNS settings with nmcli requires the connection to go down/up,
# so we directly make the change using resolvectl

export LC_ALL=C

if [ "$1" = crc ]; then
        resolvectl domain "$1" ~testing
        resolvectl dns "$1" 192.168.130.11
        resolvectl default-route "$1" false
fi

exit 0
`
)

var systemdResolvedPreflightChecks = [...]Check{
	{
		configKeySuffix:  "check-systemd-resolved-running",
		checkDescription: "Checking if the systemd-resolved service is running",
		check:            checkSystemdResolvedIsRunning,
		fixDescription:   "systemd-resolved is required on this distribution. Please make sure it is installed and running manually",
		flags:            NoFix,
	},
	{
		configKeySuffix:    "check-crc-nm-dispatcher-file",
		checkDescription:   fmt.Sprintf("Checking if %s exists", crcNetworkManagerDispatcherPath),
		check:              checkCrcNetworkManagerDispatcherFile,
		fixDescription:     "Writing NetworkManager dispatcher file for crc",
		fix:                fixCrcNetworkManagerDispatcherFile,
		cleanupDescription: fmt.Sprintf("Removing %s file", crcNetworkManagerDispatcherPath),
		cleanup:            removeCrcNetworkManagerDispatcherFile,
	},
}

func fixNetworkManagerConfigFile(path string, content string, perms os.FileMode) error {
	err := crcos.WriteToFileAsRoot(
		fmt.Sprintf("write NetworkManager configuration to %s", path),
		content,
		path,
		perms,
	)
	if err != nil {
		return fmt.Errorf("Failed to write config file: %s: %v", path, err)
	}

	logging.Debug("Reloading NetworkManager")
	sd := systemd.NewHostSystemdCommander()
	if err := sd.Reload("NetworkManager"); err != nil {
		return fmt.Errorf("Failed to restart NetworkManager: %v", err)
	}

	return nil
}

func removeNetworkManagerConfigFile(path string) error {
	if err := checkNetworkManagerInstalled(); err != nil {
		// When NetworkManager is not installed, its config files won't exist
		return nil
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		logging.Debugf("Removing NetworkManager configuration file: %s", path)
		err := crcos.RemoveFileAsRoot(
			fmt.Sprintf("removing NetworkManager configuration file in %s", path),
			path,
		)
		if err != nil {
			return fmt.Errorf("Failed to remove NetworkManager configuration file: %s: %v", path, err)
		}

		logging.Debug("Reloading NetworkManager")
		sd := systemd.NewHostSystemdCommander()
		if err := sd.Reload("NetworkManager"); err != nil {
			return fmt.Errorf("Failed to restart NetworkManager: %v", err)
		}
	}
	return nil
}

func checkCrcDnsmasqConfigFile() error {
	logging.Debug("Checking dnsmasq configuration")
	err := crcos.FileContentMatches(crcDnsmasqConfigPath, []byte(crcDnsmasqConfig))
	if err != nil {
		return err
	}
	logging.Debug("dnsmasq configuration is good")
	return nil
}

func fixCrcDnsmasqConfigFile() error {
	logging.Debug("Fixing dnsmasq configuration")
	err := fixNetworkManagerConfigFile(crcDnsmasqConfigPath, crcDnsmasqConfig, 0644)
	if err != nil {
		return err
	}

	logging.Debug("dnsmasq configuration fixed")
	return nil
}

func removeCrcDnsmasqConfigFile() error {
	return removeNetworkManagerConfigFile(crcDnsmasqConfigPath)
}

func checkCrcNetworkManagerConfig() error {
	logging.Debug("Checking NetworkManager configuration")
	err := crcos.FileContentMatches(crcNetworkManagerConfigPath, []byte(crcNetworkManagerConfig))
	if err != nil {
		return err
	}
	logging.Debug("NetworkManager configuration is good")
	return nil
}

func fixCrcNetworkManagerConfig() error {
	logging.Debug("Fixing NetworkManager configuration")
	err := fixNetworkManagerConfigFile(crcNetworkManagerConfigPath, crcNetworkManagerConfig, 0644)
	if err != nil {
		return err
	}
	logging.Debug("NetworkManager configuration fixed")
	return nil
}

func removeCrcNetworkManagerConfig() error {
	return removeNetworkManagerConfigFile(crcNetworkManagerConfigPath)
}

func checkSystemdNetworkdIsNotRunning() error {
	err := checkSystemdServiceRunning("systemd-networkd.service")
	if err == nil {
		return fmt.Errorf("systemd-networkd.service is running")
	}

	logging.Debugf("systemd-networkd.service is not running")
	return nil
}

func checkNetworkManagerInstalled() error {
	logging.Debug("Checking if 'nmcli' is available")
	path, err := exec.LookPath("nmcli")
	if err != nil {
		return fmt.Errorf("NetworkManager cli nmcli was not found in path")
	}
	logging.Debug("'nmcli' was found in ", path)
	return nil
}

func checkSystemdServiceRunning(service string) error {
	logging.Debugf("Checking if %s is running", service)
	sd := systemd.NewHostSystemdCommander()
	status, err := sd.Status(service)
	if err != nil {
		return err
	}
	if status != states.Running {
		return fmt.Errorf("%s is not running", service)
	}
	logging.Debugf("%s is already running", service)
	return nil
}

func checkNetworkManagerIsRunning() error {
	return checkSystemdServiceRunning("NetworkManager.service")
}

func checkSystemdResolvedIsRunning() error {
	return checkSystemdServiceRunning("systemd-resolved.service")
}

func checkCrcNetworkManagerDispatcherFile() error {
	logging.Debug("Checking NetworkManager dispatcher file for crc network")
	err := crcos.FileContentMatches(crcNetworkManagerDispatcherPath, []byte(crcNetworkManagerDispatcherConfig))
	if err != nil {
		return err
	}
	logging.Debug("Dispatcher file has the expected content")
	return nil
}

func fixCrcNetworkManagerDispatcherFile() error {
	logging.Debug("Fixing NetworkManager dispatcher configuration")
	err := fixNetworkManagerConfigFile(crcNetworkManagerDispatcherPath, crcNetworkManagerDispatcherConfig, 0755)
	if err != nil {
		return err
	}

	logging.Debug("NetworkManager dispatcher configuration fixed")
	return nil
}

func removeCrcNetworkManagerDispatcherFile() error {
	return removeNetworkManagerConfigFile(crcNetworkManagerDispatcherPath)
}
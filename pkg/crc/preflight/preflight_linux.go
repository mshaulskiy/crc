package preflight

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"strings"
	"syscall"

	"github.com/code-ready/crc/pkg/crc/logging"
	"github.com/code-ready/crc/pkg/crc/network"
	crcos "github.com/code-ready/crc/pkg/os"
	"github.com/code-ready/crc/pkg/os/linux"
)

var libvirtPreflightChecks = [...]Check{
	{
		configKeySuffix:  "check-virt-enabled",
		checkDescription: "Checking if Virtualization is enabled",
		check:            checkVirtualizationEnabled,
		fixDescription:   "Setting up virtualization",
		fix:              fixVirtualizationEnabled,
	},
	{
		configKeySuffix:  "check-kvm-enabled",
		checkDescription: "Checking if KVM is enabled",
		check:            checkKvmEnabled,
		fixDescription:   "Setting up KVM",
		fix:              fixKvmEnabled,
	},
	{
		configKeySuffix:  "check-libvirt-installed",
		checkDescription: "Checking if libvirt is installed",
		check:            checkLibvirtInstalled,
		fixDescription:   "Installing libvirt service and dependencies",
		fix:              fixLibvirtInstalled,
	},
	{
		configKeySuffix:  "check-user-in-libvirt-group",
		checkDescription: "Checking if user is part of libvirt group",
		check:            checkUserPartOfLibvirtGroup,
		fixDescription:   "Adding user to libvirt group",
		fix:              fixUserPartOfLibvirtGroup,
	},
	{
		configKeySuffix:  "check-libvirt-running",
		checkDescription: "Checking if libvirt daemon is running",
		check:            checkLibvirtServiceRunning,
		fixDescription:   "Starting libvirt service",
		fix:              fixLibvirtServiceRunning,
	},
	{
		configKeySuffix:  "check-libvirt-version",
		checkDescription: "Checking if a supported libvirt version is installed",
		check:            checkLibvirtVersion,
		fixDescription:   "Installing a supported libvirt version",
		fix:              fixLibvirtVersion,
	},
	{
		configKeySuffix:  "check-libvirt-driver",
		checkDescription: "Checking if crc-driver-libvirt is installed",
		check:            checkMachineDriverLibvirtInstalled,
		fixDescription:   "Installing crc-driver-libvirt",
		fix:              fixMachineDriverLibvirtInstalled,
	},
	{
		configKeySuffix:  "check-obsolete-libvirt-driver",
		checkDescription: "Checking for obsolete crc-driver-libvirt",
		check:            checkOldMachineDriverLibvirtInstalled,
		fixDescription:   "Removing older system-wide crc-driver-libvirt",
		fix:              fixOldMachineDriverLibvirtInstalled,
		flags:            SetupOnly,
	},
	{
		configKeySuffix:    "check-crc-network",
		checkDescription:   "Checking if libvirt 'crc' network is available",
		check:              checkLibvirtCrcNetworkAvailable,
		fixDescription:     "Setting up libvirt 'crc' network",
		fix:                fixLibvirtCrcNetworkAvailable,
		cleanupDescription: "Removing 'crc' network from libvirt",
		cleanup:            removeLibvirtCrcNetwork,
	},
	{
		configKeySuffix:  "check-crc-network-active",
		checkDescription: "Checking if libvirt 'crc' network is active",
		check:            checkLibvirtCrcNetworkActive,
		fixDescription:   "Starting libvirt 'crc' network",
		fix:              fixLibvirtCrcNetworkActive,
	},
	{
		cleanupDescription: "Removing the crc VM if exists",
		cleanup:            removeCrcVM,
		flags:              CleanUpOnly,
	},
}

var vsockPreflightChecks = Check{
	configKeySuffix:  "check-vsock",
	checkDescription: "Checking if vsock is correctly configured",
	check:            checkVsock,
	fixDescription:   "Checking if vsock is correctly configured",
	fix:              fixVsock,
}

func checkVsock() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	getcap, _, err := crcos.RunWithDefaultLocale("getcap", executable)
	if err != nil {
		return err
	}
	if !strings.Contains(string(getcap), "cap_net_bind_service+eip") {
		return fmt.Errorf("capabilities are not correct for %s", executable)
	}
	info, err := os.Stat("/dev/vsock")
	if err != nil {
		return err
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		group, err := user.LookupGroupId(fmt.Sprint(stat.Gid))
		if err != nil {
			return err
		}
		if group.Name != "libvirt" {
			return errors.New("/dev/vsock is not is the right group")
		}
	} else {
		return errors.New("cannot cast info")
	}
	if info.Mode()&0060 == 0 {
		return errors.New("/dev/vsock doesn't have the right permissions")
	}
	return nil
}

func fixVsock() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	_, _, err = crcos.RunWithPrivilege("setcap cap_net_bind_service=+eip", "setcap", "cap_net_bind_service=+eip", executable)
	if err != nil {
		return err
	}
	_, _, err = crcos.RunWithPrivilege("modprobe vhost_vsock", "modprobe", "vhost_vsock")
	if err != nil {
		return err
	}
	_, _, err = crcos.RunWithPrivilege("chown /dev/vsock", "chown", "root:libvirt", "/dev/vsock")
	if err != nil {
		return err
	}
	_, _, err = crcos.RunWithPrivilege("chmod /dev/vsock", "chmod", "g+rw", "/dev/vsock")
	if err != nil {
		return err
	}
	return nil
}

func getAllPreflightChecks() []Check {
	checks := getPreflightChecksForDistro(distro(), network.DefaultMode)
	checks = append(checks, vsockPreflightChecks)
	return checks
}

func getPreflightChecks(_ bool, networkMode network.Mode) []Check {
	return getPreflightChecksForDistro(distro(), networkMode)
}

func getPreflightChecksForDistro(distro linux.OsType, networkMode network.Mode) []Check {
	checks := commonChecks()

	if networkMode == network.VSockMode {
		checks = append(checks, vsockPreflightChecks)
	}

	switch distro {
	case linux.Ubuntu:
	case linux.RHEL, linux.CentOS, linux.Fedora:
		if networkMode == network.DefaultMode {
			checks = append(checks, redhatPreflightChecks[:]...)
		}
	default:
		logging.Warnf("distribution-specific preflight checks are not implemented for %s", distro)
		if networkMode == network.DefaultMode {
			checks = append(checks, redhatPreflightChecks[:]...)
		}
	}

	return checks
}

func commonChecks() []Check {
	var checks []Check
	checks = append(checks, genericPreflightChecks[:]...)
	checks = append(checks, nonWinPreflightChecks[:]...)
	checks = append(checks, libvirtPreflightChecks[:]...)
	return checks
}

func distro() linux.OsType {
	distro, err := linux.GetOsRelease()
	if err != nil {
		logging.Warnf("cannot get distribution name: %v", err)
		return "unknown"
	}
	return distro.ID
}

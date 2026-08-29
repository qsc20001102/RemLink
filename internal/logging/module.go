// Package logging configures structured RemLink logs and packet sampling.
package logging

// Module identifies the subsystem that emitted a log record.
type Module string

const (
	ModuleCore      Module = "CORE"
	ModuleBootstrap Module = "BOOTSTRAP"
	ModuleWG        Module = "WG"
	ModuleIPAM      Module = "IPAM"
	ModuleControl   Module = "CONTROL"
	ModuleSession   Module = "SESSION"
	ModuleRoute     Module = "ROUTE"
	ModuleNetstack  Module = "NETSTACK"
	ModuleTUN       Module = "TUN"
	ModuleSubnet    Module = "SUBNET"
	ModuleSystem    Module = "SYSTEM"
)

// Modules is the complete v1 logging-module set.
var Modules = [...]Module{
	ModuleCore,
	ModuleBootstrap,
	ModuleWG,
	ModuleIPAM,
	ModuleControl,
	ModuleSession,
	ModuleRoute,
	ModuleNetstack,
	ModuleTUN,
	ModuleSubnet,
	ModuleSystem,
}

// Valid reports whether the module belongs to the v1 logging taxonomy.
func (m Module) Valid() bool {
	switch m {
	case ModuleCore,
		ModuleBootstrap,
		ModuleWG,
		ModuleIPAM,
		ModuleControl,
		ModuleSession,
		ModuleRoute,
		ModuleNetstack,
		ModuleTUN,
		ModuleSubnet,
		ModuleSystem:
		return true
	default:
		return false
	}
}

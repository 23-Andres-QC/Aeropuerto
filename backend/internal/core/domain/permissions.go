package domain

import "regexp"

// The modules of the web and, with them, what each role may reach. This is the only place the matrix lives: the API
// (every endpoint, every role), nginx (the live view, the phones and the video room) and the menu of the web all read it.
const (
	ModBase     = "base"          // the lists every working screen needs: the sites, their map and their sessions
	ModEnVivo   = "en_vivo"       // 1. En vivo (and the replay and the people panel)
	ModInsights = "insights"      // 2. Insights
	ModVentas   = "ventas"        // 3. Ventas
	ModRegistro = "registros"     // 4. Registros
	ModConfig   = "configuracion" // 5. Configuración (and anything that changes the plan, the zones or the data)
	ModTelefono = "telefonos"     // 6. Teléfonos (cameras)
	ModVideos   = "videos"        // 7. Videos (upload and tracking)
	ModUsuarios = "usuarios"      // accounts, keys, audit log, privacy and the demo site
)

// WebModules are the ones that have a place in the menu.
var WebModules = []string{ModEnVivo, ModInsights, ModVentas, ModRegistro, ModConfig, ModTelefono, ModVideos, ModUsuarios}

var rolePermissions = map[string]map[string]bool{
	RoleAdmin:    set(ModBase, ModEnVivo, ModInsights, ModVentas, ModRegistro, ModConfig, ModTelefono, ModVideos, ModUsuarios),
	RoleSecurity: set(ModBase, ModEnVivo, ModTelefono, ModVideos),
	RoleBusiness: set(ModBase, ModVentas, ModInsights),
	// The scripts (Build, Part III) read and write everything but the accounts.
	RoleMachine: set(ModBase, ModEnVivo, ModInsights, ModVentas, ModRegistro, ModConfig, ModTelefono, ModVideos),
}

func set(modules ...string) map[string]bool {
	m := make(map[string]bool, len(modules))
	for _, k := range modules {
		m[k] = true
	}
	return m
}

// Can reports whether a role may use a module. A role that is not in the matrix (a store, a store key) may use none:
// stores have their own, narrower scope.
func Can(role, module string) bool { return rolePermissions[role][module] }

// ModulesOf lists the menu modules of a role, in menu order.
func ModulesOf(role string) []string {
	out := []string{}
	for _, m := range WebModules {
		if Can(role, m) {
			out = append(out, m)
		}
	}
	return out
}

var (
	reOnlySuper = regexp.MustCompile(`^/api/v1/(usuarios|auditoria|demo|privacidad)(/|$)|/llaves(/|$)|/grupo-minimo$`)
	rePhones    = regexp.MustCompile(`^/api/v1/telefonos(/|$)`)
	reCameras   = regexp.MustCompile(`^/api/v1/cameras/[^/]+/`)
	reSales     = regexp.MustCompile(`^/api/v1/sites/[^/]+/(ventas(/|$)|sessions/[^/]+/ventas(/|$))`)
	reTraffic   = regexp.MustCompile(`^/api/v1/sites/[^/]+/trafico/(resumen|serie|calor|permanencia|embudo|umbrales)$`)
	reInsights  = regexp.MustCompile(`^/api/v1/sites/[^/]+/sessions/[^/]+/(insights|analytics|points)$`)
	reMedia     = regexp.MustCompile(`^/api/v1/sites/[^/]+/media/.+$`)
	// The positions of a saved session: En vivo plays them back and Insights draws its movement and heat maps from them.
	reReplay = regexp.MustCompile(`^/api/v1/sites/[^/]+/sessions/[^/]+/replay$`)
	// Saving a capture is what En vivo does when the live mode is turned off.
	reSave = regexp.MustCompile(`^/api/v1/sites/[^/]+/sessions$`)
	reBase      = regexp.MustCompile(`^/api/v1/sites(/[^/]+/(config|sessions))?$`)
)

// ModuleOf says which module an API request belongs to. What it does not recognize belongs to the strictest one that can
// change things (ModConfig), so a new endpoint is closed to the business and security roles until it is listed here.
func ModuleOf(method, path string) string {
	read := method == "GET" || method == "HEAD"
	switch {
	case reOnlySuper.MatchString(path):
		return ModUsuarios
	case rePhones.MatchString(path):
		return ModTelefono
	case reCameras.MatchString(path):
		return ModEnVivo
	case reSales.MatchString(path):
		// Sales and insights are for looking: loading tickets, matching and simulating data belong to Configuración.
		if read {
			return ModVentas
		}
		return ModConfig
	case read && reTraffic.MatchString(path), read && reInsights.MatchString(path):
		return ModInsights
	case read && reReplay.MatchString(path):
		return ModBase
	case read && reMedia.MatchString(path):
		return ModEnVivo
	case method == "POST" && reSave.MatchString(path):
		return ModEnVivo
	case read && reBase.MatchString(path):
		return ModBase
	}
	return ModConfig
}

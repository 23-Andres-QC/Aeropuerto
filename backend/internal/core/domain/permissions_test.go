package domain

import "testing"

func TestModuleOf(t *testing.T) {
	for _, c := range []struct{ method, path, want string }{
		{"GET", "/api/v1/sites", ModBase}, {"GET", "/api/v1/sites/esan/config", ModBase}, {"GET", "/api/v1/sites/esan/sessions", ModBase},
		{"GET", "/api/v1/sites/esan/sessions/s1/replay", ModBase}, {"GET", "/api/v1/sites/esan/media/a/b.mp4", ModEnVivo},
		{"POST", "/api/v1/sites/esan/sessions", ModEnVivo}, {"DELETE", "/api/v1/sites/esan/sessions/s1", ModConfig},
		{"GET", "/api/v1/cameras/cam01/watch", ModEnVivo}, {"POST", "/api/v1/cameras/cam01/detections/publish", ModEnVivo},
		{"GET", "/api/v1/telefonos", ModTelefono}, {"DELETE", "/api/v1/telefonos/x", ModTelefono},
		{"GET", "/api/v1/sites/esan/ventas/resumen", ModVentas}, {"POST", "/api/v1/sites/esan/sessions/s1/ventas/emparejar", ModConfig}, {"POST", "/api/v1/sites/esan/ventas/tickets", ModConfig},
		{"POST", "/api/v1/sites/esan/ventas/simulados", ModConfig},
		{"GET", "/api/v1/sites/esan/trafico/calor", ModInsights}, {"GET", "/api/v1/sites/esan/sessions/s1/insights", ModInsights},
		{"PUT", "/api/v1/sites/esan/trafico/umbrales", ModConfig}, {"POST", "/api/v1/sites/esan/trafico/recalcular", ModConfig},
		{"POST", "/api/v1/sites", ModConfig}, {"PUT", "/api/v1/sites/esan/plano", ModConfig}, {"POST", "/api/v1/sites/esan/zones", ModConfig},
		{"GET", "/api/v1/usuarios", ModUsuarios}, {"GET", "/api/v1/auditoria", ModUsuarios}, {"PUT", "/api/v1/sites/esan/grupo-minimo", ModUsuarios},
		{"GET", "/api/v1/sites/esan/tiendas/3/llaves", ModUsuarios}, {"POST", "/api/v1/demo", ModUsuarios}, {"GET", "/api/v1/privacidad", ModUsuarios},
		// lo que nadie listó va al módulo más estricto que puede cambiar cosas
		{"GET", "/api/v1/algo-nuevo", ModConfig}, {"GET", "/api/v1/sites/esan/otra-cosa", ModConfig},
	} {
		if got := ModuleOf(c.method, c.path); got != c.want {
			t.Errorf("%s %s: %s (debía ser %s)", c.method, c.path, got, c.want)
		}
	}
}

func TestRoleModules(t *testing.T) {
	for role, want := range map[string][]string{
		RoleAdmin:    {ModEnVivo, ModInsights, ModVentas, ModRegistro, ModConfig, ModTelefono, ModVideos, ModUsuarios},
		RoleSecurity: {ModEnVivo, ModTelefono, ModVideos},
		RoleBusiness: {ModInsights, ModVentas},
	} {
		got := ModulesOf(role)
		if len(got) != len(want) {
			t.Errorf("%s: %v (debía ser %v)", role, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s: %v (debía ser %v)", role, got, want)
			}
		}
	}
	if Can(RoleMachine, ModUsuarios) || !Can(RoleMachine, ModConfig) {
		t.Error("la llave de scripts lee y escribe todo menos las cuentas")
	}
	if Can("", ModBase) || Can("desconocido", ModEnVivo) {
		t.Error("un rol desconocido (o uno que ya no existe, como tienda) no tiene módulos")
	}
}

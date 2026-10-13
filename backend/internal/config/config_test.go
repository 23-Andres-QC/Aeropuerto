package config

import (
	"strings"
	"testing"
)

func TestLocalEnvironmentWorksWithNoSetup(t *testing.T) {
	c := Config{Env: EnvLocal}
	if err := c.resolveAccess(); err != nil {
		t.Fatal(err)
	}
	if c.AdminUser != LocalUser || c.AdminPassword != LocalPassword || c.APIKey != LocalAPIKey {
		t.Errorf("local defaults: %+v", c)
	}
	if !c.UsesDemoCredentials() {
		t.Error("the local defaults are the demo credentials")
	}
}

func TestLocalKeepsWhatIsConfigured(t *testing.T) {
	c := Config{Env: EnvLocal, AdminUser: "otro", AdminPassword: "una-clave-propia-1", APIKey: "k"}
	if err := c.resolveAccess(); err != nil {
		t.Fatal(err)
	}
	if c.AdminUser != "otro" || c.AdminPassword != "una-clave-propia-1" || c.APIKey != "k" || c.UsesDemoCredentials() {
		t.Errorf("configured values replaced: %+v", c)
	}
}

func TestServidorRefusesDemoOrWeakSecrets(t *testing.T) {
	good := Config{Env: EnvServidor, AdminUser: "admin", AdminPassword: "una-clave-larga-123", APIKey: strings.Repeat("k", 24)}
	c := good
	if err := c.resolveAccess(); err != nil {
		t.Fatalf("real credentials: %v", err)
	}
	for name, mod := range map[string]func(*Config){
		"sin variables":   func(c *Config) { *c = Config{Env: EnvServidor} },
		"clave de prueba": func(c *Config) { c.AdminPassword = LocalPassword },
		"clave corta":     func(c *Config) { c.AdminPassword = "corta" },
		"llave de prueba": func(c *Config) { c.APIKey = LocalAPIKey },
		"llave corta":     func(c *Config) { c.APIKey = "corta" },
		"sin usuario":     func(c *Config) { c.AdminUser = "" },
	} {
		c := good
		mod(&c)
		err := c.resolveAccess()
		if err == nil {
			t.Errorf("%s: should refuse to start", name)
			continue
		}
		// The message names what is missing, never what was configured.
		for _, secret := range []string{good.AdminPassword, good.APIKey} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("%s: the error leaks a secret: %v", name, err)
			}
		}
	}
}

func TestUnknownEnvironment(t *testing.T) {
	c := Config{Env: "produccion"}
	if err := c.resolveAccess(); err == nil {
		t.Error("an unknown APP_ENV must be an error")
	}
}

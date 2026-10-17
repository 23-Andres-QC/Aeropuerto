package domain

import "testing"

func TestPrivateGenderCases(t *testing.T) {
	for _, c := range []struct {
		name    string
		in      map[string]int
		visible map[string]int
		hidden  int
	}{
		{"todo visible", map[string]int{"HOMBRE": 12, "MUJER": 9, "SIN_DETERMINAR": 5}, map[string]int{"HOMBRE": 12, "MUJER": 9, "SIN_DETERMINAR": 5}, 0},
		{"una chica sola no se ve ni se deduce", map[string]int{"HOMBRE": 20, "MUJER": 1}, map[string]int{}, 21},
		{"dos pequeñas juntas bastan", map[string]int{"HOMBRE": 20, "MUJER": 3, "SIN_DETERMINAR": 3}, map[string]int{"HOMBRE": 20}, 6},
		{"una pequeña arrastra a la menor visible", map[string]int{"HOMBRE": 20, "MUJER": 9, "SIN_DETERMINAR": 1}, map[string]int{"HOMBRE": 20}, 10},
		{"vacías no cuentan", map[string]int{"HOMBRE": 7, "MUJER": 0, "SIN_DETERMINAR": 0}, map[string]int{"HOMBRE": 7}, 0},
		{"un grupo pequeño solo", map[string]int{"HOMBRE": 3}, map[string]int{}, 3},
		{"sin nadie", map[string]int{}, map[string]int{}, 0},
		{"nil", nil, map[string]int{}, 0},
	} {
		visible, hidden := PrivateGender(c.in, MinGenderGroup)
		if hidden != c.hidden || len(visible) != len(c.visible) {
			t.Errorf("%s: visible %v, ocultos %d (quería %v, %d)", c.name, visible, hidden, c.visible, c.hidden)
			continue
		}
		for g, n := range c.visible {
			if visible[g] != n {
				t.Errorf("%s: %s = %d (quería %d)", c.name, g, visible[g], n)
			}
		}
	}
}

// For every combination of counts: what is shown is big enough, the shown plus the hidden add up to the total, and what is left out
// cannot be found by subtracting (it is at least k people, or nothing is shown at all).
func TestPrivateGenderNeverLeaksASmallGroup(t *testing.T) {
	for m := 0; m <= 14; m++ {
		for w := 0; w <= 14; w++ {
			for u := 0; u <= 14; u++ {
				in := map[string]int{"HOMBRE": m, "MUJER": w, "SIN_DETERMINAR": u}
				visible, hidden := PrivateGender(in, MinGenderGroup)
				shown := 0
				for g, n := range visible {
					if n < MinGenderGroup || n != in[g] {
						t.Fatalf("%v: se muestra %s = %d", in, g, n)
					}
					shown += n
				}
				if shown+hidden != m+w+u {
					t.Fatalf("%v: %d visibles + %d ocultos no suman el total", in, shown, hidden)
				}
				if hidden > 0 && hidden < MinGenderGroup && len(visible) > 0 {
					t.Fatalf("%v: quedan %d ocultos (menos de %d) con categorías a la vista: se deducen restando", in, hidden, MinGenderGroup)
				}
				// A category left out always has company in the hidden group, or the whole group is the hidden one.
				for g, n := range in {
					if _, shown := visible[g]; !shown && n > 0 && hidden < n {
						t.Fatalf("%v: %s (%d) no se muestra pero los ocultos son %d", in, g, n, hidden)
					}
				}
			}
		}
	}
}

func TestCheckPassword(t *testing.T) {
	for pw, ok := range map[string]bool{
		"Una-clave-larga-1": true, "Doce caracteres 1": true, "corta": false, "": false, "aaaaaaaaaaaaaaaa": false, "11111111111111": false,
		"mi-usuario-largo": false, "Xyyyyyyyyyyy1": true,
		// la política: mayúscula, minúscula y número
		"sin mayuscula 123456": false, "SIN MINUSCULA 123456": false, "Sin numero en la clave": false,
	} {
		err := CheckPassword(pw, "mi-usuario-largo")
		if (err == nil) != ok {
			t.Errorf("%q: %v (aceptada: %v)", pw, err, ok)
		}
	}
	long := make([]rune, MaxPassword+1)
	for i := range long {
		long[i] = rune('a' + i%20)
	}
	if CheckPassword(string(long), "") == nil {
		t.Error("más de 128 caracteres debe rechazarse")
	}
}

func TestUserInput(t *testing.T) {
	for name, c := range map[string]struct {
		in UserInput
		ok bool
	}{
		"con espacios y mayúsculas": {UserInput{Username: " Caja.Uno ", Email: "Caja@Lap.pe", Role: RoleBusiness}, true},
		"admin":               {UserInput{Username: "ana@lap.pe", Email: "ana@lap.pe", Role: RoleAdmin}, true},
		"seguridad":           {UserInput{Username: "vigilante", Email: "vigi@lap.pe", Role: RoleSecurity}, true},
		"negocio":             {UserInput{Username: "gerente", Email: "gerente@lap.pe", Role: RoleBusiness}, true},
		"solo el correo":      {UserInput{Email: "solo.correo@lap.pe", Role: RoleBusiness}, true},
		"sin correo":          {UserInput{Username: "ana", Role: RoleAdmin}, false},
		"correo malo":         {UserInput{Username: "ana", Email: "ana@", Role: RoleAdmin}, false},
		"el rol tienda ya no existe": {UserInput{Username: "caja", Email: "c@lap.pe", Role: "tienda"}, false},
		"rol":                 {UserInput{Username: "caja", Email: "c@lap.pe", Role: "root"}, false},
		"usuario corto":       {UserInput{Username: "ab", Email: "c@lap.pe", Role: RoleBusiness}, false},
		"usuario con espacio": {UserInput{Username: "caja uno", Email: "c@lap.pe", Role: RoleBusiness}, false},
	} {
		in := c.in
		if err := in.Normalize(); (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
		}
		if c.ok && c.in.Username != "" && in.Username != NormalizeUsername(c.in.Username) {
			t.Errorf("%s: el usuario no se normalizó: %q", name, in.Username)
		}
	}
}
